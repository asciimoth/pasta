package pasta_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

type workspaceNode struct {
	l            pasta.Logger
	initPrimary  string
	initLabel    string
	failOn       map[string]error
	panicOn      map[string]bool
	rootStatus   []bool
	initData     *pasta.NodeInitData
	initFlags    workspaceNodeInitFlags
	readyCount   int
	stopCount    int
	triggerCount int
}

type workspaceNodeInitFlags struct {
	isReplacement            bool
	isPlaceholderReplacement bool
	isClassConstructed       bool
	isRestored               bool
}

func (n *workspaceNode) OnInit(w *pasta.Workspace, l pasta.Logger, id uint64, class string, restored *pasta.NodeInitData, isReplacement bool, isPlaceholderReplacement bool, isClassConstructed bool, isRestored bool) error {
	n.l = l
	n.initFlags = workspaceNodeInitFlags{
		isReplacement:            isReplacement,
		isPlaceholderReplacement: isPlaceholderReplacement,
		isClassConstructed:       isClassConstructed,
		isRestored:               isRestored,
	}
	if restored != nil {
		data := *restored
		data.LeftPorts = slices.Clone(restored.LeftPorts)
		data.RightPorts = slices.Clone(restored.RightPorts)
		n.initData = &data
	}
	l.Debugf("init id=%d class=%s", id, class)
	if n.initPrimary != "" {
		if err := w.SetNodePrimaryLocked(id, n.initPrimary); err != nil {
			return err
		}
	}
	if n.initLabel != "" {
		return w.SetNodeLabelLocked(id, n.initLabel)
	}
	return n.maybeFail("OnInit")
}

func (n *workspaceNode) OnReady() error {
	n.readyCount += 1
	n.l.Debug("ready")
	return n.maybeFail("OnReady")
}

func (n *workspaceNode) OnRootStatus(hasRootPath bool) error {
	n.rootStatus = append(n.rootStatus, hasRootPath)
	return n.maybeFail("OnRootStatus")
}

func (n *workspaceNode) OnStop() {
	n.stopCount += 1
	n.l.Debug("stop")
}

func (n *workspaceNode) OnPortAdd(port uint64, direction string, types []string) error {
	n.l.Debugf("port add port=%d direction=%s types=%v", port, direction, types)
	return n.maybeFail("OnPortAdd")
}

func (n *workspaceNode) OnPortRemoved(port uint64, direction string) error {
	n.l.Debugf("port removed port=%d direction=%s", port, direction)
	return n.maybeFail("OnPortRemoved")
}

func (n *workspaceNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	n.l.Debugf("pre link add port=%d type=%s direction=%s", port, linkType, portDirection)
	return n.maybeFail("PreLinkAdd")
}

func (n *workspaceNode) OnLinkAdd(link, port uint64, linkType, portDirection string) error {
	n.l.Debugf("link add link=%d port=%d type=%s direction=%s", link, port, linkType, portDirection)
	return n.maybeFail("OnLinkAdd")
}

func (n *workspaceNode) OnLinkRemoved(link, port uint64, linkType, portDirection string) error {
	n.l.Debugf("link removed link=%d port=%d type=%s direction=%s", link, port, linkType, portDirection)
	return n.maybeFail("OnLinkRemoved")
}

func (n *workspaceNode) OnEvent(event pasta.Event, linkType string, receiverPortTypes []string, receiverPortDirection string) error {
	n.l.Debugf("event sender=%d:%d receiver=%d:%d type=%s receiver_types=%v receiver_direction=%s payload=%v", event.SenderNode, event.SenderPort, event.ReceiverNode, event.ReceiverPort, linkType, receiverPortTypes, receiverPortDirection, event.Payload)
	return n.maybeFail("OnEvent")
}

func (n *workspaceNode) OnInbox(message pasta.InboxMessage) error {
	n.l.Debugf("inbox receiver=%d payload=%v", message.ReceiverNode, message.Payload)
	return n.maybeFail("OnInbox")
}

func (n *workspaceNode) OnFormularMsg(message any) error {
	n.l.Debugf("formular payload=%v", message)
	return n.maybeFail("OnFormularMsg")
}

func (n *workspaceNode) OnTrigger() error {
	n.triggerCount += 1
	n.l.Debug("trigger")
	return n.maybeFail("OnTrigger")
}

func (n *workspaceNode) OnSave(cfg configer.Config) error {
	return n.maybeFail("OnSave")
}

func (n *workspaceNode) maybeFail(callback string) error {
	if n.panicOn != nil && n.panicOn[callback] {
		panic(callback)
	}
	if n.failOn == nil {
		return nil
	}
	return n.failOn[callback]
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
		Name:      "left1",
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
			left: {Node: nodeAID, Direction: "left", Name: "left1", Types: []string{"example.com/typeA"}},
		},
	})
	right, err := w.AddPort(pasta.Port{
		Node:      nodeBID,
		Direction: "right",
		Name:      "right1",
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
			left:  {Node: nodeAID, Direction: "left", Name: "left1", Types: []string{"example.com/typeA"}},
			right: {Node: nodeBID, Direction: "right", Name: "right1", Types: []string{"example.com/typeA"}},
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
			left:  {Node: nodeAID, Direction: "left", Name: "left1", Types: []string{"example.com/typeA"}, Links: []uint64{link}},
			right: {Node: nodeBID, Direction: "right", Name: "right1", Types: []string{"example.com/typeA"}, Links: []uint64{link}},
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
	if err := w.SetNodePosition(nodeAID, `{"x":12.5,"y":-3}`); err != nil {
		t.Fatalf("SetNodePosition: %v", err)
	}
	withLink.Nodes[nodeAID] = pasta.NodeSnapshot{
		Class:       "example.com/NodeA",
		PrimaryType: "example.com/typeA",
		Position:    `{"x":12.5,"y":-3}`,
		LeftPorts:   []uint64{left},
	}
	assertWorkspaceSnapshot(t, w, withLink)
	if err := w.SetNodePosition(nodeAID, ""); err != nil {
		t.Fatalf("SetNodePosition clear: %v", err)
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
	if err := w.SetNodePosition(999, "ignored"); !errors.Is(err, pasta.ErrNoNode) {
		t.Fatalf("SetNodePosition missing node error = %v, want %v", err, pasta.ErrNoNode)
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
			right: {Node: nodeBID, Direction: "right", Name: "right1", Types: []string{"example.com/typeA"}},
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
			right: {Node: nodeBID, Direction: "right", Name: "right1", Types: []string{"example.com/typeA"}},
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
			port: pasta.Port{Node: nodeID, Direction: "up", Name: "input", Types: []string{"example.com/typeA"}},
			want: pasta.ErrPortDirection,
		},
		{
			name: "no types",
			port: pasta.Port{Node: nodeID, Direction: "left", Name: "input"},
			want: pasta.ErrNoPortTypes,
		},
		{
			name: "bad type",
			port: pasta.Port{Node: nodeID, Direction: "left", Name: "input", Types: []string{"example.com/TypeA"}},
			want: pasta.ErrTypeName,
		},
		{
			name: "bad name",
			port: pasta.Port{Node: nodeID, Direction: "left", Name: "input!", Types: []string{"example.com/typeA"}},
			want: pasta.ErrPortName,
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

func TestWorkspacePortNameValidationAndScopedUniqueness(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeID, err := w.AddNode(&workspaceNode{}, "example.com/Node")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	left, err := w.AddPort(pasta.Port{
		Node:      nodeID,
		Direction: "left",
		Name:      "input 1-A_b",
		Types:     []string{"example.com/typeA"},
	})
	if err != nil {
		t.Fatalf("AddPort valid name: %v", err)
	}
	if _, err := w.AddPort(pasta.Port{
		Node:      nodeID,
		Direction: "left",
		Name:      "input 1-A_b",
		Types:     []string{"example.com/typeA"},
	}); !errors.Is(err, pasta.ErrPortName) {
		t.Fatalf("AddPort duplicate left name error = %v, want %v", err, pasta.ErrPortName)
	}
	right, err := w.AddPort(pasta.Port{
		Node:      nodeID,
		Direction: "right",
		Name:      "input 1-A_b",
		Types:     []string{"example.com/typeA"},
	})
	if err != nil {
		t.Fatalf("AddPort same name on opposite side: %v", err)
	}
	auxLeft, err := w.AddPort(pasta.Port{
		Node:      nodeID,
		Direction: "left",
		Name:      "aux",
		Types:     []string{"example.com/typeA"},
	})
	if err != nil {
		t.Fatalf("AddPort aux left: %v", err)
	}
	if err := w.SetPortName(auxLeft, "input 1-A_b"); !errors.Is(err, pasta.ErrPortName) {
		t.Fatalf("SetPortName duplicate left name error = %v, want %v", err, pasta.ErrPortName)
	}
	if err := w.SetPortName(right, "bad.name"); !errors.Is(err, pasta.ErrPortName) {
		t.Fatalf("SetPortName invalid name error = %v, want %v", err, pasta.ErrPortName)
	}
	if err := w.SetPortName(right, "output"); err != nil {
		t.Fatalf("SetPortName right: %v", err)
	}
	if err := w.SetPortName(left, "output"); err != nil {
		t.Fatalf("SetPortName same name on opposite side: %v", err)
	}

	if err := w.AddNodeClass(testNodeClass{
		name: "example.com/DuplicateDefaultPorts",
		params: pasta.NodeClassParams{InitialPorts: []pasta.Port{
			{Direction: "left", Name: "input", Types: []string{"example.com/typeA"}},
			{Direction: "left", Name: "input", Types: []string{"example.com/typeA"}},
		}},
	}); !errors.Is(err, pasta.ErrPortName) {
		t.Fatalf("AddNodeClass duplicate default port error = %v, want %v", err, pasta.ErrPortName)
	}
	if _, err := w.AddPlaceholderNode("example.com/DuplicatePlaceholderPorts", []pasta.Port{
		{Direction: "right", Name: "output", Types: []string{"example.com/typeA"}},
		{Direction: "right", Name: "output", Types: []string{"example.com/typeA"}},
	}); !errors.Is(err, pasta.ErrPortName) {
		t.Fatalf("AddPlaceholderNode duplicate port error = %v, want %v", err, pasta.ErrPortName)
	}

	placeholderID, err := w.AddPlaceholderNode("example.com/DuplicateReplacementPorts", []pasta.Port{
		{Direction: "left", Name: "input", Types: []string{"example.com/typeA"}},
	})
	if err != nil {
		t.Fatalf("AddPlaceholderNode replacement target: %v", err)
	}
	before := w.Snapshot()
	if err := w.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{name: "example.com/DuplicateReplacementPorts"},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			if len(previous) == 0 {
				return &workspaceNode{}, nil
			}
			previous[0].LeftPorts = append(previous[0].LeftPorts, pasta.Port{
				Direction: "left",
				Name:      "input",
				Types:     []string{"example.com/typeA"},
			})
			return &workspaceNode{}, nil
		},
	}); !errors.Is(err, pasta.ErrPortName) {
		t.Fatalf("AddNodeClass duplicate replacement port error = %v, want %v", err, pasta.ErrPortName)
	}
	after := w.Snapshot()
	if !after.Nodes[placeholderID].Placeholder ||
		!reflect.DeepEqual(after.Nodes[placeholderID].LeftPorts, before.Nodes[placeholderID].LeftPorts) ||
		len(after.Ports) != len(before.Ports) {
		t.Fatalf("duplicate replacement mutated workspace: before=%#v after=%#v", before, after)
	}
	for id, port := range before.Ports {
		if !equalPortSnapshot(after.Ports[id], port) {
			t.Fatalf("duplicate replacement mutated port %d: before=%#v after=%#v", id, port, after.Ports[id])
		}
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
		{Direction: "right", Name: "right1", Types: []string{"example.com/typeA"}},
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

type testNodeClass struct {
	name   string
	short  string
	long   string
	params pasta.NodeClassParams
}

func (c testNodeClass) ClassName() string {
	return c.name
}

func (c testNodeClass) ShortDescription() string {
	return c.short
}

func (c testNodeClass) LongDescription() string {
	return c.long
}

func (c testNodeClass) DefaultNodeParams() pasta.NodeClassParams {
	return c.params
}

type testFactoryNodeClass struct {
	testNodeClass
	newNode func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error)
}

func (c testFactoryNodeClass) NewNode(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
	if c.newNode == nil {
		return nil, nil
	}
	return c.newNode(cfg, previous...)
}

func TestWorkspaceNodeClassesSnapshotsFactoriesAndNotifications(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	metadata := testNodeClass{
		name:  "example.com/MetadataNode",
		short: "Metadata node",
		long:  "**Metadata** node class.",
	}
	if err := w.AddNodeClass(metadata); err != nil {
		t.Fatalf("AddNodeClass metadata: %v", err)
	}
	if got, ok := w.NodeClassSnapshot("example.com/MetadataNode"); !ok || !equalNodeClassSnapshot(got, pasta.NodeClassSnapshot{
		Class:            "example.com/MetadataNode",
		ShortDescription: "Metadata node",
	}) {
		t.Fatalf("NodeClassSnapshot = %#v, %v", got, ok)
	}
	if got, ok := w.NodeClassLongDescription("example.com/MetadataNode"); !ok || got != "**Metadata** node class." {
		t.Fatalf("NodeClassLongDescription = %#v, %v", got, ok)
	}
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Classes: map[string]pasta.NodeClassSnapshot{
			"example.com/MetadataNode": {
				Class:            "example.com/MetadataNode",
				ShortDescription: "Metadata node",
			},
		},
	})

	var factoryNode *workspaceNode
	factory := testFactoryNodeClass{
		testNodeClass: testNodeClass{
			name:  "example.com/FactoryNode",
			short: "Factory node",
			params: pasta.NodeClassParams{
				Root:        true,
				PrimaryType: "example.com/typeA",
				InitialPorts: []pasta.Port{
					{Direction: "left", Name: "input", Types: []string{"example.com/typeA"}},
					{Direction: "right", Name: "output", Types: []string{"example.com/typeA"}},
				},
			},
		},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			factoryNode = &workspaceNode{}
			return factoryNode, nil
		},
	}
	if err := w.AddNodeClass(factory); err != nil {
		t.Fatalf("AddNodeClass factory: %v", err)
	}
	nodeID, err := w.AddNodeByClass("example.com/FactoryNode")
	if err != nil {
		t.Fatalf("AddNodeByClass: %v", err)
	}
	snapshot := w.Snapshot()
	nodeSnapshot := snapshot.Nodes[nodeID]
	if nodeSnapshot.Class != "example.com/FactoryNode" || !nodeSnapshot.Root || nodeSnapshot.PrimaryType != "example.com/typeA" {
		t.Fatalf("factory node snapshot = %#v", snapshot.Nodes[nodeID])
	}
	if len(nodeSnapshot.LeftPorts) != 1 || len(nodeSnapshot.RightPorts) != 1 {
		t.Fatalf("factory node ports = %#v, want one left and one right", nodeSnapshot)
	}
	leftPort := nodeSnapshot.LeftPorts[0]
	rightPort := nodeSnapshot.RightPorts[0]
	if !equalPortSnapshot(snapshot.Ports[leftPort], pasta.PortSnapshot{
		Node:      nodeID,
		Direction: "left",
		Name:      "input",
		Types:     []string{"example.com/typeA"},
	}) {
		t.Fatalf("left default port = %#v", snapshot.Ports[leftPort])
	}
	if !equalPortSnapshot(snapshot.Ports[rightPort], pasta.PortSnapshot{
		Node:      nodeID,
		Direction: "right",
		Name:      "output",
		Types:     []string{"example.com/typeA"},
	}) {
		t.Fatalf("right default port = %#v", snapshot.Ports[rightPort])
	}
	if factoryNode.initData == nil || factoryNode.initData.PrimaryType != "example.com/typeA" ||
		!reflect.DeepEqual(factoryNode.initData.LeftPorts, []uint64{leftPort}) ||
		!reflect.DeepEqual(factoryNode.initData.RightPorts, []uint64{rightPort}) {
		t.Fatalf("factory init data = %#v", factoryNode.initData)
	}
	if got, want := factoryNode.initFlags, (workspaceNodeInitFlags{isClassConstructed: true}); got != want {
		t.Fatalf("factory init flags = %#v, want %#v", got, want)
	}
	if got := snapshot.Classes["example.com/FactoryNode"]; got.PrimaryType != "example.com/typeA" ||
		!reflect.DeepEqual(got.InitialPorts, []pasta.NodeClassPortSnapshot{
			{Direction: "left", Name: "input", Types: []string{"example.com/typeA"}},
			{Direction: "right", Name: "output", Types: []string{"example.com/typeA"}},
		}) {
		t.Fatalf("factory class snapshot = %#v", got)
	}
	if _, err := w.AddNodeByClass("example.com/MissingNode"); !errors.Is(err, pasta.ErrNoNodeClass) {
		t.Fatalf("AddNodeByClass missing error = %v, want %v", err, pasta.ErrNoNodeClass)
	}
	if _, err := w.AddNodeByClass("example.com/MetadataNode"); !errors.Is(err, pasta.ErrNodeClassFactory) {
		t.Fatalf("AddNodeByClass metadata error = %v, want %v", err, pasta.ErrNodeClassFactory)
	}
	if _, err := w.AddNodeByClass("example.com/metadataNode"); !errors.Is(err, pasta.ErrClassName) {
		t.Fatalf("AddNodeByClass invalid name error = %v, want %v", err, pasta.ErrClassName)
	}

	var notifications []pasta.WorkspaceNotification
	w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		notifications = append(notifications, notification)
	})
	notifications = nil

	replacement := testNodeClass{
		name:  "example.com/MetadataNode",
		short: "Updated metadata node",
		long:  "Updated **metadata**.",
	}
	if err := w.AddNodeClass(replacement); err != nil {
		t.Fatalf("AddNodeClass replacement: %v", err)
	}
	if err := w.RemoveNodeClass("example.com/MetadataNode"); err != nil {
		t.Fatalf("RemoveNodeClass: %v", err)
	}
	if err := w.RemoveNodeClass("example.com/MetadataNode"); !errors.Is(err, pasta.ErrNoNodeClass) {
		t.Fatalf("RemoveNodeClass missing error = %v, want %v", err, pasta.ErrNoNodeClass)
	}
	if err := w.AddNodeClass(testNodeClass{name: "example.com/metadataNode"}); !errors.Is(err, pasta.ErrClassName) {
		t.Fatalf("AddNodeClass invalid name error = %v, want %v", err, pasta.ErrClassName)
	}
	if err := w.AddNodeClass(testNodeClass{
		name:   "example.com/BadPrimaryNode",
		params: pasta.NodeClassParams{PrimaryType: "example.com/BadType"},
	}); !errors.Is(err, pasta.ErrTypeName) {
		t.Fatalf("AddNodeClass invalid primary error = %v, want %v", err, pasta.ErrTypeName)
	}
	if err := w.AddNodeClass(testNodeClass{
		name:   "example.com/BadPortNode",
		params: pasta.NodeClassParams{InitialPorts: []pasta.Port{{Direction: "left"}}},
	}); !errors.Is(err, pasta.ErrNoPortTypes) {
		t.Fatalf("AddNodeClass invalid port error = %v, want %v", err, pasta.ErrNoPortTypes)
	}

	if len(notifications) != 2 {
		t.Fatalf("class notifications = %#v, want 2", notifications)
	}
	assertClassNotification(t, notifications[0], pasta.NotificationNodeClassAdded, "example.com/MetadataNode", pasta.NodeClassSnapshot{
		Class:            "example.com/MetadataNode",
		ShortDescription: "Updated metadata node",
	})
	assertClassNotification(t, notifications[1], pasta.NotificationNodeClassRemoved, "example.com/MetadataNode", pasta.NodeClassSnapshot{
		Class:            "example.com/MetadataNode",
		ShortDescription: "Updated metadata node",
	})
}

func TestWorkspaceAddNodeClassSuggestsPlaceholderReplacement(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	replaceID, err := w.AddPlaceholderNodeWithRoot("example.com/RestoredNode", false, []pasta.Port{
		{Direction: "left", Name: "legacy in", Types: []string{"example.com/typeA"}},
		{Direction: "right", Name: "legacy out", Types: []string{"example.com/typeA"}},
		{Direction: "right", Name: "delete out", Types: []string{"example.com/typeA"}},
	})
	if err != nil {
		t.Fatalf("AddPlaceholderNodeWithRoot replace: %v", err)
	}
	leaveID, err := w.AddPlaceholderNode("example.com/RestoredNode", []pasta.Port{
		{Direction: "right", Name: "leave", Types: []string{"example.com/typeA"}},
	})
	if err != nil {
		t.Fatalf("AddPlaceholderNode leave: %v", err)
	}
	left := w.Snapshot().Nodes[replaceID].LeftPorts[0]
	right := w.Snapshot().Nodes[replaceID].RightPorts[0]
	deletedRight := w.Snapshot().Nodes[replaceID].RightPorts[1]
	peerID, err := w.AddNode(&workspaceNode{}, "example.com/PeerNode")
	if err != nil {
		t.Fatalf("AddNode peer: %v", err)
	}
	peerLeft := mustAddPort(t, w, peerID, "left", "example.com/typeA")
	deletedLink, _, err := w.AddLink(peerLeft, deletedRight)
	if err != nil {
		t.Fatalf("AddLink deleted port: %v", err)
	}
	peerLeft2 := mustAddPort(t, w, peerID, "left", "example.com/typeA")
	incompatibleLink, _, err := w.AddLink(peerLeft2, right)
	if err != nil {
		t.Fatalf("AddLink incompatible kept port: %v", err)
	}
	if err := w.SetNodePrimary(replaceID, "example.com/typeA"); err != nil {
		t.Fatalf("SetNodePrimary placeholder: %v", err)
	}
	if err := w.SetNodeLabel(replaceID, "legacy"); err != nil {
		t.Fatalf("SetNodeLabel placeholder: %v", err)
	}

	var restored *workspaceNode
	var suggestions []pasta.NodeClassState
	class := testFactoryNodeClass{
		testNodeClass: testNodeClass{
			name:  "example.com/RestoredNode",
			short: "Restored node",
		},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			if len(previous) == 0 {
				return &workspaceNode{}, nil
			}
			state := previous[0]
			suggestions = append(suggestions, *state)
			if state.Label != "legacy" {
				return nil, nil
			}
			state.Root = true
			state.PrimaryType = "example.com/typeB"
			state.Label = "restored"
			state.LeftPorts[0].Name = "input"
			state.LeftPorts[0].Types = []string{"example.com/typeB"}
			state.LeftPorts = append(state.LeftPorts, pasta.Port{
				Direction: "left",
				Name:      "aux",
				Types:     []string{"example.com/typeB"},
			})
			state.RightPorts[0].Name = "output"
			state.RightPorts[0].Types = []string{"example.com/typeB"}
			state.RightPorts = state.RightPorts[:1]
			restored = &workspaceNode{}
			return restored, nil
		},
	}

	var notifications []pasta.WorkspaceNotification
	w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		notifications = append(notifications, notification)
	})
	notifications = nil

	if err := w.AddNodeClass(class); err != nil {
		t.Fatalf("AddNodeClass: %v", err)
	}
	if len(suggestions) != 2 {
		t.Fatalf("placeholder suggestions = %d, want 2", len(suggestions))
	}
	if restored == nil || restored.initData == nil {
		t.Fatalf("restored node/init data = %#v", restored)
	}
	if restored.initData.PrimaryType != "example.com/typeB" ||
		restored.initData.Label != "restored" ||
		len(restored.initData.LeftPorts) != 2 ||
		restored.initData.LeftPorts[0] != left ||
		!reflect.DeepEqual(restored.initData.RightPorts, []uint64{right}) {
		t.Fatalf("restored init data = %#v", restored.initData)
	}
	if got, want := restored.initFlags, (workspaceNodeInitFlags{
		isReplacement:            true,
		isPlaceholderReplacement: true,
		isClassConstructed:       true,
	}); got != want {
		t.Fatalf("restored init flags = %#v, want %#v", got, want)
	}
	addedLeft := restored.initData.LeftPorts[1]
	snapshot := w.Snapshot()
	if snapshot.Nodes[replaceID].Placeholder ||
		!snapshot.Nodes[replaceID].Root ||
		snapshot.Nodes[replaceID].PrimaryType != "example.com/typeB" ||
		snapshot.Nodes[replaceID].Label != "restored" {
		t.Fatalf("replaced placeholder snapshot = %#v", snapshot.Nodes[replaceID])
	}
	if !snapshot.Nodes[leaveID].Placeholder {
		t.Fatalf("skipped placeholder snapshot = %#v, want placeholder", snapshot.Nodes[leaveID])
	}
	if _, ok := snapshot.Ports[deletedRight]; ok {
		t.Fatalf("deleted placeholder port %d remains in snapshot", deletedRight)
	}
	if _, ok := snapshot.Links[deletedLink]; ok {
		t.Fatalf("link %d attached to deleted placeholder port remains in snapshot", deletedLink)
	}
	if _, ok := snapshot.Links[incompatibleLink]; ok {
		t.Fatalf("link %d incompatible with restored placeholder port remains in snapshot", incompatibleLink)
	}
	if !equalPortSnapshot(snapshot.Ports[left], pasta.PortSnapshot{
		Node:      replaceID,
		Direction: "left",
		Name:      "input",
		Types:     []string{"example.com/typeB"},
	}) {
		t.Fatalf("restored left port = %#v", snapshot.Ports[left])
	}
	if !equalPortSnapshot(snapshot.Ports[addedLeft], pasta.PortSnapshot{
		Node:      replaceID,
		Direction: "left",
		Name:      "aux",
		Types:     []string{"example.com/typeB"},
	}) {
		t.Fatalf("added left port = %#v", snapshot.Ports[addedLeft])
	}
	if !equalPortSnapshot(snapshot.Ports[right], pasta.PortSnapshot{
		Node:      replaceID,
		Direction: "right",
		Name:      "output",
		Types:     []string{"example.com/typeB"},
	}) {
		t.Fatalf("restored right port = %#v", snapshot.Ports[right])
	}
	assertHasNotification(t, notifications, pasta.NotificationNodeClassAdded, 0)
	assertHasNotification(t, notifications, pasta.NotificationNodeUpdated, replaceID)

	notifications = nil
	if err := w.AddNodeClass(class); err != nil {
		t.Fatalf("re-add NodeClass: %v", err)
	}
	if len(suggestions) != 2 {
		t.Fatalf("placeholder suggestions after re-add = %d, want still 2", len(suggestions))
	}
	assertNotificationMatches(t, notifications, []notificationMatch{
		{kind: pasta.NotificationNodeClassAdded},
	})
}

func TestWorkspaceNodeClassReplacementKeepsCompatibleAnyLinks(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	peerSingleID, err := w.AddNode(&workspaceNode{}, "example.com/PeerSingle")
	if err != nil {
		t.Fatalf("AddNode peer single: %v", err)
	}
	peerSingle := mustAddPort(t, w, peerSingleID, "left", "example.com/typeA")

	peerMultiID, err := w.AddNode(&workspaceNode{}, "example.com/PeerMulti")
	if err != nil {
		t.Fatalf("AddNode peer multi: %v", err)
	}
	peerMulti, err := w.AddPort(pasta.Port{
		Node:      peerMultiID,
		Direction: "left",
		Name:      "left1",
		Types:     []string{"example.com/typeB", "example.com/typeC"},
	})
	if err != nil {
		t.Fatalf("AddPort peer multi: %v", err)
	}

	peerAnyID, err := w.AddNode(&workspaceNode{}, "example.com/PeerAny")
	if err != nil {
		t.Fatalf("AddNode peer any: %v", err)
	}
	peerAny, err := w.AddPort(pasta.Port{
		Node:      peerAnyID,
		Direction: "left",
		Name:      "left1",
		Types:     []string{pasta.AnyType, "example.com/typeF"},
	})
	if err != nil {
		t.Fatalf("AddPort peer any: %v", err)
	}

	keepConcrete, keepConcreteRight := mustAddAnyPlaceholder(t, w, "keep concrete")
	keepAny, keepAnyRight := mustAddAnyPlaceholder(t, w, "keep any")
	removeAny, removeAnyRight := mustAddAnyPlaceholder(t, w, "remove any")
	specializeAny, specializeAnyRight := mustAddAnyPlaceholder(t, w, "specialize any")
	keepViaPeerAny, keepViaPeerAnyRight := mustAddAnyPlaceholder(t, w, "keep via peer any")

	keepConcreteLink, linkType, err := w.AddLink(peerSingle, keepConcreteRight)
	if err != nil {
		t.Fatalf("AddLink keep concrete: %v", err)
	}
	if linkType != "example.com/typeA" {
		t.Fatalf("keep concrete link type = %q, want example.com/typeA", linkType)
	}
	keepAnyLink, linkType, err := w.AddLink(peerMulti, keepAnyRight)
	if err != nil {
		t.Fatalf("AddLink keep any: %v", err)
	}
	if linkType != pasta.AnyType {
		t.Fatalf("keep any link type = %q, want %q", linkType, pasta.AnyType)
	}
	removeAnyLink, linkType, err := w.AddLink(peerMulti, removeAnyRight)
	if err != nil {
		t.Fatalf("AddLink remove any: %v", err)
	}
	if linkType != pasta.AnyType {
		t.Fatalf("remove any link type = %q, want %q", linkType, pasta.AnyType)
	}
	specializeAnyLink, linkType, err := w.AddLink(peerMulti, specializeAnyRight)
	if err != nil {
		t.Fatalf("AddLink specialize any: %v", err)
	}
	if linkType != pasta.AnyType {
		t.Fatalf("specialize any link type = %q, want %q", linkType, pasta.AnyType)
	}
	keepViaPeerAnyLink, linkType, err := w.AddLink(peerAny, keepViaPeerAnyRight)
	if err != nil {
		t.Fatalf("AddLink keep via peer any: %v", err)
	}
	if linkType != pasta.AnyType {
		t.Fatalf("keep via peer any link type = %q, want %q", linkType, pasta.AnyType)
	}

	if err := w.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{name: "example.com/AnyRestored"},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			if len(previous) > 0 {
				switch previous[0].Label {
				case "keep concrete", "keep any":
					previous[0].RightPorts[0].Types = []string{pasta.AnyType}
				case "specialize any", "keep via peer any":
					previous[0].RightPorts[0].Types = []string{"example.com/typeB"}
				case "remove any":
					previous[0].RightPorts[0].Types = []string{"example.com/typeD"}
				}
			}
			return &workspaceNode{}, nil
		},
	}); err != nil {
		t.Fatalf("AddNodeClass: %v", err)
	}

	snapshot := w.Snapshot()
	for id, linkID := range map[uint64]uint64{
		keepConcrete:   keepConcreteLink,
		keepAny:        keepAnyLink,
		keepViaPeerAny: keepViaPeerAnyLink,
	} {
		if snapshot.Nodes[id].Placeholder {
			t.Fatalf("node %d remains placeholder", id)
		}
		if _, ok := snapshot.Links[linkID]; !ok {
			t.Fatalf("compatible any link %d was removed", linkID)
		}
	}
	if snapshot.Nodes[removeAny].Placeholder {
		t.Fatalf("remove-any node %d remains placeholder", removeAny)
	}
	if _, ok := snapshot.Links[removeAnyLink]; ok {
		t.Fatalf("incompatible any link %d remains", removeAnyLink)
	}
	if snapshot.Nodes[specializeAny].Placeholder {
		t.Fatalf("specialize-any node %d remains placeholder", specializeAny)
	}
	if got, ok := snapshot.Links[specializeAnyLink]; !ok || got.Type != "example.com/typeB" {
		t.Fatalf("specialized any link = %#v, %v; want type example.com/typeB", got, ok)
	}
}

func TestWorkspaceUniqueNodeClassRegistrationRemovesDuplicatesBeforePlaceholderReplacement(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	keep := &workspaceNode{}
	remove := &workspaceNode{}
	keepID, err := w.AddNode(keep, "example.com/UniqueRestored")
	if err != nil {
		t.Fatalf("AddNode keep: %v", err)
	}
	removeID, err := w.AddNode(remove, "example.com/UniqueRestored")
	if err != nil {
		t.Fatalf("AddNode remove: %v", err)
	}
	placeholderID, err := w.AddPlaceholderNode("example.com/UniqueRestored", nil)
	if err != nil {
		t.Fatalf("AddPlaceholderNode: %v", err)
	}

	replacementCalls := 0
	if err := w.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{
			name: "example.com/UniqueRestored",
			params: pasta.NodeClassParams{
				Unique: true,
			},
		},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			if len(previous) == 0 {
				return &workspaceNode{}, nil
			}
			replacementCalls += 1
			return &workspaceNode{}, nil
		},
	}); err != nil {
		t.Fatalf("AddNodeClass unique: %v", err)
	}

	nodes, err := w.NodesByClass("example.com/UniqueRestored")
	if err != nil {
		t.Fatalf("NodesByClass: %v", err)
	}
	if !reflect.DeepEqual(nodes, []uint64{keepID}) {
		t.Fatalf("unique class nodes = %v, want [%d]", nodes, keepID)
	}
	if replacementCalls != 0 {
		t.Fatalf("placeholder replacements = %d, want 0", replacementCalls)
	}
	if remove.stopCount != 1 {
		t.Fatalf("removed duplicate stop count = %d, want 1", remove.stopCount)
	}
	if _, ok := w.NodeSnapshot(removeID); ok {
		t.Fatalf("removed duplicate node %d still exists", removeID)
	}
	if _, ok := w.NodeSnapshot(placeholderID); ok {
		t.Fatalf("removed duplicate placeholder %d still exists", placeholderID)
	}
	if got := w.Snapshot().Classes["example.com/UniqueRestored"]; !got.Unique {
		t.Fatalf("unique class snapshot = %#v, want Unique true", got)
	}
}

func TestWorkspaceUniqueNodeClassKeepsSmallestIDPlaceholderThenReplacesIt(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	placeholderID, err := w.AddPlaceholderNodeWithRoot("example.com/UniquePlaceholderFirst", true, []pasta.Port{
		{Direction: "right", Name: "legacy", Types: []string{"example.com/typeA"}},
	})
	if err != nil {
		t.Fatalf("AddPlaceholderNodeWithRoot: %v", err)
	}
	removed := &workspaceNode{}
	removedID, err := w.AddNode(removed, "example.com/UniquePlaceholderFirst")
	if err != nil {
		t.Fatalf("AddNode removed: %v", err)
	}
	removedPlaceholderID, err := w.AddPlaceholderNode("example.com/UniquePlaceholderFirst", nil)
	if err != nil {
		t.Fatalf("AddPlaceholderNode removed: %v", err)
	}

	var restored *workspaceNode
	replacementCalls := 0
	if err := w.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{
			name: "example.com/UniquePlaceholderFirst",
			params: pasta.NodeClassParams{
				Unique: true,
			},
		},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			if len(previous) == 0 {
				return &workspaceNode{}, nil
			}
			replacementCalls += 1
			previous[0].Label = "restored"
			restored = &workspaceNode{}
			return restored, nil
		},
	}); err != nil {
		t.Fatalf("AddNodeClass unique: %v", err)
	}

	nodes, err := w.NodesByClass("example.com/UniquePlaceholderFirst")
	if err != nil {
		t.Fatalf("NodesByClass: %v", err)
	}
	if !reflect.DeepEqual(nodes, []uint64{placeholderID}) {
		t.Fatalf("unique class nodes = %v, want [%d]", nodes, placeholderID)
	}
	if replacementCalls != 1 {
		t.Fatalf("placeholder replacements = %d, want 1", replacementCalls)
	}
	snapshot := w.Snapshot().Nodes[placeholderID]
	if snapshot.Placeholder || !snapshot.Root || snapshot.Label != "restored" {
		t.Fatalf("kept placeholder replacement snapshot = %#v", snapshot)
	}
	if got, want := restored.initFlags, (workspaceNodeInitFlags{
		isReplacement:            true,
		isPlaceholderReplacement: true,
		isClassConstructed:       true,
	}); got != want {
		t.Fatalf("restored init flags = %#v, want %#v", got, want)
	}
	if removed.stopCount != 1 {
		t.Fatalf("removed live node stop count = %d, want 1", removed.stopCount)
	}
	if _, ok := w.NodeSnapshot(removedID); ok {
		t.Fatalf("removed live node %d still exists", removedID)
	}
	if _, ok := w.NodeSnapshot(removedPlaceholderID); ok {
		t.Fatalf("removed placeholder node %d still exists", removedPlaceholderID)
	}
}

func TestWorkspaceUniqueNodeClassReAddExistingClassRemovesDuplicates(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	if err := w.AddNodeClass(testNodeClass{name: "example.com/ReaddedUnique"}); err != nil {
		t.Fatalf("AddNodeClass initial: %v", err)
	}
	keepID, err := w.AddNode(&workspaceNode{}, "example.com/ReaddedUnique")
	if err != nil {
		t.Fatalf("AddNode keep: %v", err)
	}
	remove := &workspaceNode{}
	removeID, err := w.AddNode(remove, "example.com/ReaddedUnique")
	if err != nil {
		t.Fatalf("AddNode remove: %v", err)
	}

	if err := w.AddNodeClass(testNodeClass{
		name: "example.com/ReaddedUnique",
		params: pasta.NodeClassParams{
			Unique: true,
		},
	}); err != nil {
		t.Fatalf("AddNodeClass re-add unique: %v", err)
	}

	nodes, err := w.NodesByClass("example.com/ReaddedUnique")
	if err != nil {
		t.Fatalf("NodesByClass: %v", err)
	}
	if !reflect.DeepEqual(nodes, []uint64{keepID}) {
		t.Fatalf("unique class nodes = %v, want [%d]", nodes, keepID)
	}
	if remove.stopCount != 1 {
		t.Fatalf("removed node stop count = %d, want 1", remove.stopCount)
	}
	if _, ok := w.NodeSnapshot(removeID); ok {
		t.Fatalf("removed node %d still exists", removeID)
	}
}

func TestWorkspaceUniqueNodeClassRejectsAddingNodes(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	newNodeCalls := 0
	if err := w.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{
			name: "example.com/UniqueNode",
			params: pasta.NodeClassParams{
				Unique: true,
			},
		},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			newNodeCalls += 1
			return &workspaceNode{}, nil
		},
	}); err != nil {
		t.Fatalf("AddNodeClass: %v", err)
	}
	if _, err := w.AddNodeByClass("example.com/UniqueNode"); err != nil {
		t.Fatalf("AddNodeByClass first: %v", err)
	}
	if _, err := w.AddNodeByClass("example.com/UniqueNode"); !errors.Is(err, pasta.ErrUniqueNodeClassDup) {
		t.Fatalf("AddNodeByClass duplicate error = %v, want %v", err, pasta.ErrUniqueNodeClassDup)
	}
	if newNodeCalls != 1 {
		t.Fatalf("factory new node calls = %d, want 1", newNodeCalls)
	}
	if _, err := w.AddNode(&workspaceNode{}, "example.com/UniqueNode"); !errors.Is(err, pasta.ErrUniqueNodeClassDup) {
		t.Fatalf("AddNode duplicate unique class error = %v, want %v", err, pasta.ErrUniqueNodeClassDup)
	}
	if _, err := w.AddPlaceholderNode("example.com/UniqueNode", nil); !errors.Is(err, pasta.ErrUniqueNodeClassDup) {
		t.Fatalf("AddPlaceholderNode duplicate unique class error = %v, want %v", err, pasta.ErrUniqueNodeClassDup)
	}
}

func TestWorkspaceAddNodeByClassNameValidationPrecedesFactory(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	newNodeCalls := 0
	if err := w.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{name: "example.com/NamedFactory"},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			newNodeCalls += 1
			return &workspaceNode{}, nil
		},
	}); err != nil {
		t.Fatalf("AddNodeClass: %v", err)
	}
	if _, err := w.AddNode(&workspaceNode{}, "example.com/Peer", "taken"); err != nil {
		t.Fatalf("AddNode peer: %v", err)
	}
	before := w.Snapshot()

	if _, err := w.AddNodeByClass("example.com/NamedFactory", "bad[name"); !errors.Is(err, pasta.ErrNodeName) {
		t.Fatalf("AddNodeByClass invalid name error = %v, want %v", err, pasta.ErrNodeName)
	}
	if _, err := w.AddNodeByClass("example.com/NamedFactory", "taken"); !errors.Is(err, pasta.ErrNodeNameDup) {
		t.Fatalf("AddNodeByClass duplicate name error = %v, want %v", err, pasta.ErrNodeNameDup)
	}
	if newNodeCalls != 0 {
		t.Fatalf("factory new node calls after rejected names = %d, want 0", newNodeCalls)
	}
	assertWorkspaceSnapshot(t, w, before)

	nodeID, err := w.AddNodeByClass("example.com/NamedFactory", "created")
	if err != nil {
		t.Fatalf("AddNodeByClass explicit name: %v", err)
	}
	if newNodeCalls != 1 {
		t.Fatalf("factory new node calls after valid name = %d, want 1", newNodeCalls)
	}
	if got := w.Snapshot().Nodes[nodeID].Name; got != "created" {
		t.Fatalf("factory node name = %q, want %q", got, "created")
	}
}

func TestWorkspaceUniqueNodeClassAllowsReplacingOnlyNode(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	if err := w.AddNodeClass(testNodeClass{
		name: "example.com/OnlyUniqueNode",
		params: pasta.NodeClassParams{
			Unique: true,
		},
	}); err != nil {
		t.Fatalf("AddNodeClass: %v", err)
	}
	nodeID, err := w.AddPlaceholderNode("example.com/OnlyUniqueNode", nil)
	if err != nil {
		t.Fatalf("AddPlaceholderNode first: %v", err)
	}
	if err := w.ReplacePlaceholderNode(nodeID, &workspaceNode{}); err != nil {
		t.Fatalf("ReplacePlaceholderNode only unique node: %v", err)
	}
	if err := w.ReplaceNode(nodeID, &workspaceNode{}); err != nil {
		t.Fatalf("ReplaceNode only unique node: %v", err)
	}
	if err := w.ReplaceNodeWithPlaceholder(nodeID, nil); err != nil {
		t.Fatalf("ReplaceNodeWithPlaceholder only unique node: %v", err)
	}
	if err := w.ReplacePlaceholderNode(nodeID, &workspaceNode{}); err != nil {
		t.Fatalf("ReplacePlaceholderNode after placeholder conversion: %v", err)
	}

	nodes, err := w.NodesByClass("example.com/OnlyUniqueNode")
	if err != nil {
		t.Fatalf("NodesByClass: %v", err)
	}
	if !reflect.DeepEqual(nodes, []uint64{nodeID}) {
		t.Fatalf("unique class nodes = %v, want [%d]", nodes, nodeID)
	}
}

func TestWorkspaceUniqueNodeClassRejectsReplacementWhenDuplicateExists(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	class := &testNodeClass{name: "example.com/MutableUniqueNode"}
	if err := w.AddNodeClass(class); err != nil {
		t.Fatalf("AddNodeClass: %v", err)
	}
	nodeAID, err := w.AddNode(&workspaceNode{}, "example.com/MutableUniqueNode")
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	if _, err := w.AddNode(&workspaceNode{}, "example.com/MutableUniqueNode"); err != nil {
		t.Fatalf("AddNode B: %v", err)
	}

	class.params.Unique = true
	if err := w.ReplaceNode(nodeAID, &workspaceNode{}); !errors.Is(err, pasta.ErrUniqueNodeClassDup) {
		t.Fatalf("ReplaceNode duplicate unique class error = %v, want %v", err, pasta.ErrUniqueNodeClassDup)
	}
	if err := w.ReplaceNodeWithPlaceholder(nodeAID, nil); !errors.Is(err, pasta.ErrUniqueNodeClassDup) {
		t.Fatalf("ReplaceNodeWithPlaceholder duplicate unique class error = %v, want %v", err, pasta.ErrUniqueNodeClassDup)
	}
}

func TestWorkspaceNodeClassSnapshotCopiesAndReplacement(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	class := testNodeClass{
		name:  "example.com/CopyNode",
		short: "Copy node",
		params: pasta.NodeClassParams{
			PrimaryType: "example.com/typeA",
			InitialPorts: []pasta.Port{
				{Direction: "left", Name: "input", Types: []string{"example.com/typeA"}},
			},
		},
	}
	if err := w.AddNodeClass(class); err != nil {
		t.Fatalf("AddNodeClass: %v", err)
	}
	snapshot, ok := w.NodeClassSnapshot("example.com/CopyNode")
	if !ok {
		t.Fatal("NodeClassSnapshot returned false")
	}
	snapshot.InitialPorts[0].Types[0] = "example.com/changed"
	snapshot.InitialPorts[0].Name = "changed"
	state := w.Snapshot()
	if got := state.Classes["example.com/CopyNode"].InitialPorts[0]; got.Name != "input" || got.Types[0] != "example.com/typeA" {
		t.Fatalf("class snapshot was mutated through returned copy: %#v", got)
	}

	replacement := testNodeClass{
		name:  "example.com/CopyNode",
		short: "Updated copy node",
		params: pasta.NodeClassParams{
			PrimaryType: "example.com/typeB",
			InitialPorts: []pasta.Port{
				{Direction: "right", Name: "output", Types: []string{"example.com/typeB"}},
			},
		},
	}
	if err := w.AddNodeClass(replacement); err != nil {
		t.Fatalf("AddNodeClass replacement: %v", err)
	}
	got := w.Snapshot().Classes["example.com/CopyNode"]
	if !equalNodeClassSnapshot(got, pasta.NodeClassSnapshot{
		Class:            "example.com/CopyNode",
		ShortDescription: "Updated copy node",
		PrimaryType:      "example.com/typeB",
		InitialPorts: []pasta.NodeClassPortSnapshot{
			{Direction: "right", Name: "output", Types: []string{"example.com/typeB"}},
		},
	}) {
		t.Fatalf("replacement class snapshot = %#v", got)
	}
}

func TestWorkspaceAddNodeByClassFactoryFailuresDoNotMutate(t *testing.T) {
	failErr := errors.New("factory boom")
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	if err := w.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{name: "example.com/FailFactoryNode"},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			return nil, failErr
		},
	}); err != nil {
		t.Fatalf("AddNodeClass fail factory: %v", err)
	}
	before := w.Snapshot()
	if _, err := w.AddNodeByClass("example.com/FailFactoryNode"); !errors.Is(err, failErr) {
		t.Fatalf("AddNodeByClass factory error = %v, want %v", err, failErr)
	}
	assertWorkspaceSnapshot(t, w, before)

	if err := w.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{name: "example.com/NilFactoryNode"},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("AddNodeClass nil factory: %v", err)
	}
	before = w.Snapshot()
	if _, err := w.AddNodeByClass("example.com/NilFactoryNode"); !errors.Is(err, pasta.ErrNoNode) {
		t.Fatalf("AddNodeByClass nil node error = %v, want %v", err, pasta.ErrNoNode)
	}
	assertWorkspaceSnapshot(t, w, before)
}

func TestWorkspaceNodeClassReplacementEdgeCases(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	metadataPlaceholder, err := w.AddPlaceholderNode("example.com/MetadataOnly", []pasta.Port{
		{Direction: "right", Name: "out", Types: []string{"example.com/typeA"}},
	})
	if err != nil {
		t.Fatalf("AddPlaceholderNode metadata: %v", err)
	}
	if err := w.AddNodeClass(testNodeClass{name: "example.com/MetadataOnly"}); err != nil {
		t.Fatalf("AddNodeClass metadata: %v", err)
	}
	if snapshot := w.Snapshot().Nodes[metadataPlaceholder]; !snapshot.Placeholder {
		t.Fatalf("metadata-only class replaced placeholder: %#v", snapshot)
	}

	invalidID, err := w.AddPlaceholderNode("example.com/InvalidReplacement", []pasta.Port{
		{Direction: "left", Name: "in", Types: []string{"example.com/typeA"}},
		{Direction: "right", Name: "out", Types: []string{"example.com/typeA"}},
	})
	if err != nil {
		t.Fatalf("AddPlaceholderNode invalid: %v", err)
	}
	invalidBefore := w.Snapshot()
	if err := w.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{name: "example.com/InvalidReplacement"},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			if len(previous) == 0 {
				return &workspaceNode{}, nil
			}
			previous[0].LeftPorts = append(previous[0].LeftPorts, previous[0].LeftPorts[0])
			return &workspaceNode{}, nil
		},
	}); !errors.Is(err, pasta.ErrPortOrder) {
		t.Fatalf("AddNodeClass duplicate replacement port error = %v, want %v", err, pasta.ErrPortOrder)
	}
	afterInvalid := w.Snapshot()
	if !afterInvalid.Nodes[invalidID].Placeholder ||
		!reflect.DeepEqual(afterInvalid.Nodes[invalidID].LeftPorts, invalidBefore.Nodes[invalidID].LeftPorts) ||
		!reflect.DeepEqual(afterInvalid.Nodes[invalidID].RightPorts, invalidBefore.Nodes[invalidID].RightPorts) {
		t.Fatalf("invalid replacement mutated placeholder: before=%#v after=%#v", invalidBefore.Nodes[invalidID], afterInvalid.Nodes[invalidID])
	}
	for id, port := range invalidBefore.Ports {
		if !equalPortSnapshot(afterInvalid.Ports[id], port) {
			t.Fatalf("invalid replacement mutated port %d: before=%#v after=%#v", id, port, afterInvalid.Ports[id])
		}
	}

	badPortID, err := w.AddPlaceholderNode("example.com/BadNewPortReplacement", []pasta.Port{
		{Direction: "right", Name: "out", Types: []string{"example.com/typeA"}},
	})
	if err != nil {
		t.Fatalf("AddPlaceholderNode bad new port: %v", err)
	}
	badPortBefore := w.Snapshot()
	if err := w.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{name: "example.com/BadNewPortReplacement"},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			if len(previous) == 0 {
				return &workspaceNode{}, nil
			}
			previous[0].LeftPorts = append(previous[0].LeftPorts, pasta.Port{Direction: "left"})
			return &workspaceNode{}, nil
		},
	}); !errors.Is(err, pasta.ErrNoPortTypes) {
		t.Fatalf("AddNodeClass bad new replacement port error = %v, want %v", err, pasta.ErrNoPortTypes)
	}
	afterBadPort := w.Snapshot()
	if !afterBadPort.Nodes[badPortID].Placeholder ||
		!reflect.DeepEqual(afterBadPort.Nodes[badPortID].RightPorts, badPortBefore.Nodes[badPortID].RightPorts) ||
		len(afterBadPort.Ports) != len(badPortBefore.Ports) {
		t.Fatalf("bad new port replacement mutated workspace: before=%#v after=%#v", badPortBefore, afterBadPort)
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

func TestWorkspaceNodeNamesAreUniqueAndObservable(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeA, err := w.AddNode(&workspaceNode{}, "example.com/CalcDiv")
	if err != nil {
		t.Fatalf("AddNode generic: %v", err)
	}
	if got := w.Snapshot().Nodes[nodeA].Name; got != "CalcDiv 1" {
		t.Fatalf("generic node name = %q, want %q", got, "CalcDiv 1")
	}

	nodeB, err := w.AddNode(&workspaceNode{}, "example.com/CalcDiv", "custom []")
	if !errors.Is(err, pasta.ErrNodeName) {
		t.Fatalf("AddNode invalid name error = %v, want %v", err, pasta.ErrNodeName)
	}
	if nodeB != 0 {
		t.Fatalf("AddNode invalid name id = %d, want 0", nodeB)
	}

	nodeB, err = w.AddNode(&workspaceNode{}, "example.com/CalcDiv", "custom name")
	if err != nil {
		t.Fatalf("AddNode explicit: %v", err)
	}
	if got := w.Snapshot().Nodes[nodeB].Name; got != "custom name" {
		t.Fatalf("explicit node name = %q, want %q", got, "custom name")
	}
	if _, err := w.AddNode(&workspaceNode{}, "example.com/CalcDiv", "custom name"); !errors.Is(err, pasta.ErrNodeNameDup) {
		t.Fatalf("AddNode duplicate name error = %v, want %v", err, pasta.ErrNodeNameDup)
	}
	if err := w.SetNodeName(nodeB, "CalcDiv 1"); !errors.Is(err, pasta.ErrNodeNameDup) {
		t.Fatalf("SetNodeName duplicate error = %v, want %v", err, pasta.ErrNodeNameDup)
	}
	if err := w.SetNodeName(nodeB, "renamed"); err != nil {
		t.Fatalf("SetNodeName: %v", err)
	}
	if got := w.Snapshot().Nodes[nodeB].Name; got != "renamed" {
		t.Fatalf("renamed node name = %q, want %q", got, "renamed")
	}
	if err := w.SetNodeName(nodeB, ""); !errors.Is(err, pasta.ErrNodeName) {
		t.Fatalf("SetNodeName empty error = %v, want %v", err, pasta.ErrNodeName)
	}
	if err := w.SetNodeName(nodeB, "bad[name"); !errors.Is(err, pasta.ErrNodeName) {
		t.Fatalf("SetNodeName left bracket error = %v, want %v", err, pasta.ErrNodeName)
	}
	if err := w.SetNodeName(nodeB, "bad]name"); !errors.Is(err, pasta.ErrNodeName) {
		t.Fatalf("SetNodeName right bracket error = %v, want %v", err, pasta.ErrNodeName)
	}
	if err := w.SetNodeName(999, "missing"); !errors.Is(err, pasta.ErrNoNode) {
		t.Fatalf("SetNodeName missing node error = %v, want %v", err, pasta.ErrNoNode)
	}
}

func TestWorkspaceNodeNameValidationDoesNotConsumeIDs(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	if _, err := w.AddNode(&workspaceNode{}, "example.com/Node", "bad[name"); !errors.Is(err, pasta.ErrNodeName) {
		t.Fatalf("AddNode invalid name error = %v, want %v", err, pasta.ErrNodeName)
	}
	nodeID, err := w.AddNode(&workspaceNode{}, "example.com/Node")
	if err != nil {
		t.Fatalf("AddNode after invalid name: %v", err)
	}
	if got := w.Snapshot().Nodes[nodeID].Name; got != "Node 1" {
		t.Fatalf("name after invalid add = %q, want %q", got, "Node 1")
	}
	if nodeID != 1 {
		t.Fatalf("node id after invalid add = %d, want 1", nodeID)
	}
}

func TestWorkspaceNodeNameChangeNotifies(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	nodeID, err := w.AddNode(&workspaceNode{}, "example.com/Node")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	var notifications []pasta.WorkspaceNotification
	w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		notifications = append(notifications, notification)
	})
	if err := w.SetNodeName(nodeID, "visible header"); err != nil {
		t.Fatalf("SetNodeName: %v", err)
	}

	got := notifications[len(notifications)-1]
	if got.Kind != pasta.NotificationNodeUpdated || got.ID != nodeID || got.Node == nil || got.Node.Name != "visible header" {
		t.Fatalf("last notification = %#v, want node update with changed name", got)
	}
}

func TestWorkspaceReplacementCanSetOrRegenerateNodeName(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	nodeID, err := w.AddNode(&workspaceNode{}, "example.com/Node", "old name")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	replacement := &workspaceNode{}
	if err := w.ReplaceNodeWithName(nodeID, replacement, "new name"); err != nil {
		t.Fatalf("ReplaceNodeWithName explicit: %v", err)
	}
	if got := w.Snapshot().Nodes[nodeID].Name; got != "new name" {
		t.Fatalf("replacement name = %q, want %q", got, "new name")
	}
	if replacement.initData == nil || replacement.initData.Name != "new name" {
		t.Fatalf("replacement init data = %#v, want name %q", replacement.initData, "new name")
	}

	if err := w.ReplaceNodeWithPlaceholderWithName(nodeID, nil, ""); err != nil {
		t.Fatalf("ReplaceNodeWithPlaceholderWithName generic: %v", err)
	}
	if got := w.Snapshot().Nodes[nodeID].Name; got != "Node 1" {
		t.Fatalf("generic replacement name = %q, want %q", got, "Node 1")
	}
}

func TestWorkspaceReplacementNameFailuresDoNotMutate(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	nodeA := &workspaceNode{}
	nodeB := &workspaceNode{}
	nodeAID, err := w.AddNode(nodeA, "example.com/NodeA", "alpha")
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	nodeBID, err := w.AddNode(nodeB, "example.com/NodeB", "beta")
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}
	before := w.Snapshot()

	if err := w.ReplaceNodeWithName(nodeAID, &workspaceNode{}, "beta"); !errors.Is(err, pasta.ErrNodeNameDup) {
		t.Fatalf("ReplaceNodeWithName duplicate error = %v, want %v", err, pasta.ErrNodeNameDup)
	}
	if got := w.Snapshot(); !equalWorkspaceSnapshot(got, before) {
		t.Fatalf("duplicate replacement name mutated workspace: before=%#v after=%#v", before, got)
	}
	if nodeA.stopCount != 0 {
		t.Fatalf("duplicate replacement stopped old node = %d, want 0", nodeA.stopCount)
	}

	if err := w.ReplaceNodeWithPlaceholderWithName(nodeBID, nil, "bad]name"); !errors.Is(err, pasta.ErrNodeName) {
		t.Fatalf("ReplaceNodeWithPlaceholderWithName invalid error = %v, want %v", err, pasta.ErrNodeName)
	}
	if got := w.Snapshot(); !equalWorkspaceSnapshot(got, before) {
		t.Fatalf("invalid placeholder name mutated workspace: before=%#v after=%#v", before, got)
	}
	if nodeB.stopCount != 0 {
		t.Fatalf("invalid placeholder replacement stopped old node = %d, want 0", nodeB.stopCount)
	}
}

func TestWorkspacePlaceholderNameStateIsSuggestedAndApplied(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	placeholderID, err := w.AddPlaceholderNode("example.com/NamedRestore", nil, "legacy header")
	if err != nil {
		t.Fatalf("AddPlaceholderNode: %v", err)
	}
	if _, err := w.AddNode(&workspaceNode{}, "example.com/Peer", "peer header"); err != nil {
		t.Fatalf("AddNode peer: %v", err)
	}

	var suggested pasta.NodeClassState
	if err := w.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{name: "example.com/NamedRestore"},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			if len(previous) == 0 {
				return &workspaceNode{}, nil
			}
			suggested = *previous[0]
			previous[0].Name = "restored header"
			return &workspaceNode{}, nil
		},
	}); err != nil {
		t.Fatalf("AddNodeClass: %v", err)
	}
	if suggested.Name != "legacy header" {
		t.Fatalf("suggested placeholder name = %q, want %q", suggested.Name, "legacy header")
	}
	if got := w.Snapshot().Nodes[placeholderID].Name; got != "restored header" {
		t.Fatalf("restored node name = %q, want %q", got, "restored header")
	}
}

func TestWorkspacePlaceholderNameReplacementFailuresDoNotMutate(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	placeholderID, err := w.AddPlaceholderNode("example.com/InvalidNameRestore", nil, "legacy header")
	if err != nil {
		t.Fatalf("AddPlaceholderNode: %v", err)
	}
	if _, err := w.AddNode(&workspaceNode{}, "example.com/Peer", "peer header"); err != nil {
		t.Fatalf("AddNode peer: %v", err)
	}
	before := w.Snapshot()

	if err := w.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{name: "example.com/InvalidNameRestore"},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			if len(previous) == 0 {
				return &workspaceNode{}, nil
			}
			previous[0].Name = "peer header"
			return &workspaceNode{}, nil
		},
	}); !errors.Is(err, pasta.ErrNodeNameDup) {
		t.Fatalf("AddNodeClass duplicate placeholder replacement name error = %v, want %v", err, pasta.ErrNodeNameDup)
	}
	after := w.Snapshot()
	if !after.Nodes[placeholderID].Placeholder || !equalNodeSnapshot(after.Nodes[placeholderID], before.Nodes[placeholderID]) {
		t.Fatalf("duplicate placeholder replacement name mutated node: before=%#v after=%#v", before.Nodes[placeholderID], after.Nodes[placeholderID])
	}
	if len(after.Ports) != len(before.Ports) || len(after.Links) != len(before.Links) {
		t.Fatalf("duplicate placeholder replacement name mutated graph: before=%#v after=%#v", before, after)
	}
}

func TestWorkspaceGeneratesDeterministicRandomNodeNameFallback(t *testing.T) {
	first := pasta.NewWorkspace(&StringLoggerFactory{})
	second := pasta.NewWorkspace(&StringLoggerFactory{})

	firstName := forceGenericNameCollision(t, first)
	secondName := forceGenericNameCollision(t, second)
	if firstName != secondName {
		t.Fatalf("fallback names = %q and %q, want deterministic per workspace", firstName, secondName)
	}
	if strings.HasPrefix(firstName, "CalcDiv 3") || !strings.HasPrefix(firstName, "CalcDiv ") {
		t.Fatalf("fallback name = %q, want CalcDiv plus non-ID suffix", firstName)
	}
	suffix := strings.TrimPrefix(firstName, "CalcDiv ")
	if suffix == "" || !isASCIIAlpha(suffix[0]) {
		t.Fatalf("fallback suffix = %q, want first character to be a letter", suffix)
	}
}

func forceGenericNameCollision(t *testing.T, w *pasta.Workspace) string {
	t.Helper()

	if _, err := w.AddNode(&workspaceNode{}, "example.com/Other", "CalcDiv 3"); err != nil {
		t.Fatalf("AddNode colliding name holder: %v", err)
	}
	if _, err := w.AddNode(&workspaceNode{}, "example.com/Other"); err != nil {
		t.Fatalf("AddNode ID spacer: %v", err)
	}
	nodeID, err := w.AddNode(&workspaceNode{}, "example.com/CalcDiv")
	if err != nil {
		t.Fatalf("AddNode fallback: %v", err)
	}
	return w.Snapshot().Nodes[nodeID].Name
}

func isASCIIAlpha(c byte) bool {
	return ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z')
}

func TestWorkspaceNodePopupSnapshotsAndMutations(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeAID, err := w.AddNode(&workspaceNode{}, "example.com/NodeA")
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	nodeBID, err := w.AddNode(&workspaceNode{}, "example.com/NodeB")
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}

	var notifications []pasta.WorkspaceNotification
	w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		notifications = append(notifications, notification)
	})
	notifications = nil

	infoA, err := w.AddNodePopup(nodeAID, pasta.NodePopupInfo, "configure input", false)
	if err != nil {
		t.Fatalf("AddNodePopup info: %v", err)
	}
	wardA, err := w.AddNodePopup(nodeAID, pasta.NodePopupWard, "unstable input", false)
	if err != nil {
		t.Fatalf("AddNodePopup ward: %v", err)
	}
	infoADedup, err := w.AddNodePopup(nodeAID, pasta.NodePopupInfo, "configure input", true)
	if err != nil {
		t.Fatalf("AddNodePopup dedup: %v", err)
	}
	if infoADedup == infoA {
		t.Fatalf("deduplicated popup reused id %d", infoADedup)
	}
	errB, err := w.AddNodePopup(nodeBID, pasta.NodePopupErr, "configure input", false)
	if err != nil {
		t.Fatalf("AddNodePopup err: %v", err)
	}
	if err := w.SetNodePrimary(nodeAID, "example.com/typeA"); err != nil {
		t.Fatalf("SetNodePrimary: %v", err)
	}

	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {
				Class:       "example.com/NodeA",
				PrimaryType: "example.com/typeA",
				Popups: []pasta.NodePopup{
					{ID: wardA, Type: pasta.NodePopupWard, Text: "unstable input"},
					{ID: infoADedup, Type: pasta.NodePopupInfo, Text: "configure input"},
				},
			},
			nodeBID: {
				Class: "example.com/NodeB",
				Popups: []pasta.NodePopup{
					{ID: errB, Type: pasta.NodePopupErr, Text: "configure input"},
				},
			},
		},
	})
	assertNotificationMatches(t, notifications, []notificationMatch{
		{kind: pasta.NotificationNodeUpdated, id: nodeAID},
		{kind: pasta.NotificationNodeUpdated, id: nodeAID},
		{kind: pasta.NotificationNodeUpdated, id: nodeAID},
		{kind: pasta.NotificationNodeUpdated, id: nodeBID},
		{kind: pasta.NotificationNodeUpdated, id: nodeAID},
	})
	if got := notifications[2].Node.Popups; !reflect.DeepEqual(got, []pasta.NodePopup{{ID: wardA, Type: pasta.NodePopupWard, Text: "unstable input"}, {ID: infoADedup, Type: pasta.NodePopupInfo, Text: "configure input"}}) {
		t.Fatalf("deduplicated notification popups = %#v", got)
	}

	if err := w.RemoveNodePopup(nodeAID, errB); err != nil {
		t.Fatalf("RemoveNodePopup wrong node: %v", err)
	}
	if err := w.RemoveNodePopup(nodeAID, wardA); err != nil {
		t.Fatalf("RemoveNodePopup(%d): %v", wardA, err)
	}
	if err := w.RemoveNodePopup(nodeAID, wardA); err != nil {
		t.Fatalf("second RemoveNodePopup(%d): %v", wardA, err)
	}
	if err := w.RemoveNodePopupsByText(nodeAID, "configure input"); err != nil {
		t.Fatalf("RemoveNodePopupsByText node A: %v", err)
	}
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {Class: "example.com/NodeA", PrimaryType: "example.com/typeA"},
			nodeBID: {
				Class: "example.com/NodeB",
				Popups: []pasta.NodePopup{
					{ID: errB, Type: pasta.NodePopupErr, Text: "configure input"},
				},
			},
		},
	})
	if err := w.RemoveNodePopupsByText(nodeBID, "configure input"); err != nil {
		t.Fatalf("RemoveNodePopupsByText node B: %v", err)
	}

	wardA, err = w.AddNodePopup(nodeAID, pasta.NodePopupWard, "late", false)
	if err != nil {
		t.Fatalf("AddNodePopup late A: %v", err)
	}
	wardB, err := w.AddNodePopup(nodeBID, pasta.NodePopupWard, "late", false)
	if err != nil {
		t.Fatalf("AddNodePopup late B: %v", err)
	}
	if err := w.RemoveNodePopupsByType(nodeAID, pasta.NodePopupWard); err != nil {
		t.Fatalf("RemoveNodePopupsByType: %v", err)
	}
	if err := w.RemoveNodePopup(nodeAID, wardA); err != nil {
		t.Fatalf("RemoveNodePopup(%d) after type removal: %v", wardA, err)
	}
	assertWorkspaceSnapshot(t, w, pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {Class: "example.com/NodeA", PrimaryType: "example.com/typeA"},
			nodeBID: {
				Class: "example.com/NodeB",
				Popups: []pasta.NodePopup{
					{ID: wardB, Type: pasta.NodePopupWard, Text: "late"},
				},
			},
		},
	})
	if err := w.RemoveNodePopups(nodeBID); err != nil {
		t.Fatalf("RemoveNodePopups empty: %v", err)
	}

	if _, err := w.AddNodePopup(nodeAID, "warn", "bad type", false); !errors.Is(err, pasta.ErrNodePopupType) {
		t.Fatalf("AddNodePopup invalid type error = %v, want %v", err, pasta.ErrNodePopupType)
	}
	if _, err := w.AddNodePopup(999, pasta.NodePopupInfo, "missing", false); !errors.Is(err, pasta.ErrNoNode) {
		t.Fatalf("AddNodePopup missing node error = %v, want %v", err, pasta.ErrNoNode)
	}
	if err := w.RemoveNodePopups(999); !errors.Is(err, pasta.ErrNoNode) {
		t.Fatalf("RemoveNodePopups missing node error = %v, want %v", err, pasta.ErrNoNode)
	}
	if err := w.RemoveNodePopup(999, wardA); !errors.Is(err, pasta.ErrNoNode) {
		t.Fatalf("RemoveNodePopup missing node error = %v, want %v", err, pasta.ErrNoNode)
	}
	if err := w.RemoveNodePopupsByText(999, "missing"); !errors.Is(err, pasta.ErrNoNode) {
		t.Fatalf("RemoveNodePopupsByText missing node error = %v, want %v", err, pasta.ErrNoNode)
	}
	if err := w.RemoveNodePopupsByType(nodeAID, "warn"); !errors.Is(err, pasta.ErrNodePopupType) {
		t.Fatalf("RemoveNodePopupsByType invalid type error = %v, want %v", err, pasta.ErrNodePopupType)
	}
	if err := w.RemoveNodePopupsByType(999, pasta.NodePopupInfo); !errors.Is(err, pasta.ErrNoNode) {
		t.Fatalf("RemoveNodePopupsByType missing node error = %v, want %v", err, pasta.ErrNoNode)
	}
}

func TestWorkspaceNodeCallbackFailuresBecomePlaceholders(t *testing.T) {
	failErr := errors.New("boom")

	t.Run("OnInit", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		node := &workspaceNode{failOn: map[string]error{"OnInit": failErr}}

		nodeID, err := w.AddNode(node, "example.com/Node")
		if !errors.Is(err, failErr) {
			t.Fatalf("AddNode error = %v, want %v", err, failErr)
		}
		assertFailedPlaceholder(t, w, nodeID, "OnInit", "boom")
		if node.stopCount != 1 {
			t.Fatalf("stop count = %d, want 1", node.stopCount)
		}
	})

	t.Run("OnReady", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		node := &workspaceNode{failOn: map[string]error{"OnReady": failErr}}

		nodeID, err := w.AddNode(node, "example.com/Node")
		if !errors.Is(err, failErr) {
			t.Fatalf("AddNode error = %v, want %v", err, failErr)
		}
		assertFailedPlaceholder(t, w, nodeID, "OnReady", "boom")
		if node.stopCount != 1 {
			t.Fatalf("stop count = %d, want 1", node.stopCount)
		}
	})

	t.Run("OnReady panic", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		node := &workspaceNode{panicOn: map[string]bool{"OnReady": true}}

		nodeID, err := w.AddNode(node, "example.com/Node")
		if !errors.Is(err, pasta.ErrNodePanic) {
			t.Fatalf("AddNode error = %v, want %v", err, pasta.ErrNodePanic)
		}
		assertFailedPlaceholder(t, w, nodeID, "OnReady", "node panic")
		if node.stopCount != 1 {
			t.Fatalf("stop count = %d, want 1", node.stopCount)
		}
	})

	t.Run("OnRootStatus", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		node := &workspaceNode{failOn: map[string]error{"OnRootStatus": failErr}}

		nodeID, err := w.AddRootNode(node, "example.com/Node")
		if !errors.Is(err, failErr) {
			t.Fatalf("AddRootNode error = %v, want %v", err, failErr)
		}
		assertFailedPlaceholder(t, w, nodeID, "OnRootStatus", "boom")
		if node.stopCount != 1 {
			t.Fatalf("stop count = %d, want 1", node.stopCount)
		}
	})

	t.Run("OnPortAdd", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		node := &workspaceNode{failOn: map[string]error{"OnPortAdd": failErr}}
		nodeID, err := w.AddNode(node, "example.com/Node")
		if err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		addOldPopup(t, w, nodeID)

		if _, err := w.AddPort(pasta.Port{Node: nodeID, Direction: "left", Name: "left1", Types: []string{"example.com/typeA"}}); !errors.Is(err, failErr) {
			t.Fatalf("AddPort error = %v, want %v", err, failErr)
		}
		assertFailedPlaceholder(t, w, nodeID, "OnPortAdd", "boom")
	})

	t.Run("OnPortRemoved", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		node := &workspaceNode{}
		nodeID, err := w.AddNode(node, "example.com/Node")
		if err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		port := mustAddPort(t, w, nodeID, "left", "example.com/typeA")
		addOldPopup(t, w, nodeID)
		node.failOn = map[string]error{"OnPortRemoved": failErr}

		w.RemovePort(port)
		assertFailedPlaceholder(t, w, nodeID, "OnPortRemoved", "boom")
	})

	t.Run("PreLinkAdd panic", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		leftID, rightID, leftPort, rightPort := addLinkedPairNodes(t, w, &workspaceNode{panicOn: map[string]bool{"PreLinkAdd": true}}, &workspaceNode{})
		addOldPopup(t, w, leftID)

		if _, _, err := w.AddLink(leftPort, rightPort); !errors.Is(err, pasta.ErrNodePanic) {
			t.Fatalf("AddLink error = %v, want %v", err, pasta.ErrNodePanic)
		}
		assertFailedPlaceholder(t, w, leftID, "PreLinkAdd", "node panic")
		assertLiveNode(t, w, rightID)
	})

	t.Run("PreLinkAdd rejection", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		leftID, rightID, leftPort, rightPort := addLinkedPairNodes(t, w, &workspaceNode{failOn: map[string]error{"PreLinkAdd": failErr}}, &workspaceNode{})
		popupID := addOldPopup(t, w, leftID)

		if _, _, err := w.AddLink(leftPort, rightPort); !errors.Is(err, failErr) {
			t.Fatalf("AddLink error = %v, want %v", err, failErr)
		}
		assertLiveNode(t, w, leftID)
		assertLiveNode(t, w, rightID)
		if got := w.Snapshot().Nodes[leftID].Popups; !reflect.DeepEqual(got, []pasta.NodePopup{{ID: popupID, Type: pasta.NodePopupInfo, Text: "old popup"}}) {
			t.Fatalf("popups after rejection = %#v, want old popup preserved", got)
		}
	})

	t.Run("OnLinkAdd", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		leftID, _, leftPort, rightPort := addLinkedPairNodes(t, w, &workspaceNode{failOn: map[string]error{"OnLinkAdd": failErr}}, &workspaceNode{})
		addOldPopup(t, w, leftID)

		if _, _, err := w.AddLink(leftPort, rightPort); !errors.Is(err, failErr) {
			t.Fatalf("AddLink error = %v, want %v", err, failErr)
		}
		assertFailedPlaceholder(t, w, leftID, "OnLinkAdd", "boom")
	})

	t.Run("OnLinkRemoved", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		left := &workspaceNode{}
		leftID, _, leftPort, rightPort := addLinkedPairNodes(t, w, left, &workspaceNode{})
		link, _, err := w.AddLink(leftPort, rightPort)
		if err != nil {
			t.Fatalf("AddLink: %v", err)
		}
		addOldPopup(t, w, leftID)
		left.failOn = map[string]error{"OnLinkRemoved": failErr}

		w.RemoveLink(link)
		assertFailedPlaceholder(t, w, leftID, "OnLinkRemoved", "boom")
	})

	t.Run("OnEvent", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		receiver := &workspaceNode{failOn: map[string]error{"OnEvent": failErr}}
		senderID, receiverID, senderPort, receiverPort := addLinkedPairNodes(t, w, &workspaceNode{}, receiver)
		if _, _, err := w.AddLink(senderPort, receiverPort); err != nil {
			t.Fatalf("AddLink: %v", err)
		}
		addOldPopup(t, w, receiverID)

		w.SendEvent(pasta.Event{SenderNode: senderID, SenderPort: senderPort, ReceiverNode: receiverID, ReceiverPort: receiverPort, Payload: "payload"})
		assertFailedPlaceholder(t, w, receiverID, "OnEvent", "boom")
	})

	t.Run("OnInbox", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		node := &workspaceNode{failOn: map[string]error{"OnInbox": failErr}}
		nodeID, err := w.AddNode(node, "example.com/Node")
		if err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		addOldPopup(t, w, nodeID)

		var notifications []pasta.WorkspaceNotification
		w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
			notifications = append(notifications, notification)
		})
		notifications = nil

		w.SendInbox(pasta.InboxMessage{ReceiverNode: nodeID, Payload: "payload"})
		assertFailedPlaceholder(t, w, nodeID, "OnInbox", "boom")
		assertHasNotification(t, notifications, pasta.NotificationNodeUpdated, nodeID)
		if got := notifications[len(notifications)-1].Node.Popups; len(got) != 1 || got[0].Type != pasta.NodePopupErr {
			t.Fatalf("failure notification popups = %#v, want one error popup", got)
		}
	})

	t.Run("OnFormularMsg", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		node := &workspaceNode{failOn: map[string]error{"OnFormularMsg": failErr}}
		nodeID, err := w.AddNode(node, "example.com/Node")
		if err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		addOldPopup(t, w, nodeID)

		var notifications []pasta.WorkspaceNotification
		w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
			notifications = append(notifications, notification)
		})
		notifications = nil

		w.SendNodeFormularMsg(nodeID, "payload")
		assertFailedPlaceholder(t, w, nodeID, "OnFormularMsg", "boom")
		assertHasNotification(t, notifications, pasta.NotificationNodeUpdated, nodeID)
		if got := notifications[len(notifications)-1].Node.Popups; len(got) != 1 || got[0].Type != pasta.NodePopupErr {
			t.Fatalf("failure notification popups = %#v, want one error popup", got)
		}
	})

	t.Run("OnTrigger", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		node := &workspaceNode{failOn: map[string]error{"OnTrigger": failErr}}
		nodeID, err := w.AddNode(node, "example.com/Node")
		if err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		addOldPopup(t, w, nodeID)

		var notifications []pasta.WorkspaceNotification
		w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
			notifications = append(notifications, notification)
		})
		notifications = nil

		if err := w.Trigger(nodeID); !errors.Is(err, failErr) {
			t.Fatalf("Trigger error = %v, want %v", err, failErr)
		}
		assertFailedPlaceholder(t, w, nodeID, "OnTrigger", "boom")
		assertHasNotification(t, notifications, pasta.NotificationNodeUpdated, nodeID)
		if got := notifications[len(notifications)-1].Node.Popups; len(got) != 1 || got[0].Type != pasta.NodePopupErr {
			t.Fatalf("failure notification popups = %#v, want one error popup", got)
		}
	})

	t.Run("OnTrigger panic", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		node := &workspaceNode{panicOn: map[string]bool{"OnTrigger": true}}
		nodeID, err := w.AddNode(node, "example.com/Node")
		if err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		addOldPopup(t, w, nodeID)

		if err := w.Trigger(nodeID); !errors.Is(err, pasta.ErrNodePanic) {
			t.Fatalf("Trigger error = %v, want %v", err, pasta.ErrNodePanic)
		}
		assertFailedPlaceholder(t, w, nodeID, "OnTrigger", "node panic")
	})
}

func TestWorkspaceTrigger(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	node := &workspaceNode{}
	nodeID, err := w.AddNode(node, "example.com/TriggerNode")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	if err := w.Trigger(nodeID); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if node.triggerCount != 1 {
		t.Fatalf("trigger count = %d, want 1", node.triggerCount)
	}

	w.Lock()
	if err := w.TriggerLocked(nodeID); err != nil {
		w.Unlock()
		t.Fatalf("TriggerLocked: %v", err)
	}
	w.Unlock()
	if node.triggerCount != 2 {
		t.Fatalf("trigger count after TriggerLocked = %d, want 2", node.triggerCount)
	}
}

func TestWorkspaceTriggerBasicNodeNoop(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	nodeID, err := w.AddNode(pasta.BasicNode{}, "example.com/BasicNode")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	if err := w.Trigger(nodeID); err != nil {
		t.Fatalf("Trigger BasicNode: %v", err)
	}
}

func TestWorkspaceTriggerRejectsUnavailableNode(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		if err := w.Trigger(999); !errors.Is(err, pasta.ErrNoNode) {
			t.Fatalf("Trigger missing node error = %v, want %v", err, pasta.ErrNoNode)
		}
	})

	t.Run("placeholder", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		nodeID, err := w.AddPlaceholderNode("example.com/Missing", nil)
		if err != nil {
			t.Fatalf("AddPlaceholderNode: %v", err)
		}
		if err := w.Trigger(nodeID); !errors.Is(err, pasta.ErrNoNode) {
			t.Fatalf("Trigger placeholder error = %v, want %v", err, pasta.ErrNoNode)
		}
	})

	t.Run("closed", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		nodeID, err := w.AddNode(&workspaceNode{}, "example.com/TriggerNode")
		if err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		w.Close()
		if err := w.Trigger(nodeID); !errors.Is(err, pasta.ErrWorkspaceClosed) {
			t.Fatalf("Trigger closed workspace error = %v, want %v", err, pasta.ErrWorkspaceClosed)
		}
	})
}

func TestWorkspaceLogsNodeFailurePlaceholderReplacement(t *testing.T) {
	t.Run("callback error", func(t *testing.T) {
		logf := &StringLoggerFactory{}
		w := pasta.NewWorkspace(logf)
		node := &workspaceNode{failOn: map[string]error{"OnInbox": errors.New("logged boom")}}
		nodeID, err := w.AddNode(node, "example.com/LoggedNode")
		if err != nil {
			t.Fatalf("AddNode: %v", err)
		}

		w.SendInbox(pasta.InboxMessage{ReceiverNode: nodeID, Payload: "payload"})

		assertFailedPlaceholder(t, w, nodeID, "OnInbox", "logged boom")
		assertNodeFailureLog(t, logf.Result(), nodeID, "example.com/LoggedNode", "OnInbox", "logged boom")
	})

	t.Run("callback panic", func(t *testing.T) {
		logf := &StringLoggerFactory{}
		w := pasta.NewWorkspace(logf)
		node := &workspaceNode{panicOn: map[string]bool{"OnReady": true}}

		nodeID, err := w.AddNode(node, "example.com/PanicNode")
		if !errors.Is(err, pasta.ErrNodePanic) {
			t.Fatalf("AddNode error = %v, want %v", err, pasta.ErrNodePanic)
		}

		assertFailedPlaceholder(t, w, nodeID, "OnReady", "node panic")
		assertNodeFailureLog(t, logf.Result(), nodeID, "example.com/PanicNode", "OnReady", "node panic")
	})
}

func TestWorkspaceReplaceNodeFailuresBecomePlaceholders(t *testing.T) {
	failErr := errors.New("replace boom")

	for _, callback := range []string{"OnInit", "OnReady"} {
		t.Run(callback, func(t *testing.T) {
			w := pasta.NewWorkspace(&StringLoggerFactory{})
			nodeID, err := w.AddNode(&workspaceNode{}, "example.com/Node")
			if err != nil {
				t.Fatalf("AddNode: %v", err)
			}
			addOldPopup(t, w, nodeID)

			err = w.ReplaceNode(nodeID, &workspaceNode{failOn: map[string]error{callback: failErr}})
			if !errors.Is(err, failErr) {
				t.Fatalf("ReplaceNode error = %v, want %v", err, failErr)
			}
			assertFailedPlaceholder(t, w, nodeID, callback, "replace boom")
		})
	}

	t.Run("OnRootStatus", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		nodeID, err := w.AddRootNode(&workspaceNode{}, "example.com/Node")
		if err != nil {
			t.Fatalf("AddRootNode: %v", err)
		}
		addOldPopup(t, w, nodeID)

		err = w.ReplaceNode(nodeID, &workspaceNode{failOn: map[string]error{"OnRootStatus": failErr}})
		if !errors.Is(err, failErr) {
			t.Fatalf("ReplaceNode error = %v, want %v", err, failErr)
		}
		assertFailedPlaceholder(t, w, nodeID, "OnRootStatus", "replace boom")
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
	if got, want := legacy.initFlags, (workspaceNodeInitFlags{}); got != want {
		t.Fatalf("legacy init flags = %#v, want %#v", got, want)
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
	if got, want := replacement.initFlags, (workspaceNodeInitFlags{isReplacement: true}); got != want {
		t.Fatalf("replacement init flags = %#v, want %#v", got, want)
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

func TestWorkspaceReplaceNodeClearsPopupsAndNotifies(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeID, err := w.AddNode(&workspaceNode{}, "example.com/Node")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if _, err := w.AddNodePopup(nodeID, pasta.NodePopupErr, "old implementation", false); err != nil {
		t.Fatalf("AddNodePopup: %v", err)
	}
	var notifications []pasta.WorkspaceNotification
	w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		notifications = append(notifications, notification)
	})
	notifications = nil

	replacement := &workspaceNode{}
	if err := w.ReplaceNode(nodeID, replacement); err != nil {
		t.Fatalf("ReplaceNode: %v", err)
	}
	if got := w.Snapshot().Nodes[nodeID].Popups; len(got) != 0 {
		t.Fatalf("popups after ReplaceNode = %#v, want empty", got)
	}
	assertNotificationMatches(t, notifications, []notificationMatch{
		{kind: pasta.NotificationNodeUpdated, id: nodeID},
	})

	if _, err := w.AddNodePopup(nodeID, pasta.NodePopupWard, "placeholder warning", false); err != nil {
		t.Fatalf("AddNodePopup before placeholder: %v", err)
	}
	notifications = nil
	if err := w.ReplaceNodeWithPlaceholder(nodeID, nil); err != nil {
		t.Fatalf("ReplaceNodeWithPlaceholder: %v", err)
	}
	if got := w.Snapshot().Nodes[nodeID].Popups; len(got) != 0 {
		t.Fatalf("popups after ReplaceNodeWithPlaceholder = %#v, want empty", got)
	}
	assertHasNotification(t, notifications, pasta.NotificationNodeUpdated, nodeID)

	if _, err := w.AddNodePopup(nodeID, pasta.NodePopupInfo, "placeholder info", false); err != nil {
		t.Fatalf("AddNodePopup on placeholder: %v", err)
	}
	notifications = nil
	if err := w.ReplacePlaceholderNode(nodeID, &workspaceNode{}); err != nil {
		t.Fatalf("ReplacePlaceholderNode: %v", err)
	}
	if got := w.Snapshot().Nodes[nodeID].Popups; len(got) != 0 {
		t.Fatalf("popups after ReplacePlaceholderNode = %#v, want empty", got)
	}
	assertHasNotification(t, notifications, pasta.NotificationNodeUpdated, nodeID)
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
		{Direction: "right", Name: "right1", Types: []string{"example.com/typeA"}},
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
	if got, want := replacement.initFlags, (workspaceNodeInitFlags{
		isReplacement:            true,
		isPlaceholderReplacement: true,
	}); got != want {
		t.Fatalf("replacement init flags = %#v, want %#v", got, want)
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
		{Direction: "right", Name: "right2", Types: []string{pasta.AnyType}},
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
		{Direction: "left", Name: "left1", Types: []string{"example.com/typeA"}},
		{Direction: "right", Name: "right1", Types: []string{"example.com/typeA"}},
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
			leftA:  {Node: nodeID, Direction: "left", Name: "left1", Types: []string{"example.com/typeA"}},
			leftB:  {Node: nodeID, Direction: "left", Name: "left2", Types: []string{"example.com/typeA"}},
			leftC:  {Node: nodeID, Direction: "left", Name: "left3", Types: []string{"example.com/typeA"}},
			rightA: {Node: nodeID, Direction: "right", Name: "right1", Types: []string{"example.com/typeA"}},
			rightB: {Node: nodeID, Direction: "right", Name: "right2", Types: []string{"example.com/typeA"}},
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
		Name:      "right1",
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
		Name:      "left1",
		Types:     []string{"example.com/typeD", "example.com/typeE"},
	})
	if err != nil {
		t.Fatalf("AddPort multi left: %v", err)
	}
	singleAnyRight, err := w.AddPort(pasta.Port{
		Node:      nodeF,
		Direction: "right",
		Name:      "right1",
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
	if err := w.SetNodeName(nodeAID, "closed"); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("SetNodeName after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if _, err := w.AddNodePopup(nodeAID, pasta.NodePopupInfo, "closed", false); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("AddNodePopup after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.RemoveNodePopups(nodeAID); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("RemoveNodePopups after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.RemoveNodePopupsByType(nodeAID, pasta.NodePopupInfo); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("RemoveNodePopupsByType after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.RemoveNodePopup(nodeAID, 1); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("RemoveNodePopup after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.RemoveNodePopupsByText(nodeAID, "closed"); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("RemoveNodePopupsByText after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
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
	if err := w.AddNodeClass(testNodeClass{name: "example.com/NodeClass"}); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("AddNodeClass after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.RemoveNodeClass("example.com/NodeA"); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("RemoveNodeClass after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if _, err := w.AddNodeByClass("example.com/NodeA"); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("AddNodeByClass after Close error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if _, ok := w.NodeClass("example.com/NodeA"); ok {
		t.Fatal("NodeClass after Close returned ok")
	}
	if _, ok := w.NodeClassLongDescription("example.com/NodeA"); ok {
		t.Fatal("NodeClassLongDescription after Close returned ok")
	}
	if _, ok := w.NodeClassSnapshot("example.com/NodeA"); ok {
		t.Fatal("NodeClassSnapshot after Close returned ok")
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

func assertFailedPlaceholder(t *testing.T, w *pasta.Workspace, nodeID uint64, callback, reason string) {
	t.Helper()

	snapshot, ok := w.NodeSnapshot(nodeID)
	if !ok {
		t.Fatalf("NodeSnapshot(%d) returned false", nodeID)
	}
	if !snapshot.Placeholder {
		t.Fatalf("node %d snapshot = %#v, want placeholder", nodeID, snapshot)
	}
	if len(snapshot.Popups) != 1 {
		t.Fatalf("node %d popups = %#v, want one failure popup", nodeID, snapshot.Popups)
	}
	popup := snapshot.Popups[0]
	if popup.Type != pasta.NodePopupErr {
		t.Fatalf("failure popup type = %q, want %q", popup.Type, pasta.NodePopupErr)
	}
	if !strings.Contains(popup.Text, callback) || !strings.Contains(popup.Text, reason) {
		t.Fatalf("failure popup text = %q, want callback %q and reason %q", popup.Text, callback, reason)
	}
}

func assertNodeFailureLog(t *testing.T, logs string, nodeID uint64, class, callback, cause string) {
	t.Helper()

	for _, part := range []string{
		"workspace[err]node callback failed; replacing node with placeholder",
		fmt.Sprintf("node=%d", nodeID),
		"class=" + class,
		"callback=" + callback,
		"cause=" + cause,
	} {
		if !strings.Contains(logs, part) {
			t.Fatalf("failure log missing %q in:\n%s", part, logs)
		}
	}
}

func assertLiveNode(t *testing.T, w *pasta.Workspace, nodeID uint64) {
	t.Helper()

	snapshot, ok := w.NodeSnapshot(nodeID)
	if !ok {
		t.Fatalf("NodeSnapshot(%d) returned false", nodeID)
	}
	if snapshot.Placeholder {
		t.Fatalf("node %d snapshot = %#v, want live node", nodeID, snapshot)
	}
}

func addOldPopup(t *testing.T, w *pasta.Workspace, nodeID uint64) uint64 {
	t.Helper()

	popupID, err := w.AddNodePopup(nodeID, pasta.NodePopupInfo, "old popup", false)
	if err != nil {
		t.Fatalf("AddNodePopup old: %v", err)
	}
	return popupID
}

func addLinkedPairNodes(t *testing.T, w *pasta.Workspace, left, right *workspaceNode) (uint64, uint64, uint64, uint64) {
	t.Helper()

	leftID, err := w.AddNode(left, "example.com/Left")
	if err != nil {
		t.Fatalf("AddNode left: %v", err)
	}
	rightID, err := w.AddNode(right, "example.com/Right")
	if err != nil {
		t.Fatalf("AddNode right: %v", err)
	}
	leftPort := mustAddPort(t, w, leftID, "left", "example.com/typeA")
	rightPort := mustAddPort(t, w, rightID, "right", "example.com/typeA")
	return leftID, rightID, leftPort, rightPort
}

func assertJSONSerializable(t *testing.T, value any) {
	t.Helper()

	if _, err := json.Marshal(value); err != nil {
		t.Fatalf("snapshot is not JSON serializable: %v", err)
	}
}

func equalWorkspaceSnapshot(a, b pasta.WorkspaceSnapshot) bool {
	if len(a.Classes) != len(b.Classes) || len(a.Nodes) != len(b.Nodes) || len(a.Ports) != len(b.Ports) || len(a.Links) != len(b.Links) {
		return false
	}
	for name, class := range a.Classes {
		if !equalNodeClassSnapshot(class, b.Classes[name]) {
			return false
		}
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

func equalNodeClassSnapshot(a, b pasta.NodeClassSnapshot) bool {
	return a.Class == b.Class &&
		a.ShortDescription == b.ShortDescription &&
		a.Unique == b.Unique &&
		a.PrimaryType == b.PrimaryType &&
		reflect.DeepEqual(emptyClassPortsIfNil(a.InitialPorts), emptyClassPortsIfNil(b.InitialPorts))
}

func emptyClassPortsIfNil(values []pasta.NodeClassPortSnapshot) []pasta.NodeClassPortSnapshot {
	if values == nil {
		return []pasta.NodeClassPortSnapshot{}
	}
	return values
}

func equalNodeSnapshot(a, b pasta.NodeSnapshot) bool {
	return a.Class == b.Class &&
		(b.Name == "" || a.Name == b.Name) &&
		a.PrimaryType == b.PrimaryType &&
		a.Label == b.Label &&
		a.Position == b.Position &&
		reflect.DeepEqual(emptyPopupsIfNil(a.Popups), emptyPopupsIfNil(b.Popups)) &&
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

func emptyPopupsIfNil(values []pasta.NodePopup) []pasta.NodePopup {
	if values == nil {
		return []pasta.NodePopup{}
	}
	return values
}

func mustAddPort(t *testing.T, w *pasta.Workspace, node uint64, direction, typ string) uint64 {
	t.Helper()

	snapshot, ok := w.NodeSnapshot(node)
	if !ok {
		t.Fatalf("NodeSnapshot(%d) missing", node)
	}
	count := len(snapshot.LeftPorts)
	if direction == "right" {
		count = len(snapshot.RightPorts)
	}
	id, err := w.AddPort(pasta.Port{
		Node:      node,
		Direction: direction,
		Name:      fmt.Sprintf("%s%d", direction, count+1),
		Types:     []string{typ},
	})
	if err != nil {
		t.Fatalf("AddPort(%d, %s, %s): %v", node, direction, typ, err)
	}
	return id
}

func mustAddAnyPlaceholder(t *testing.T, w *pasta.Workspace, label string) (uint64, uint64) {
	t.Helper()

	id, err := w.AddPlaceholderNode("example.com/AnyRestored", []pasta.Port{
		{Direction: "right", Name: "right1", Types: []string{pasta.AnyType}},
	})
	if err != nil {
		t.Fatalf("AddPlaceholderNode %q: %v", label, err)
	}
	if err := w.SetNodeLabel(id, label); err != nil {
		t.Fatalf("SetNodeLabel %q: %v", label, err)
	}
	snapshot, ok := w.NodeSnapshot(id)
	if !ok || len(snapshot.RightPorts) != 1 {
		t.Fatalf("placeholder %q snapshot = %#v, %v", label, snapshot, ok)
	}
	return id, snapshot.RightPorts[0]
}
