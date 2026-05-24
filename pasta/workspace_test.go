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
		return w.SetNodePrimary(id, n.initPrimary)
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
