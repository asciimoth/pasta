// nolint
package main

import (
	"testing"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/pasta/pasta/std"
)

const nodeTypeTestNetworkConsumer = "demo.pasta/TestNetworkConsumer"

type testNetworkConsumerClass struct {
	nodes []*testNetworkConsumerNode
}

func (c *testNetworkConsumerClass) ClassName() string        { return nodeTypeTestNetworkConsumer }
func (c *testNetworkConsumerClass) ShortDescription() string { return "test network consumer" }
func (c *testNetworkConsumerClass) LongDescription() string  { return "test network consumer" }
func (c *testNetworkConsumerClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: typeNetwork, InitialPorts: []pasta.Port{
		networkPort("right", "Network"),
	}}
}
func (c *testNetworkConsumerClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	node := &testNetworkConsumerNode{}
	c.nodes = append(c.nodes, node)
	return node, nil
}

type testNetworkConsumerNode struct {
	pasta.BasicNode

	w       *pasta.Workspace
	id      uint64
	netp    uint64
	network networkCloser
	events  int
}

func (n *testNetworkConsumerNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil && len(restored.RightPorts) > 0 {
		n.netp = restored.RightPorts[0]
	}
	return n.w.SetNodePrimary(n.id, typeNetwork)
}

func (n *testNetworkConsumerNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if port != n.netp || portDirection != "right" || linkType != typeNetwork {
		return rejectUnsupportedDemoType(linkType)
	}
	snapshot, ok := n.w.PortSnapshot(port)
	if ok && len(snapshot.Links) > 0 {
		return pasta.ErrLinkDup
	}
	return nil
}

func (n *testNetworkConsumerNode) OnLinkAdd(link, port uint64, _, _ string) error {
	if port == n.netp {
		n.requestLink(link)
	}
	return nil
}

func (n *testNetworkConsumerNode) OnLinkRemoved(_ uint64, port uint64, _, _ string) error {
	if port == n.netp {
		n.network = nil
	}
	return nil
}

func (n *testNetworkConsumerNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if event.ReceiverPort != n.netp || receiverPortDirection != "right" || linkType != typeNetwork {
		return nil
	}
	payload, ok := event.Payload.(networkPayload)
	if !ok || payload.Network == nil {
		return nil
	}
	link := linkIDForEvent(n.w, event)
	bindNetworkResource(n.w, n.id, link, payload.Network)
	n.network = payload.Network
	n.events++
	return nil
}

func (n *testNetworkConsumerNode) requestLink(link uint64) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.netp)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: n.netp, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: std.RequestValue{}})
}

func TestSelectSwitchClosesInactiveNetworkAndRefreshesActiveNetwork(t *testing.T) {
	w, consumerClass := newNetworkSelectWorkspace(t)
	defer w.Close()

	loopback := addDemoByClass(t, w, nodeTypeLoopback, "loopback")
	selectNode := addDemoByClass(t, w, std.NodeTypeSelect, "select")
	falseNode := addDemoByClass(t, w, std.NodeTypeFalseConstant, "false")
	trueNode := addDemoByClass(t, w, std.NodeTypeTrueConstant, "true")
	consumer0 := addDemoByClass(t, w, consumerClass.ClassName(), "consumer0")
	consumer1 := addDemoByClass(t, w, consumerClass.ClassName(), "consumer1")
	consumer0Node := consumerClass.nodes[0]
	consumer1Node := consumerClass.nodes[1]

	linkByDemoPortName(t, w, selectNode, "Out", loopback, "Network")
	linkByDemoPortName(t, w, falseNode, "output", selectNode, "Selector")
	linkByDemoPortName(t, w, consumer0, "Network", selectNode, "In 0")
	linkByDemoPortName(t, w, consumer1, "Network", selectNode, "In 1")

	first0 := requireConsumerNetwork(t, consumer0Node, "consumer0 initial")
	requireNoConsumerNetwork(t, consumer1Node, "consumer1 inactive initial")
	requireNetworkOpen(t, first0, "consumer0 initial")

	w.RemoveLink(linkIDByDemoPortNames(t, w, falseNode, "output", selectNode, "Selector"))
	linkByDemoPortName(t, w, trueNode, "output", selectNode, "Selector")

	requireNetworkClosed(t, first0, "consumer0 after switch to In 1")
	if consumer0Node.network != first0 {
		t.Fatal("consumer0 network changed while switching away from In 0")
	}
	first1 := requireConsumerNetwork(t, consumer1Node, "consumer1 after switch")
	requireNetworkOpen(t, first1, "consumer1 after switch")
	if first1 == first0 {
		t.Fatal("consumer1 received the same network instance that consumer0 had before switch")
	}
}

func TestSelectNetworkLifecycleAfterConsumerAndProviderReattach(t *testing.T) {
	w, consumerClass := newNetworkSelectWorkspace(t)
	defer w.Close()

	loopback := addDemoByClass(t, w, nodeTypeLoopback, "loopback")
	selectNode := addDemoByClass(t, w, std.NodeTypeSelect, "select")
	falseNode := addDemoByClass(t, w, std.NodeTypeFalseConstant, "false")
	trueNode := addDemoByClass(t, w, std.NodeTypeTrueConstant, "true")
	consumer0 := addDemoByClass(t, w, consumerClass.ClassName(), "consumer0")
	consumer1 := addDemoByClass(t, w, consumerClass.ClassName(), "consumer1")
	consumer0Node := consumerClass.nodes[0]
	consumer1Node := consumerClass.nodes[1]

	providerLink := linkByDemoPortName(t, w, selectNode, "Out", loopback, "Network")
	selectorLink := linkByDemoPortName(t, w, falseNode, "output", selectNode, "Selector")
	consumer0Link := linkByDemoPortName(t, w, consumer0, "Network", selectNode, "In 0")
	linkByDemoPortName(t, w, consumer1, "Network", selectNode, "In 1")

	initial0 := requireConsumerNetwork(t, consumer0Node, "consumer0 initial")
	w.RemoveLink(consumer0Link)
	requireNetworkClosed(t, initial0, "consumer0 after detach")
	requireNoConsumerNetwork(t, consumer0Node, "consumer0 after detach")

	linkByDemoPortName(t, w, consumer0, "Network", selectNode, "In 0")
	reattached0 := requireConsumerNetwork(t, consumer0Node, "consumer0 after reattach")
	requireNetworkOpen(t, reattached0, "consumer0 after reattach")
	if reattached0 == initial0 {
		t.Fatal("consumer0 received the same network instance after reattach")
	}

	w.RemoveLink(providerLink)
	requireNetworkClosed(t, reattached0, "consumer0 after provider detach")
	requireNoConsumerNetwork(t, consumer1Node, "consumer1 while inactive")

	providerLink = linkByDemoPortName(t, w, selectNode, "Out", loopback, "Network")
	refreshed0 := requireConsumerNetwork(t, consumer0Node, "consumer0 after provider reattach")
	requireNetworkOpen(t, refreshed0, "consumer0 after provider reattach")
	if refreshed0 == reattached0 {
		t.Fatal("consumer0 received the same network instance after provider reattach")
	}

	w.RemoveLink(selectorLink)
	linkByDemoPortName(t, w, trueNode, "output", selectNode, "Selector")

	requireNetworkClosed(t, refreshed0, "consumer0 after switch to In 1")
	if consumer0Node.network != refreshed0 {
		t.Fatal("consumer0 network changed while switching away from In 0")
	}
	active1 := requireConsumerNetwork(t, consumer1Node, "consumer1 after switch")
	requireNetworkOpen(t, active1, "consumer1 after switch")

	w.RemoveLink(providerLink)
	requireNetworkClosed(t, active1, "consumer1 after second provider detach")
	linkByDemoPortName(t, w, selectNode, "Out", loopback, "Network")
	refreshed1 := requireConsumerNetwork(t, consumer1Node, "consumer1 after second provider reattach")
	requireNetworkOpen(t, refreshed1, "consumer1 after second provider reattach")
	if refreshed1 == active1 {
		t.Fatal("consumer1 received the same network instance after provider reattach")
	}
}

func newNetworkSelectWorkspace(t *testing.T) (*pasta.Workspace, *testNetworkConsumerClass) {
	t.Helper()
	w := pasta.NewWorkspace(testLogFactory{})
	for _, class := range stdClasses() {
		if err := w.AddNodeClass(class); err != nil {
			t.Fatalf("AddNodeClass %s: %v", class.ClassName(), err)
		}
	}
	consumerClass := &testNetworkConsumerClass{}
	if err := w.AddNodeClass(consumerClass); err != nil {
		t.Fatalf("AddNodeClass %s: %v", consumerClass.ClassName(), err)
	}
	return w, consumerClass
}

func addDemoByClass(t *testing.T, w *pasta.Workspace, class, name string) uint64 {
	t.Helper()
	id, err := w.AddNodeByClass(class, name)
	if err != nil {
		t.Fatalf("AddNodeByClass %s: %v", class, err)
	}
	return id
}

func linkByDemoPortName(t *testing.T, w *pasta.Workspace, rightNode uint64, rightName string, leftNode uint64, leftName string) uint64 {
	t.Helper()
	snapshot := w.Snapshot()
	right := demoPortByName(t, snapshot, rightNode, "right", rightName)
	left := demoPortByName(t, snapshot, leftNode, "left", leftName)
	link, _, err := w.AddLink(right, left)
	if err != nil {
		t.Fatalf("AddLink %s -> %s: %v", rightName, leftName, err)
	}
	return link
}

func linkIDByDemoPortNames(t *testing.T, w *pasta.Workspace, rightNode uint64, rightName string, leftNode uint64, leftName string) uint64 {
	t.Helper()
	snapshot := w.Snapshot()
	right := demoPortByName(t, snapshot, rightNode, "right", rightName)
	left := demoPortByName(t, snapshot, leftNode, "left", leftName)
	link, _, ok := w.LinkByPorts(right, left)
	if !ok {
		t.Fatalf("missing link %s -> %s", rightName, leftName)
	}
	return link
}

func demoPortByName(t *testing.T, snapshot pasta.WorkspaceSnapshot, node uint64, direction, name string) uint64 {
	t.Helper()
	for id, port := range snapshot.Ports {
		if port.Node == node && port.Direction == direction && port.Name == name {
			return id
		}
	}
	t.Fatalf("port %s %q on node %d not found", direction, name, node)
	return 0
}

func requireConsumerNetwork(t *testing.T, node *testNetworkConsumerNode, label string) networkCloser {
	t.Helper()
	if node == nil {
		t.Fatalf("%s: consumer node is nil", label)
	}
	if node.network == nil {
		t.Fatalf("%s: network is nil", label)
	}
	if node.events < 1 {
		t.Fatalf("%s: network event count = %d, want >= 1", label, node.events)
	}
	return node.network
}

func requireNoConsumerNetwork(t *testing.T, node *testNetworkConsumerNode, label string) {
	t.Helper()
	if node == nil {
		t.Fatalf("%s: consumer node is nil", label)
	}
	if node.network != nil {
		t.Fatalf("%s: network = %#v, want nil", label, node.network)
	}
}

func requireNetworkOpen(t *testing.T, network networkCloser, label string) {
	t.Helper()
	up, ok := network.(interface{ IsUp() (bool, error) })
	if !ok {
		t.Fatalf("%s: network does not expose IsUp", label)
	}
	isUp, err := up.IsUp()
	if err != nil {
		t.Fatalf("%s: IsUp error: %v", label, err)
	}
	if !isUp {
		t.Fatalf("%s: network is not up", label)
	}
}

func requireNetworkClosed(t *testing.T, network networkCloser, label string) {
	t.Helper()
	up, ok := network.(interface{ IsUp() (bool, error) })
	if !ok {
		t.Fatalf("%s: network does not expose IsUp", label)
	}
	isUp, err := up.IsUp()
	if err != nil {
		t.Fatalf("%s: IsUp error: %v", label, err)
	}
	if isUp {
		t.Fatalf("%s: network is still up", label)
	}
}
