package pasta_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/asciimoth/pasta/pasta"
)

type workspaceNode struct {
	l pasta.Logger
}

func (n *workspaceNode) OnInit(w *pasta.Workspace, l pasta.Logger, id uint64, class string) error {
	n.l = l
	l.Debugf("init id=%d class=%s", id, class)
	return nil
}

func (n *workspaceNode) OnReady() error {
	n.l.Debug("ready")
	return nil
}

func (n *workspaceNode) OnStop() {
	n.l.Debug("stop")
}

func (n *workspaceNode) OnPortAdd(port uint64, direction string, types []string) error {
	n.l.Debugf("port add port=%d direction=%s types=%v", port, direction, types)
	return nil
}

func (n *workspaceNode) OnPortRemoved(port uint64, direction string) error {
	n.l.Debugf("port removed port=%d direction=%s", port, direction)
	return nil
}

func (n *workspaceNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	n.l.Debugf("pre link add port=%d type=%s direction=%s", port, linkType, portDirection)
	return nil
}

func (n *workspaceNode) OnLinkAdd(link, port uint64, linkType, portDirection string) error {
	n.l.Debugf("link add link=%d port=%d type=%s direction=%s", link, port, linkType, portDirection)
	return nil
}

func (n *workspaceNode) OnLinkRemoved(link, port uint64, linkType, portDirection string) error {
	n.l.Debugf("link removed link=%d port=%d type=%s direction=%s", link, port, linkType, portDirection)
	return nil
}

func TestWorkspaceAddRemoveNodesPortsLinksLogs(t *testing.T) {
	logf := &StringLoggerFactory{}
	w := pasta.NewWorkspace(logf)

	if !w.IsReady() {
		t.Fatal("new workspace is not ready")
	}

	nodeA := &workspaceNode{}
	nodeB := &workspaceNode{}

	nodeAID, err := w.AddNode(nodeA, "example.com/NodeA", "")
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	nodeBID, err := w.AddNode(nodeB, "example.com/NodeB", "")
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}

	left, err := w.AddPort(pasta.Port{
		Node:      nodeAID,
		Direction: "left",
		Types:     []string{"example.com/typeA"},
	})
	if err != nil {
		t.Fatalf("AddPort left: %v", err)
	}
	right, err := w.AddPort(pasta.Port{
		Node:      nodeBID,
		Direction: "right",
		Types:     []string{"example.com/typeA"},
	})
	if err != nil {
		t.Fatalf("AddPort right: %v", err)
	}

	link, linkType, err := w.AddLink(left, right)
	if err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	if link == 0 {
		t.Fatal("AddLink returned zero id")
	}
	if linkType != "example.com/typeA" {
		t.Fatalf("link type = %q, want example.com/typeA", linkType)
	}
	if !w.PortsConnected(left, right) || !w.PortsConnected(right, left) {
		t.Fatal("ports are not connected after AddLink")
	}

	w.RemoveLink(link)
	if w.PortsConnected(left, right) {
		t.Fatal("ports are connected after RemoveLink")
	}
	w.RemovePort(left)
	w.RemoveNode(nodeBID)

	want := strings.Join([]string{
		"1 example.com/NodeA[debug]init id=1 class=example.com/NodeA",
		"1 example.com/NodeA[debug]ready",
		"workspace[debug]node added1",
		"2 example.com/NodeB[debug]init id=2 class=example.com/NodeB",
		"2 example.com/NodeB[debug]ready",
		"workspace[debug]node added2",
		"1 example.com/NodeA[debug]port add port=3 direction=left types=[example.com/typeA]",
		"workspace[debug]port added3",
		"2 example.com/NodeB[debug]port add port=4 direction=right types=[example.com/typeA]",
		"workspace[debug]port added4",
		"1 example.com/NodeA[debug]pre link add port=3 type=example.com/typeA direction=left",
		"2 example.com/NodeB[debug]pre link add port=4 type=example.com/typeA direction=right",
		"1 example.com/NodeA[debug]link add link=5 port=3 type=example.com/typeA direction=left",
		"2 example.com/NodeB[debug]link add link=5 port=4 type=example.com/typeA direction=right",
		"workspace[debug]link added5",
		"1 example.com/NodeA[debug]link removed link=5 port=3 type=example.com/typeA direction=left",
		"2 example.com/NodeB[debug]link removed link=5 port=4 type=example.com/typeA direction=right",
		"workspace[debug]removed link5",
		"1 example.com/NodeA[debug]port removed port=3 direction=left",
		"workspace[debug]removed port3",
		"2 example.com/NodeB[debug]stop",
		"workspace[debug]removed port4",
		"workspace[debug]removed node2",
		"",
	}, "\n")

	if got := logf.Result(); got != want {
		t.Fatalf("logs mismatch\ngot:\n%swant:\n%s", got, want)
	}
}

func TestWorkspaceRejectsInvalidNodeAndPortOperations(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	node := &workspaceNode{}

	if _, err := w.AddNode(node, "example.com/node", ""); !errors.Is(err, pasta.ErrClassName) {
		t.Fatalf("invalid class AddNode error = %v, want %v", err, pasta.ErrClassName)
	}

	nodeID, err := w.AddNode(node, "example.com/Node", "")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if _, err := w.AddNode(node, "example.com/Node", ""); !errors.Is(err, pasta.ErrNodeDup) {
		t.Fatalf("duplicate AddNode error = %v, want %v", err, pasta.ErrNodeDup)
	}

	portCases := []struct {
		name string
		port pasta.Port
		want error
	}{
		{
			name: "missing node",
			port: pasta.Port{Node: 999, Direction: "left", Types: []string{"example.com/typeA"}},
			want: pasta.ErrNoNode,
		},
		{
			name: "bad direction",
			port: pasta.Port{Node: nodeID, Direction: "up", Types: []string{"example.com/typeA"}},
			want: pasta.ErrPortDirection,
		},
		{
			name: "no types",
			port: pasta.Port{Node: nodeID, Direction: "left"},
			want: pasta.ErrNoPortTypes,
		},
		{
			name: "bad type",
			port: pasta.Port{Node: nodeID, Direction: "left", Types: []string{"example.com/TypeA"}},
			want: pasta.ErrTypeName,
		},
	}

	for _, tt := range portCases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := w.AddPort(tt.port)
			if !errors.Is(err, tt.want) {
				t.Fatalf("AddPort error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestWorkspaceLinkEdgeCasesAndInvariants(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeA, _ := w.AddNode(&workspaceNode{}, "example.com/NodeA", "")
	nodeB, _ := w.AddNode(&workspaceNode{}, "example.com/NodeB", "")
	nodeC, _ := w.AddNode(&workspaceNode{}, "example.com/NodeC", "")

	aLeft := mustAddPort(t, w, nodeA, "left", "example.com/typeA")
	aRight := mustAddPort(t, w, nodeA, "right", "example.com/typeA")
	bLeft := mustAddPort(t, w, nodeB, "left", "example.com/typeA")
	bRight := mustAddPort(t, w, nodeB, "right", "example.com/typeA")
	cLeft := mustAddPort(t, w, nodeC, "left", "example.com/typeA")
	cRight := mustAddPort(t, w, nodeC, "right", "example.com/typeA")

	if _, _, err := w.AddLink(999, bRight); !errors.Is(err, pasta.ErrNoPort) {
		t.Fatalf("missing first port error = %v, want %v", err, pasta.ErrNoPort)
	}
	if _, _, err := w.AddLink(aLeft, 999); !errors.Is(err, pasta.ErrNoPort) {
		t.Fatalf("missing second port error = %v, want %v", err, pasta.ErrNoPort)
	}
	if _, _, err := w.AddLink(aLeft, aRight); !errors.Is(err, pasta.ErrCycle) {
		t.Fatalf("same node link error = %v, want %v", err, pasta.ErrCycle)
	}
	if _, _, err := w.AddLink(aLeft, bLeft); !errors.Is(err, pasta.ErrSameDirection) {
		t.Fatalf("same direction link error = %v, want %v", err, pasta.ErrSameDirection)
	}

	incompatible := mustAddPort(t, w, nodeB, "right", "example.com/typeB")
	if _, _, err := w.AddLink(aLeft, incompatible); !errors.Is(err, pasta.ErrTypeCompat) {
		t.Fatalf("incompatible type error = %v, want %v", err, pasta.ErrTypeCompat)
	}

	first, _, err := w.AddLink(aLeft, bRight)
	if err != nil {
		t.Fatalf("AddLink A->B: %v", err)
	}
	if first == 0 || !w.PortsConnected(aLeft, bRight) {
		t.Fatalf("A->B link not registered, id=%d connected=%v", first, w.PortsConnected(aLeft, bRight))
	}
	if _, _, err := w.AddLink(aLeft, bRight); !errors.Is(err, pasta.ErrLinkDup) {
		t.Fatalf("duplicate link error = %v, want %v", err, pasta.ErrLinkDup)
	}

	if _, _, err := w.AddLink(bLeft, cRight); err != nil {
		t.Fatalf("AddLink B->C: %v", err)
	}
	if _, _, err := w.AddLink(cLeft, aRight); !errors.Is(err, pasta.ErrCycle) {
		t.Fatalf("cycle link error = %v, want %v", err, pasta.ErrCycle)
	}
	if w.PortsConnected(cLeft, aRight) {
		t.Fatal("ports are connected after rejected cycle")
	}
}

func mustAddPort(t *testing.T, w *pasta.Workspace, node uint64, direction, typ string) uint64 {
	t.Helper()

	id, err := w.AddPort(pasta.Port{
		Node:      node,
		Direction: direction,
		Types:     []string{typ},
	})
	if err != nil {
		t.Fatalf("AddPort(%d, %s, %s): %v", node, direction, typ, err)
	}
	return id
}
