package pasta_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/asciimoth/pasta/pasta"
)

type workspaceNode struct {
	l           pasta.Logger
	initPrimary string
	initLabel   string
	rootStatus  []bool
	initData    *pasta.NodeInitData
	readyCount  int
	stopCount   int
}

func (n *workspaceNode) OnInit(w *pasta.Workspace, l pasta.Logger, id uint64, class string, restored *pasta.NodeInitData) error {
	n.l = l
	if restored != nil {
		data := *restored
		data.LeftPorts = slices.Clone(restored.LeftPorts)
		data.RightPorts = slices.Clone(restored.RightPorts)
		n.initData = &data
	}
	l.Debugf("init id=%d class=%s", id, class)
	if n.initPrimary != "" {
		if err := w.SetNodePrimary(id, n.initPrimary); err != nil {
			return err
		}
	}
	if n.initLabel != "" {
		return w.SetNodeLabel(id, n.initLabel)
	}
	return nil
}

func (n *workspaceNode) OnReady() error {
	n.readyCount += 1
	n.l.Debug("ready")
	return nil
}

func (n *workspaceNode) OnRootStatus(hasRootPath bool) error {
	n.rootStatus = append(n.rootStatus, hasRootPath)
	return nil
}

func (n *workspaceNode) OnStop() {
	n.stopCount += 1
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

func (n *workspaceNode) OnEvent(event pasta.Event, linkType string, receiverPortTypes []string, receiverPortDirection string) error {
	n.l.Debugf("event sender=%d:%d receiver=%d:%d type=%s receiver_types=%v receiver_direction=%s payload=%v", event.SenderNode, event.SenderPort, event.ReceiverNode, event.ReceiverPort, linkType, receiverPortTypes, receiverPortDirection, event.Payload)
	return nil
}

func (n *workspaceNode) OnInbox(message pasta.InboxMessage) error {
	n.l.Debugf("inbox receiver=%d payload=%v", message.ReceiverNode, message.Payload)
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

	nodeAID, err := w.AddNode(nodeA, "example.com/NodeA")
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {Class: "example.com/NodeA"},
		},
	})
	nodeBID, err := w.AddNode(nodeB, "example.com/NodeB")
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
	linkByPortsID, linkByPortsSnapshot, ok := w.GetLinkByPorts(right, left)
	if !ok || linkByPortsID != link || !reflect.DeepEqual(linkByPortsSnapshot, withLink.Links[link]) {
		t.Fatalf("GetLinkByPorts(%d, %d) = %d, %#v, %v; want %d, %#v, true", right, left, linkByPortsID, linkByPortsSnapshot, ok, link, withLink.Links[link])
	}
	if !w.NodesConnected(nodeAID, nodeBID) || !w.NodesConnected(nodeBID, nodeAID) {
		t.Fatal("nodes are not connected after AddLink")
	}
	if got := w.GetLinksByNodes(nodeAID, nodeBID); !reflect.DeepEqual(got, withLink.Links) {
		t.Fatalf("GetLinksByNodes(%d, %d) = %#v, want %#v", nodeAID, nodeBID, got, withLink.Links)
	}

	if err := w.SetNodePrimary(nodeAID, "example.com/typeA"); err != nil {
		t.Fatalf("SetNodePrimary: %v", err)
	}
	withLink.Nodes[nodeAID] = pasta.NodeSnapshot{
		Class:       "example.com/NodeA",
		PrimaryType: "example.com/typeA",
		LeftPorts:   []uint64{left},
	}
	assertWorkspaceSnapshot(t, w, withLink)
	if err := w.SetPortName(left, "input"); err != nil {
		t.Fatalf("SetPortName: %v", err)
	}
	withLink.Ports[left] = pasta.PortSnapshot{
		Node:      nodeAID,
		Direction: "left",
		Name:      "input",
		Types:     []string{"example.com/typeA"},
		Links:     []uint64{link},
	}
	assertWorkspaceSnapshot(t, w, withLink)
	if err := w.SetNodePrimary(nodeAID, "example.com/TypeA"); !errors.Is(err, pasta.ErrTypeName) {
		t.Fatalf("SetNodePrimary invalid type error = %v, want %v", err, pasta.ErrTypeName)
	}
	if err := w.SetNodePrimary(999, ""); !errors.Is(err, pasta.ErrNoNode) {
		t.Fatalf("SetNodePrimary missing node error = %v, want %v", err, pasta.ErrNoNode)
	}
	if err := w.SetPortName(999, "missing"); !errors.Is(err, pasta.ErrNoPort) {
		t.Fatalf("SetPortName missing port error = %v, want %v", err, pasta.ErrNoPort)
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
			nodeAID: {Class: "example.com/NodeA", PrimaryType: "example.com/typeA", LeftPorts: []uint64{left}},
			nodeBID: {Class: "example.com/NodeB", RightPorts: []uint64{right}},
		},
		Ports: map[uint64]pasta.PortSnapshot{
			left:  {Node: nodeAID, Direction: "left", Name: "input", Types: []string{"example.com/typeA"}},
			right: {Node: nodeBID, Direction: "right", Types: []string{"example.com/typeA"}},
		},
	})
	if w.NodesConnected(nodeAID, nodeBID) {
		t.Fatal("nodes are connected after RemoveLink")
	}
	if _, _, ok := w.GetLinkByPorts(left, right); ok {
		t.Fatal("GetLinkByPorts returned removed link")
	}
	w.RemovePort(left)
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {Class: "example.com/NodeA", PrimaryType: "example.com/typeA"},
			nodeBID: {Class: "example.com/NodeB", RightPorts: []uint64{right}},
		},
		Ports: map[uint64]pasta.PortSnapshot{
			right: {Node: nodeBID, Direction: "right", Types: []string{"example.com/typeA"}},
		},
	})
	w.RemoveNode(nodeBID)
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {Class: "example.com/NodeA", PrimaryType: "example.com/typeA"},
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

	if _, err := w.AddNode(node, "example.com/node"); !errors.Is(err, pasta.ErrClassName) {
		t.Fatalf("invalid class AddNode error = %v, want %v", err, pasta.ErrClassName)
	}
	assertWorkspaceSnapshot(t, w, empty)

	nodeID, err := w.AddNode(node, "example.com/Node")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	onlyNode := pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeID: {Class: "example.com/Node"},
		},
	}
	assertWorkspaceSnapshot(t, w, onlyNode)
	if _, err := w.AddNode(node, "example.com/Node"); !errors.Is(err, pasta.ErrNodeDup) {
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

func TestWorkspaceClassWideNodeOperations(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeA := &workspaceNode{}
	nodeB := &workspaceNode{}
	nodeC := &workspaceNode{}
	nodeAID, err := w.AddNode(nodeA, "example.com/Target")
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	nodeBID, err := w.AddNode(nodeB, "example.com/Other")
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}
	nodeCID, err := w.AddNode(nodeC, "example.com/Target")
	if err != nil {
		t.Fatalf("AddNode C: %v", err)
	}
	placeholderID, err := w.AddPlaceholderNode("example.com/Target", []pasta.Port{
		{Direction: "right", Types: []string{"example.com/typeA"}},
	})
	if err != nil {
		t.Fatalf("AddPlaceholderNode: %v", err)
	}

	got, err := w.NodesByClass("example.com/Target")
	if err != nil {
		t.Fatalf("NodesByClass: %v", err)
	}
	if want := []uint64{nodeAID, nodeCID, placeholderID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("NodesByClass = %v, want %v", got, want)
	}
	got, err = w.NodesByClass("example.com/Other")
	if err != nil {
		t.Fatalf("NodesByClass other: %v", err)
	}
	if want := []uint64{nodeBID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("NodesByClass other = %v, want %v", got, want)
	}
	if _, err := w.NodesByClass("example.com/target"); !errors.Is(err, pasta.ErrClassName) {
		t.Fatalf("NodesByClass invalid class error = %v, want %v", err, pasta.ErrClassName)
	}

	if err := w.RemoveNodesByClass("example.com/Target"); err != nil {
		t.Fatalf("RemoveNodesByClass: %v", err)
	}
	if got, err := w.NodesByClass("example.com/Target"); err != nil || len(got) != 0 {
		t.Fatalf("NodesByClass target after remove = %v, %v; want empty, nil", got, err)
	}
	if got, err := w.NodesByClass("example.com/Other"); err != nil || !reflect.DeepEqual(got, []uint64{nodeBID}) {
		t.Fatalf("NodesByClass other after remove = %v, %v; want [%d], nil", got, err, nodeBID)
	}
	if nodeA.stopCount != 1 || nodeC.stopCount != 1 {
		t.Fatalf("removed target stop counts = %d, %d; want 1, 1", nodeA.stopCount, nodeC.stopCount)
	}
}

func TestWorkspaceReplaceNodesByClassWithPlaceholders(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeA := &workspaceNode{}
	nodeB := &workspaceNode{}
	nodeC := &workspaceNode{}
	nodeAID, err := w.AddNode(nodeA, "example.com/Target")
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	nodeBID, err := w.AddNode(nodeB, "example.com/Other")
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}
	nodeCID, err := w.AddNode(nodeC, "example.com/Target")
	if err != nil {
		t.Fatalf("AddNode C: %v", err)
	}
	aLeft := mustAddPort(t, w, nodeAID, "left", "example.com/typeA")
	bRight := mustAddPort(t, w, nodeBID, "right", "example.com/typeA")
	link, _, err := w.AddLink(aLeft, bRight)
	if err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	cRight := mustAddPort(t, w, nodeCID, "right", pasta.AnyType)

	if err := w.ReplaceNodesByClassWithPlaceholders("example.com/Target"); err != nil {
		t.Fatalf("ReplaceNodesByClassWithPlaceholders: %v", err)
	}

	snapshot := w.Snapshot()
	if !snapshot.Nodes[nodeAID].Placeholder || !snapshot.Nodes[nodeCID].Placeholder {
		t.Fatalf("target nodes not placeholders: %#v %#v", snapshot.Nodes[nodeAID], snapshot.Nodes[nodeCID])
	}
	if snapshot.Nodes[nodeBID].Placeholder {
		t.Fatalf("non-target node became placeholder: %#v", snapshot.Nodes[nodeBID])
	}
	if !snapshot.Links[link].Placeholder {
		t.Fatalf("preserved link = %#v, want placeholder", snapshot.Links[link])
	}
	if !reflect.DeepEqual(snapshot.Nodes[nodeCID].RightPorts, []uint64{cRight}) {
		t.Fatalf("node C ports = %v, want [%d]", snapshot.Nodes[nodeCID].RightPorts, cRight)
	}
	if nodeA.stopCount != 1 || nodeC.stopCount != 1 || nodeB.stopCount != 0 {
		t.Fatalf("stop counts = %d, %d, %d; want 1, 0, 1", nodeA.stopCount, nodeB.stopCount, nodeC.stopCount)
	}
	if err := w.ReplaceNodesByClassWithPlaceholders("example.com/target"); !errors.Is(err, pasta.ErrClassName) {
		t.Fatalf("ReplaceNodesByClassWithPlaceholders invalid class error = %v, want %v", err, pasta.ErrClassName)
	}
}

func TestWorkspaceNodeCanSetPrimaryTypeInOnInit(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeID, err := w.AddNode(&workspaceNode{initPrimary: "example.com/typeA"}, "example.com/Node")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeID: {Class: "example.com/Node", PrimaryType: "example.com/typeA"},
		},
	})
}

func TestWorkspaceNodeLabelSnapshots(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeID, err := w.AddNode(&workspaceNode{initLabel: "starting"}, "example.com/Node")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeID: {Class: "example.com/Node", Label: "starting"},
		},
	})

	if err := w.SetNodeLabel(nodeID, "ready"); err != nil {
		t.Fatalf("SetNodeLabel: %v", err)
	}
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeID: {Class: "example.com/Node", Label: "ready"},
		},
	})
	if err := w.SetNodeLabel(999, "missing"); !errors.Is(err, pasta.ErrNoNode) {
		t.Fatalf("SetNodeLabel missing node error = %v, want %v", err, pasta.ErrNoNode)
	}
}

func TestWorkspaceTracksRootPaths(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeA := &workspaceNode{}
	nodeB := &workspaceNode{}
	nodeC := &workspaceNode{}

	nodeAID, err := w.AddRootNode(nodeA, "example.com/NodeA")
	if err != nil {
		t.Fatalf("AddRootNode A: %v", err)
	}
	nodeBID, err := w.AddNode(nodeB, "example.com/NodeB")
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}
	nodeCID, err := w.AddNodeWithRoot(nodeC, "example.com/NodeC", false)
	if err != nil {
		t.Fatalf("AddNodeWithRoot C: %v", err)
	}

	if got, want := nodeA.rootStatus, []bool{true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("node A root statuses = %v, want %v", got, want)
	}
	if got, want := nodeB.rootStatus, []bool{false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("node B root statuses = %v, want %v", got, want)
	}
	if got, want := nodeC.rootStatus, []bool{false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("node C root statuses = %v, want %v", got, want)
	}

	aLeft := mustAddPort(t, w, nodeAID, "left", "example.com/typeA")
	bRight := mustAddPort(t, w, nodeBID, "right", "example.com/typeA")
	bLeft := mustAddPort(t, w, nodeBID, "left", "example.com/typeA")
	cRight := mustAddPort(t, w, nodeCID, "right", "example.com/typeA")

	ab, _, err := w.AddLink(aLeft, bRight)
	if err != nil {
		t.Fatalf("AddLink A-B: %v", err)
	}
	if got, want := nodeB.rootStatus, []bool{false, true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("node B root statuses after A-B = %v, want %v", got, want)
	}

	bc, _, err := w.AddLink(bLeft, cRight)
	if err != nil {
		t.Fatalf("AddLink B-C: %v", err)
	}
	if got, want := nodeC.rootStatus, []bool{false, true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("node C root statuses after B-C = %v, want %v", got, want)
	}

	snapshot := w.Snapshot()
	if !snapshot.Nodes[nodeAID].Root || !snapshot.Nodes[nodeAID].HasRootPath {
		t.Fatalf("root node snapshot = %#v, want root with root path", snapshot.Nodes[nodeAID])
	}
	if !snapshot.Nodes[nodeBID].HasRootPath || !snapshot.Nodes[nodeCID].HasRootPath {
		t.Fatalf("connected node snapshots = %#v %#v, want root paths", snapshot.Nodes[nodeBID], snapshot.Nodes[nodeCID])
	}

	w.RemoveLink(bc)
	if got, want := nodeC.rootStatus, []bool{false, true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("node C root statuses after removing B-C = %v, want %v", got, want)
	}

	if err := w.SetNodeRoot(nodeAID, false); err != nil {
		t.Fatalf("SetNodeRoot A false: %v", err)
	}
	if got, want := nodeA.rootStatus, []bool{true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("node A root statuses after clearing root = %v, want %v", got, want)
	}
	if got, want := nodeB.rootStatus, []bool{false, true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("node B root statuses after clearing A root = %v, want %v", got, want)
	}

	if err := w.SetNodeRoot(nodeCID, true); err != nil {
		t.Fatalf("SetNodeRoot C true: %v", err)
	}
	if got, want := nodeC.rootStatus, []bool{false, true, false, true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("node C root statuses after setting C root = %v, want %v", got, want)
	}

	w.RemoveLink(ab)
	if got, want := nodeB.rootStatus, []bool{false, true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("node B root statuses after removing A-B = %v, want %v", got, want)
	}
	if err := w.SetNodeRoot(999, true); !errors.Is(err, pasta.ErrNoNode) {
		t.Fatalf("SetNodeRoot missing node error = %v, want %v", err, pasta.ErrNoNode)
	}
}

func TestWorkspaceReplaceNodePreservesRecordState(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	legacy := &workspaceNode{}
	replacement := &workspaceNode{}

	nodeID, err := w.AddRootNode(legacy, "example.com/Node")
	if err != nil {
		t.Fatalf("AddRootNode: %v", err)
	}
	left := mustAddPort(t, w, nodeID, "left", "example.com/typeA")
	right := mustAddPort(t, w, nodeID, "right", "example.com/typeA")
	if err := w.SetNodePrimary(nodeID, "example.com/typeA"); err != nil {
		t.Fatalf("SetNodePrimary: %v", err)
	}
	if err := w.SetNodeLabel(nodeID, "active"); err != nil {
		t.Fatalf("SetNodeLabel: %v", err)
	}

	before := w.Snapshot()
	var notifications []pasta.WorkspaceNotification
	w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		notifications = append(notifications, notification)
	})
	notifications = nil

	if err := w.ReplaceNode(nodeID, replacement); err != nil {
		t.Fatalf("ReplaceNode: %v", err)
	}

	if legacy.stopCount != 1 {
		t.Fatalf("legacy stop count = %d, want 1", legacy.stopCount)
	}
	if replacement.readyCount != 1 {
		t.Fatalf("replacement ready count = %d, want 1", replacement.readyCount)
	}
	if got, want := replacement.rootStatus, []bool{true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("replacement root status = %v, want %v", got, want)
	}
	if replacement.initData == nil {
		t.Fatal("replacement OnInit did not receive restored data")
	}
	if replacement.initData.PrimaryType != "example.com/typeA" {
		t.Fatalf("restored primary type = %q, want example.com/typeA", replacement.initData.PrimaryType)
	}
	if replacement.initData.Label != "active" {
		t.Fatalf("restored label = %q, want active", replacement.initData.Label)
	}
	if !reflect.DeepEqual(replacement.initData.LeftPorts, []uint64{left}) {
		t.Fatalf("restored left ports = %v, want [%d]", replacement.initData.LeftPorts, left)
	}
	if !reflect.DeepEqual(replacement.initData.RightPorts, []uint64{right}) {
		t.Fatalf("restored right ports = %v, want [%d]", replacement.initData.RightPorts, right)
	}
	assertWorkspaceSnapshot(t, w, before)
	if len(notifications) != 0 {
		t.Fatalf("replacement emitted notifications: %#v", notifications)
	}
}

func TestWorkspaceReplaceNodeRejectsMissingAndDuplicateNodes(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeA := &workspaceNode{}
	nodeB := &workspaceNode{}
	nodeAID, err := w.AddNode(nodeA, "example.com/NodeA")
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	if _, err := w.AddNode(nodeB, "example.com/NodeB"); err != nil {
		t.Fatalf("AddNode B: %v", err)
	}

	if err := w.ReplaceNode(999, &workspaceNode{}); !errors.Is(err, pasta.ErrNoNode) {
		t.Fatalf("ReplaceNode missing error = %v, want %v", err, pasta.ErrNoNode)
	}
	if err := w.ReplaceNode(nodeAID, nodeA); !errors.Is(err, pasta.ErrNodeDup) {
		t.Fatalf("ReplaceNode same node error = %v, want %v", err, pasta.ErrNodeDup)
	}
	if err := w.ReplaceNode(nodeAID, nodeB); !errors.Is(err, pasta.ErrNodeDup) {
		t.Fatalf("ReplaceNode duplicate error = %v, want %v", err, pasta.ErrNodeDup)
	}
}

func TestWorkspacePlaceholderNodeLifecycleAndSnapshots(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	root := &workspaceNode{}
	rootID, err := w.AddRootNode(root, "example.com/Root")
	if err != nil {
		t.Fatalf("AddRootNode: %v", err)
	}
	rootLeft := mustAddPort(t, w, rootID, "left", "example.com/typeA")

	placeholderID, err := w.AddPlaceholderNodeWithRoot("example.com/Missing", true, []pasta.Port{
		{Direction: "right", Types: []string{"example.com/typeA"}},
	})
	if err != nil {
		t.Fatalf("AddPlaceholderNodeWithRoot: %v", err)
	}
	placeholder := w.Snapshot().Nodes[placeholderID]
	if !placeholder.Placeholder || placeholder.Root || placeholder.HasRootPath {
		t.Fatalf("placeholder snapshot = %#v, want placeholder with false root state", placeholder)
	}
	if err := w.SetNodePrimary(placeholderID, "example.com/typeA"); err != nil {
		t.Fatalf("SetNodePrimary placeholder: %v", err)
	}
	if snapshot := w.Snapshot().Nodes[placeholderID]; snapshot.PrimaryType != "" {
		t.Fatalf("placeholder primary type snapshot = %q, want empty", snapshot.PrimaryType)
	}
	if err := w.SetNodeLabel(placeholderID, "missing"); err != nil {
		t.Fatalf("SetNodeLabel placeholder: %v", err)
	}
	if snapshot := w.Snapshot().Nodes[placeholderID]; snapshot.Label != "missing" {
		t.Fatalf("placeholder label snapshot = %q, want missing", snapshot.Label)
	}

	placeholderRight := placeholder.RightPorts[0]
	link, _, err := w.AddLink(rootLeft, placeholderRight)
	if err != nil {
		t.Fatalf("AddLink to placeholder: %v", err)
	}
	if got := len(root.rootStatus); got != 1 {
		t.Fatalf("root status callback count after placeholder link = %d, want 1", got)
	}
	linkSnapshot, ok := w.LinkSnapshot(link)
	if !ok || !linkSnapshot.Placeholder {
		t.Fatalf("placeholder link snapshot = %#v, %v; want placeholder", linkSnapshot, ok)
	}

	replacement := &workspaceNode{}
	if err := w.ReplacePlaceholderNode(placeholderID, replacement); err != nil {
		t.Fatalf("ReplacePlaceholderNode: %v", err)
	}
	if replacement.initData == nil || !reflect.DeepEqual(replacement.initData.RightPorts, []uint64{placeholderRight}) {
		t.Fatalf("replacement init data = %#v, want preserved right port %d", replacement.initData, placeholderRight)
	}
	if replacement.initData.Label != "missing" {
		t.Fatalf("replacement init label = %q, want missing", replacement.initData.Label)
	}
	if got, want := replacement.rootStatus, []bool{true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("replacement root status = %v, want %v", got, want)
	}
	linkSnapshot, ok = w.LinkSnapshot(link)
	if !ok || linkSnapshot.Placeholder {
		t.Fatalf("restored link snapshot = %#v, %v; want non-placeholder", linkSnapshot, ok)
	}
	nodeSnapshot := w.Snapshot().Nodes[placeholderID]
	if nodeSnapshot.Placeholder || !nodeSnapshot.Root || !nodeSnapshot.HasRootPath || nodeSnapshot.PrimaryType != "example.com/typeA" || nodeSnapshot.Label != "missing" {
		t.Fatalf("restored node snapshot = %#v, want normal restored state", nodeSnapshot)
	}
}

func TestWorkspaceReplaceNodeWithPlaceholderPreservesLinksButRemovesCallbacks(t *testing.T) {
	logf := &StringLoggerFactory{}
	w := pasta.NewWorkspace(logf)

	nodeA := &workspaceNode{}
	nodeB := &workspaceNode{}
	nodeAID, err := w.AddRootNode(nodeA, "example.com/NodeA")
	if err != nil {
		t.Fatalf("AddRootNode A: %v", err)
	}
	nodeBID, err := w.AddNode(nodeB, "example.com/NodeB")
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}
	left := mustAddPort(t, w, nodeAID, "left", "example.com/typeA")
	right := mustAddPort(t, w, nodeBID, "right", "example.com/typeA")
	link, _, err := w.AddLink(left, right)
	if err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	nodeB.rootStatus = nil

	var notifications []pasta.WorkspaceNotification
	w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		notifications = append(notifications, notification)
	})
	notifications = nil

	if err := w.ReplaceNodeWithPlaceholder(nodeBID, []pasta.Port{
		{Direction: "right", Types: []string{pasta.AnyType}},
	}); err != nil {
		t.Fatalf("ReplaceNodeWithPlaceholder: %v", err)
	}
	assertHasNotification(t, notifications, pasta.NotificationLinkUpdated, link)
	assertHasNotification(t, notifications, pasta.NotificationNodeUpdated, nodeBID)
	if !strings.Contains(logf.Result(), "example.com/NodeA[debug]link removed") {
		t.Fatalf("replacement did not notify live peer with OnLinkRemoved; logs:\n%s", logf.Result())
	}
	if nodeB.stopCount != 1 {
		t.Fatalf("replaced node stop count = %d, want 1", nodeB.stopCount)
	}
	if snapshot := w.Snapshot(); !snapshot.Nodes[nodeBID].Placeholder || !snapshot.Links[link].Placeholder {
		t.Fatalf("placeholder replacement snapshot = %#v", snapshot)
	}
	if !w.PortsConnected(left, right) || !w.NodesConnected(nodeAID, nodeBID) {
		t.Fatal("placeholder replacement did not preserve indexed connection")
	}
	if got, want := nodeA.rootStatus, []bool{true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("node A root statuses = %v, want %v", got, want)
	}

	extraRight := w.Snapshot().Nodes[nodeBID].RightPorts[1]
	extraLeft := mustAddPort(t, w, nodeAID, "left", pasta.AnyType)
	beforePlaceholderLink := logf.Result()
	if _, _, err := w.AddLink(extraLeft, extraRight); err != nil {
		t.Fatalf("AddLink to placeholder extra port: %v", err)
	}
	if strings.Contains(strings.TrimPrefix(logf.Result(), beforePlaceholderLink), "pre link add") ||
		strings.Contains(strings.TrimPrefix(logf.Result(), beforePlaceholderLink), "[debug]link add ") {
		t.Fatalf("placeholder link attachment called add callbacks; logs:\n%s", strings.TrimPrefix(logf.Result(), beforePlaceholderLink))
	}
	if got := len(nodeA.rootStatus); got != 1 {
		t.Fatalf("root status callback count after new placeholder link = %d, want 1", got)
	}
}

func TestWorkspacePlaceholderLinksParticipateInDAGChecks(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeAID, err := w.AddNode(&workspaceNode{}, "example.com/NodeA")
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	placeholderID, err := w.AddPlaceholderNode("example.com/Missing", []pasta.Port{
		{Direction: "left", Types: []string{"example.com/typeA"}},
		{Direction: "right", Types: []string{"example.com/typeA"}},
	})
	if err != nil {
		t.Fatalf("AddPlaceholderNode: %v", err)
	}

	aLeft := mustAddPort(t, w, nodeAID, "left", "example.com/typeA")
	aRight := mustAddPort(t, w, nodeAID, "right", "example.com/typeA")
	placeholder := w.Snapshot().Nodes[placeholderID]
	pLeft := placeholder.LeftPorts[0]
	pRight := placeholder.RightPorts[0]

	if _, _, err := w.AddLink(aLeft, pRight); err != nil {
		t.Fatalf("AddLink A->placeholder: %v", err)
	}
	beforeCycle := w.Snapshot()
	if _, _, err := w.AddLink(pLeft, aRight); !errors.Is(err, pasta.ErrCycle) {
		t.Fatalf("placeholder cycle error = %v, want %v", err, pasta.ErrCycle)
	}
	assertWorkspaceSnapshot(t, w, beforeCycle)
}

func TestWorkspaceSetNodePortOrder(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeID, err := w.AddNode(&workspaceNode{}, "example.com/Node")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	leftA := mustAddPort(t, w, nodeID, "left", "example.com/typeA")
	leftB := mustAddPort(t, w, nodeID, "left", "example.com/typeA")
	leftC := mustAddPort(t, w, nodeID, "left", "example.com/typeA")
	rightA := mustAddPort(t, w, nodeID, "right", "example.com/typeA")
	rightB := mustAddPort(t, w, nodeID, "right", "example.com/typeA")

	if err := w.SetNodePortOrder(nodeID, "left", []uint64{leftC, leftA, leftB}); err != nil {
		t.Fatalf("SetNodePortOrder left: %v", err)
	}
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeID: {
				Class:      "example.com/Node",
				LeftPorts:  []uint64{leftC, leftA, leftB},
				RightPorts: []uint64{rightA, rightB},
			},
		},
		Ports: map[uint64]pasta.PortSnapshot{
			leftA:  {Node: nodeID, Direction: "left", Types: []string{"example.com/typeA"}},
			leftB:  {Node: nodeID, Direction: "left", Types: []string{"example.com/typeA"}},
			leftC:  {Node: nodeID, Direction: "left", Types: []string{"example.com/typeA"}},
			rightA: {Node: nodeID, Direction: "right", Types: []string{"example.com/typeA"}},
			rightB: {Node: nodeID, Direction: "right", Types: []string{"example.com/typeA"}},
		},
	})

	beforeInvalid := w.Snapshot()
	if err := w.SetNodePortOrder(nodeID, "up", []uint64{leftA, leftB, leftC}); !errors.Is(err, pasta.ErrPortDirection) {
		t.Fatalf("SetNodePortOrder bad direction error = %v, want %v", err, pasta.ErrPortDirection)
	}
	if err := w.SetNodePortOrder(nodeID, "left", []uint64{leftA, leftA, leftC}); !errors.Is(err, pasta.ErrPortOrder) {
		t.Fatalf("SetNodePortOrder duplicate error = %v, want %v", err, pasta.ErrPortOrder)
	}
	if err := w.SetNodePortOrder(nodeID, "left", []uint64{leftA, leftB}); !errors.Is(err, pasta.ErrPortOrder) {
		t.Fatalf("SetNodePortOrder missing error = %v, want %v", err, pasta.ErrPortOrder)
	}
	if err := w.SetNodePortOrder(nodeID, "left", []uint64{rightA, leftA, leftB}); !errors.Is(err, pasta.ErrPortOrder) {
		t.Fatalf("SetNodePortOrder wrong side error = %v, want %v", err, pasta.ErrPortOrder)
	}
	assertWorkspaceSnapshot(t, w, beforeInvalid)

	if err := w.SetNodePortsOrder(nodeID, []uint64{leftB, leftC, leftA}, []uint64{rightB, rightA}); err != nil {
		t.Fatalf("SetNodePortsOrder: %v", err)
	}
	afterBoth := w.Snapshot()
	if !reflect.DeepEqual(afterBoth.Nodes[nodeID].LeftPorts, []uint64{leftB, leftC, leftA}) {
		t.Fatalf("left port order = %v", afterBoth.Nodes[nodeID].LeftPorts)
	}
	if !reflect.DeepEqual(afterBoth.Nodes[nodeID].RightPorts, []uint64{rightB, rightA}) {
		t.Fatalf("right port order = %v", afterBoth.Nodes[nodeID].RightPorts)
	}

	if err := w.SetNodePortsOrder(nodeID, []uint64{leftA, leftB, leftC}, []uint64{rightA, rightA}); !errors.Is(err, pasta.ErrPortOrder) {
		t.Fatalf("SetNodePortsOrder invalid right error = %v, want %v", err, pasta.ErrPortOrder)
	}
	assertWorkspaceSnapshot(t, w, afterBoth)
	if err := w.SetNodePortOrder(999, "left", nil); !errors.Is(err, pasta.ErrNoNode) {
		t.Fatalf("SetNodePortOrder missing node error = %v, want %v", err, pasta.ErrNoNode)
	}

	w.Close()
	if err := w.SetNodePortOrder(nodeID, "left", []uint64{leftA, leftB, leftC}); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("SetNodePortOrder after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.SetNodePortsOrder(nodeID, []uint64{leftA, leftB, leftC}, []uint64{rightA, rightB}); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("SetNodePortsOrder after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
}

func TestWorkspaceAnyPortTypeLinksToAnyOtherType(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeA, err := w.AddNode(&workspaceNode{}, "example.com/NodeA")
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	nodeB, err := w.AddNode(&workspaceNode{}, "example.com/NodeB")
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}
	nodeC, err := w.AddNode(&workspaceNode{}, "example.com/NodeC")
	if err != nil {
		t.Fatalf("AddNode C: %v", err)
	}
	nodeD, err := w.AddNode(&workspaceNode{}, "example.com/NodeD")
	if err != nil {
		t.Fatalf("AddNode D: %v", err)
	}

	anyLeft := mustAddPort(t, w, nodeA, "left", pasta.AnyType)
	singleRight := mustAddPort(t, w, nodeB, "right", "example.com/typeA")
	anyRight := mustAddPort(t, w, nodeC, "right", pasta.AnyType)
	multiRight, err := w.AddPort(pasta.Port{
		Node:      nodeD,
		Direction: "right",
		Types:     []string{"example.com/typeB", "example.com/typeC"},
	})
	if err != nil {
		t.Fatalf("AddPort multi right: %v", err)
	}

	_, linkType, err := w.AddLink(anyLeft, singleRight)
	if err != nil {
		t.Fatalf("AddLink any-single: %v", err)
	}
	if linkType != "example.com/typeA" {
		t.Fatalf("any-single link type = %q, want example.com/typeA", linkType)
	}

	_, linkType, err = w.AddLink(anyLeft, anyRight)
	if err != nil {
		t.Fatalf("AddLink any-any: %v", err)
	}
	if linkType != pasta.AnyType {
		t.Fatalf("any-any link type = %q, want %q", linkType, pasta.AnyType)
	}

	_, linkType, err = w.AddLink(anyLeft, multiRight)
	if err != nil {
		t.Fatalf("AddLink any-multi: %v", err)
	}
	if linkType != pasta.AnyType {
		t.Fatalf("any-multi link type = %q, want %q", linkType, pasta.AnyType)
	}

	nodeE, err := w.AddNode(&workspaceNode{}, "example.com/NodeE")
	if err != nil {
		t.Fatalf("AddNode E: %v", err)
	}
	nodeF, err := w.AddNode(&workspaceNode{}, "example.com/NodeF")
	if err != nil {
		t.Fatalf("AddNode F: %v", err)
	}
	multiLeft, err := w.AddPort(pasta.Port{
		Node:      nodeE,
		Direction: "left",
		Types:     []string{"example.com/typeD", "example.com/typeE"},
	})
	if err != nil {
		t.Fatalf("AddPort multi left: %v", err)
	}
	singleAnyRight, err := w.AddPort(pasta.Port{
		Node:      nodeF,
		Direction: "right",
		Types:     []string{pasta.AnyType, "example.com/typeF"},
	})
	if err != nil {
		t.Fatalf("AddPort any mixed right: %v", err)
	}
	_, linkType, err = w.AddLink(multiLeft, singleAnyRight)
	if err != nil {
		t.Fatalf("AddLink multi-any: %v", err)
	}
	if linkType != pasta.AnyType {
		t.Fatalf("multi-any link type = %q, want %q", linkType, pasta.AnyType)
	}
}

func TestWorkspaceCloseStopsNodesNotifiesAndRejectsOperations(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeA := &workspaceNode{}
	nodeB := &workspaceNode{}
	nodeAID, err := w.AddNode(nodeA, "example.com/NodeA")
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	nodeBID, err := w.AddNode(nodeB, "example.com/NodeB")
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}
	portA := mustAddPort(t, w, nodeAID, "left", "example.com/typeA")
	portB := mustAddPort(t, w, nodeBID, "right", "example.com/typeA")
	link, _, err := w.AddLink(portA, portB)
	if err != nil {
		t.Fatalf("AddLink: %v", err)
	}

	var notifications []pasta.WorkspaceNotification
	subscriptionID := w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		notifications = append(notifications, notification)
	})
	notifications = nil

	w.Close()
	w.Close()

	if nodeA.stopCount != 1 || nodeB.stopCount != 1 {
		t.Fatalf("stop counts = %d, %d; want 1, 1", nodeA.stopCount, nodeB.stopCount)
	}
	assertNotificationMatches(t, notifications, []notificationMatch{
		{kind: pasta.NotificationWorkspaceStopped},
	})
	if w.UnsubscribeNotifications(subscriptionID) {
		t.Fatal("UnsubscribeNotifications after Close returned true")
	}
	if got := w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		t.Fatalf("SubscribeNotifications after Close delivered %#v", notification)
	}); got != 0 {
		t.Fatalf("SubscribeNotifications after Close = %d, want 0", got)
	}

	if _, err := w.AddNode(&workspaceNode{}, "example.com/NodeC"); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("AddNode after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if _, err := w.AddRootNode(&workspaceNode{}, "example.com/NodeC"); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("AddRootNode after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.ReplaceNode(nodeAID, &workspaceNode{}); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("ReplaceNode after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if _, err := w.AddPort(pasta.Port{Node: nodeAID, Direction: "left", Types: []string{"example.com/typeA"}}); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("AddPort after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if _, _, err := w.AddLink(portA, portB); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("AddLink after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.SetNodePrimary(nodeAID, "example.com/typeA"); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("SetNodePrimary after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.SetNodeLabel(nodeAID, "closed"); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("SetNodeLabel after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.SetNodeRoot(nodeAID, true); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("SetNodeRoot after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.SetNodePortOrder(nodeAID, "left", []uint64{portA}); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("SetNodePortOrder after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.SetNodePortsOrder(nodeAID, []uint64{portA}, nil); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("SetNodePortsOrder after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.SetPortName(portA, "input"); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("SetPortName after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.RemoveNodesByClass("example.com/NodeA"); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("RemoveNodesByClass after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.ReplaceNodesByClassWithPlaceholders("example.com/NodeA"); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("ReplaceNodesByClassWithPlaceholders after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if got, err := w.NodesByClass("example.com/NodeA"); err != nil || len(got) != 0 {
		t.Fatalf("NodesByClass after Close = %v, %v; want empty, nil", got, err)
	}

	w.RemoveLink(link)
	w.RemovePort(portA)
	w.RemoveNode(nodeAID)
	w.RemoveLinksByNodes(nodeAID, nodeBID)
	w.Ready()
	w.AddPendingOp(func() {
		t.Fatal("AddPendingOp after Close ran")
	})
	w.SendEvent(pasta.Event{SenderNode: nodeAID, SenderPort: portA, ReceiverNode: nodeBID, ReceiverPort: portB})
	w.SendInbox(pasta.InboxMessage{ReceiverNode: nodeAID, Payload: "ignored"})

	if w.IsReady() {
		t.Fatal("closed workspace reports ready")
	}
	if got := w.NextID(); got != 0 {
		t.Fatalf("NextID after Close = %d, want 0", got)
	}
	if got := w.Snapshot(); !equalWorkspaceSnapshot(got, pasta.WorkspaceSnapshot{}) {
		t.Fatalf("Snapshot after Close = %#v, want empty", got)
	}
	if _, ok := w.NodeSnapshot(nodeAID); ok {
		t.Fatal("NodeSnapshot after Close returned ok")
	}
	if _, ok := w.PortSnapshot(portA); ok {
		t.Fatal("PortSnapshot after Close returned ok")
	}
	if _, _, ok := w.LinkByPorts(portA, portB); ok {
		t.Fatal("LinkByPorts after Close returned ok")
	}
	if w.PortsConnected(portA, portB) || w.NodesConnected(nodeAID, nodeBID) {
		t.Fatal("closed workspace reports connections")
	}
	if links := w.LinksByNodes(nodeAID, nodeBID); len(links) != 0 {
		t.Fatalf("LinksByNodes after Close = %#v, want empty", links)
	}
	if len(notifications) != 1 {
		t.Fatalf("notifications after post-close operations = %d, want 1", len(notifications))
	}
}

func TestWorkspaceLinkEdgeCasesAndInvariants(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeA, _ := w.AddNode(&workspaceNode{}, "example.com/NodeA")
	nodeB, _ := w.AddNode(&workspaceNode{}, "example.com/NodeB")
	nodeC, _ := w.AddNode(&workspaceNode{}, "example.com/NodeC")

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
	w.RemoveLinksByNodes(nodeB, nodeC)
	if w.NodesConnected(nodeB, nodeC) {
		t.Fatal("nodes are connected after RemoveLinksByNodes")
	}
	if got := w.LinksByNodes(nodeB, nodeC); len(got) != 0 {
		t.Fatalf("LinksByNodes after RemoveLinksByNodes = %#v, want empty", got)
	}
	if _, _, err := w.AddLink(bLeft, cRight); err != nil {
		t.Fatalf("AddLink B->C again: %v", err)
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

func assertHasNotification(t *testing.T, notifications []pasta.WorkspaceNotification, kind pasta.NotificationKind, id uint64) {
	t.Helper()

	for _, notification := range notifications {
		if notification.Kind == kind && notification.ID == id {
			return
		}
	}
	t.Fatalf("missing notification {%q, %d} in %#v", kind, id, notifications)
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
		a.Label == b.Label &&
		a.Placeholder == b.Placeholder &&
		a.Root == b.Root &&
		a.HasRootPath == b.HasRootPath &&
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
