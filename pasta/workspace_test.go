package pasta_test

import (
	"encoding/json"
	"errors"
	"reflect"
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
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{})

	nodeA := &workspaceNode{}
	nodeB := &workspaceNode{}

	nodeAID, err := w.AddNode(nodeA, "example.com/NodeA", "")
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {Class: "example.com/NodeA"},
		},
	})
	nodeBID, err := w.AddNode(nodeB, "example.com/NodeB", "")
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {Class: "example.com/NodeA"},
			nodeBID: {Class: "example.com/NodeB"},
		},
	})

	left, err := w.AddPort(pasta.Port{
		Node:      nodeAID,
		Direction: "left",
		Types:     []string{"example.com/typeA"},
	})
	if err != nil {
		t.Fatalf("AddPort left: %v", err)
	}
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {Class: "example.com/NodeA", LeftPorts: []uint64{left}},
			nodeBID: {Class: "example.com/NodeB"},
		},
		Ports: map[uint64]pasta.PortSnapshot{
			left: {Node: nodeAID, Direction: "left", Types: []string{"example.com/typeA"}},
		},
	})
	right, err := w.AddPort(pasta.Port{
		Node:      nodeBID,
		Direction: "right",
		Types:     []string{"example.com/typeA"},
	})
	if err != nil {
		t.Fatalf("AddPort right: %v", err)
	}
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {Class: "example.com/NodeA", LeftPorts: []uint64{left}},
			nodeBID: {Class: "example.com/NodeB", RightPorts: []uint64{right}},
		},
		Ports: map[uint64]pasta.PortSnapshot{
			left:  {Node: nodeAID, Direction: "left", Types: []string{"example.com/typeA"}},
			right: {Node: nodeBID, Direction: "right", Types: []string{"example.com/typeA"}},
		},
	})

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
	withLink := pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {Class: "example.com/NodeA", LeftPorts: []uint64{left}},
			nodeBID: {Class: "example.com/NodeB", RightPorts: []uint64{right}},
		},
		Ports: map[uint64]pasta.PortSnapshot{
			left:  {Node: nodeAID, Direction: "left", Types: []string{"example.com/typeA"}, Links: []uint64{link}},
			right: {Node: nodeBID, Direction: "right", Types: []string{"example.com/typeA"}, Links: []uint64{link}},
		},
		Links: map[uint64]pasta.LinkSnapshot{
			link: {
				Type:          "example.com/typeA",
				LeftPort:      left,
				LeftPortNode:  nodeAID,
				RightPort:     right,
				RightPortNode: nodeBID,
			},
		},
	}
	assertWorkspaceSnapshot(t, w, withLink)
	assertJSONSerializable(t, w.Snapshot())

	nodeSnapshot, ok := w.NodeSnapshot(nodeAID)
	if !ok || !equalNodeSnapshot(nodeSnapshot, withLink.Nodes[nodeAID]) {
		t.Fatalf("NodeSnapshot(%d) = %#v, %v; want %#v, true", nodeAID, nodeSnapshot, ok, withLink.Nodes[nodeAID])
	}
	portSnapshot, ok := w.PortSnapshot(left)
	if !ok || !equalPortSnapshot(portSnapshot, withLink.Ports[left]) {
		t.Fatalf("PortSnapshot(%d) = %#v, %v; want %#v, true", left, portSnapshot, ok, withLink.Ports[left])
	}
	linkSnapshot, ok := w.LinkSnapshot(link)
	if !ok || !reflect.DeepEqual(linkSnapshot, withLink.Links[link]) {
		t.Fatalf("LinkSnapshot(%d) = %#v, %v; want %#v, true", link, linkSnapshot, ok, withLink.Links[link])
	}

	portSnapshot.Types[0] = "example.com/changed"
	portSnapshot.Links[0] = 999
	assertWorkspaceSnapshot(t, w, withLink)

	w.RemoveLink(link)
	if w.PortsConnected(left, right) {
		t.Fatal("ports are connected after RemoveLink")
	}
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {Class: "example.com/NodeA", LeftPorts: []uint64{left}},
			nodeBID: {Class: "example.com/NodeB", RightPorts: []uint64{right}},
		},
		Ports: map[uint64]pasta.PortSnapshot{
			left:  {Node: nodeAID, Direction: "left", Types: []string{"example.com/typeA"}},
			right: {Node: nodeBID, Direction: "right", Types: []string{"example.com/typeA"}},
		},
	})
	w.RemovePort(left)
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {Class: "example.com/NodeA"},
			nodeBID: {Class: "example.com/NodeB", RightPorts: []uint64{right}},
		},
		Ports: map[uint64]pasta.PortSnapshot{
			right: {Node: nodeBID, Direction: "right", Types: []string{"example.com/typeA"}},
		},
	})
	w.RemoveNode(nodeBID)
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {Class: "example.com/NodeA"},
		},
	})
	if _, ok := w.NodeSnapshot(nodeBID); ok {
		t.Fatalf("NodeSnapshot(%d) returned removed node", nodeBID)
	}
	if _, ok := w.PortSnapshot(left); ok {
		t.Fatalf("PortSnapshot(%d) returned removed port", left)
	}
	if _, ok := w.LinkSnapshot(link); ok {
		t.Fatalf("LinkSnapshot(%d) returned removed link", link)
	}

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
	empty := pasta.WorkspaceSnapshot{}

	if _, err := w.AddNode(node, "example.com/node", ""); !errors.Is(err, pasta.ErrClassName) {
		t.Fatalf("invalid class AddNode error = %v, want %v", err, pasta.ErrClassName)
	}
	assertWorkspaceSnapshot(t, w, empty)

	nodeID, err := w.AddNode(node, "example.com/Node", "")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	onlyNode := pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeID: {Class: "example.com/Node"},
		},
	}
	assertWorkspaceSnapshot(t, w, onlyNode)
	if _, err := w.AddNode(node, "example.com/Node", ""); !errors.Is(err, pasta.ErrNodeDup) {
		t.Fatalf("duplicate AddNode error = %v, want %v", err, pasta.ErrNodeDup)
	}
	assertWorkspaceSnapshot(t, w, onlyNode)

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
			assertWorkspaceSnapshot(t, w, onlyNode)
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
	beforeLinks := w.Snapshot()
	if _, _, err := w.AddLink(aLeft, 999); !errors.Is(err, pasta.ErrNoPort) {
		t.Fatalf("missing second port error = %v, want %v", err, pasta.ErrNoPort)
	}
	assertWorkspaceSnapshot(t, w, beforeLinks)
	if _, _, err := w.AddLink(aLeft, aRight); !errors.Is(err, pasta.ErrCycle) {
		t.Fatalf("same node link error = %v, want %v", err, pasta.ErrCycle)
	}
	assertWorkspaceSnapshot(t, w, beforeLinks)
	if _, _, err := w.AddLink(aLeft, bLeft); !errors.Is(err, pasta.ErrSameDirection) {
		t.Fatalf("same direction link error = %v, want %v", err, pasta.ErrSameDirection)
	}
	assertWorkspaceSnapshot(t, w, beforeLinks)

	incompatible := mustAddPort(t, w, nodeB, "right", "example.com/typeB")
	beforeIncompatibleLink := w.Snapshot()
	if _, _, err := w.AddLink(aLeft, incompatible); !errors.Is(err, pasta.ErrTypeCompat) {
		t.Fatalf("incompatible type error = %v, want %v", err, pasta.ErrTypeCompat)
	}
	assertWorkspaceSnapshot(t, w, beforeIncompatibleLink)

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
	afterFirstLink := w.Snapshot()

	if _, _, err := w.AddLink(bLeft, cRight); err != nil {
		t.Fatalf("AddLink B->C: %v", err)
	}
	beforeCycle := w.Snapshot()
	if _, _, err := w.AddLink(cLeft, aRight); !errors.Is(err, pasta.ErrCycle) {
		t.Fatalf("cycle link error = %v, want %v", err, pasta.ErrCycle)
	}
	assertWorkspaceSnapshot(t, w, beforeCycle)
	if w.PortsConnected(cLeft, aRight) {
		t.Fatal("ports are connected after rejected cycle")
	}
	if reflect.DeepEqual(afterFirstLink, beforeCycle) {
		t.Fatal("snapshot did not change after adding B->C link")
	}
}

func assertWorkspaceSnapshot(t *testing.T, w *pasta.Workspace, want pasta.WorkspaceSnapshot) {
	t.Helper()

	got := w.Snapshot()
	if !equalWorkspaceSnapshot(got, want) {
		t.Fatalf("workspace snapshot mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
	assertJSONSerializable(t, got)
}

func assertJSONSerializable(t *testing.T, value any) {
	t.Helper()

	if _, err := json.Marshal(value); err != nil {
		t.Fatalf("snapshot is not JSON serializable: %v", err)
	}
}

func equalWorkspaceSnapshot(a, b pasta.WorkspaceSnapshot) bool {
	if len(a.Nodes) != len(b.Nodes) || len(a.Ports) != len(b.Ports) || len(a.Links) != len(b.Links) {
		return false
	}
	for id, node := range a.Nodes {
		if !equalNodeSnapshot(node, b.Nodes[id]) {
			return false
		}
	}
	for id, port := range a.Ports {
		if !equalPortSnapshot(port, b.Ports[id]) {
			return false
		}
	}
	for id, link := range a.Links {
		if link != b.Links[id] {
			return false
		}
	}
	return true
}

func equalNodeSnapshot(a, b pasta.NodeSnapshot) bool {
	return a.Class == b.Class &&
		a.PrimaryType == b.PrimaryType &&
		reflect.DeepEqual(emptyIfNil(a.LeftPorts), emptyIfNil(b.LeftPorts)) &&
		reflect.DeepEqual(emptyIfNil(a.RightPorts), emptyIfNil(b.RightPorts))
}

func equalPortSnapshot(a, b pasta.PortSnapshot) bool {
	return a.Direction == b.Direction &&
		a.Node == b.Node &&
		a.Name == b.Name &&
		reflect.DeepEqual(emptyStringsIfNil(a.Types), emptyStringsIfNil(b.Types)) &&
		reflect.DeepEqual(emptyIfNil(a.Links), emptyIfNil(b.Links))
}

func emptyIfNil(values []uint64) []uint64 {
	if values == nil {
		return []uint64{}
	}
	return values
}

func emptyStringsIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
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
