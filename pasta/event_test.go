package pasta_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

const calcType = "example.com/number"

type calcNode struct {
	kind   string
	value  float64
	inputs map[uint64]float64

	w  *pasta.Workspace
	l  pasta.Logger
	id uint64

	leftA uint64
	leftB uint64
	leftC uint64
	leftD uint64
	right uint64
}

func (n *calcNode) OnInit(w *pasta.Workspace, l pasta.Logger, id uint64, class string, restored *pasta.NodeInitData) error {
	n.w = w
	n.l = l
	n.id = id
	n.log("init")
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot()
	return nil
}

func (n *calcNode) OnReady() error {
	n.log("ready")
	return nil
}

func (n *calcNode) OnRootStatus(hasRootPath bool) error {
	return nil
}

func (n *calcNode) OnStop() {
	n.log("stop")
}

func (n *calcNode) OnPortAdd(port uint64, direction string, types []string) error {
	n.log("port add port=%d direction=%s types=%v", port, direction, types)
	return nil
}

func (n *calcNode) OnPortRemoved(port uint64, direction string) error {
	n.log("port removed port=%d direction=%s", port, direction)
	return nil
}

func (n *calcNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	n.log("pre link add port=%d type=%s direction=%s", port, linkType, portDirection)
	return nil
}

func (n *calcNode) OnLinkAdd(link, port uint64, linkType, portDirection string) error {
	n.log("link add link=%d port=%d type=%s direction=%s", link, port, linkType, portDirection)
	if port == n.right {
		n.sendToLink(link)
	}
	return nil
}

func (n *calcNode) OnLinkRemoved(link, port uint64, linkType, portDirection string) error {
	n.log("link removed link=%d port=%d type=%s direction=%s", link, port, linkType, portDirection)
	if n.isMiddleware() && portDirection == "left" {
		n.setInput(port, 0)
		n.recalcAndBroadcast()
	}
	return nil
}

func (n *calcNode) OnEvent(event pasta.Event, linkType string, receiverPortTypes []string, receiverPortDirection string) error {
	value, ok := event.Payload.(float64)
	if !ok {
		return fmt.Errorf("unexpected payload %T", event.Payload)
	}
	n.log(
		"event sender=%d:%d receiver=%d:%d type=%s receiver_types=%v receiver_direction=%s payload=%g",
		event.SenderNode,
		event.SenderPort,
		event.ReceiverNode,
		event.ReceiverPort,
		linkType,
		receiverPortTypes,
		receiverPortDirection,
		value,
	)

	if n.kind == "result" {
		n.value = value
		n.log("result state=%g", n.value)
		if err := n.updateLabel(); err != nil {
			return err
		}
		n.sendMenuBlock()
		return nil
	}

	if n.isMiddleware() {
		n.setInput(event.ReceiverPort, value)
		n.recalcAndBroadcast()
	}
	return nil
}

func (n *calcNode) OnInbox(message pasta.InboxMessage) error {
	n.log("inbox receiver=%d payload=%v", message.ReceiverNode, message.Payload)
	return nil
}

func (n *calcNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.FieldUpdateMessage)
	if !ok || n.kind != "constant" || msg.MenuID != pasta.NodeMenuID(n.id) {
		return nil
	}
	if msg.Field.BlockID != "state" || msg.Field.FieldID != "value" {
		return nil
	}
	value, ok := formularFloat(msg.Value)
	if !ok {
		return nil
	}
	n.log("formular value=%g", value)
	n.value = value
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuBlock()
	n.sendAll()
	return nil
}

func (n *calcNode) recalcAndBroadcast() {
	old := n.value
	n.value = n.recalc()
	n.log("%s state=%g", n.kind, n.value)
	if err := n.updateLabel(); err != nil {
		n.log("label update failed: %v", err)
	}
	n.sendMenuBlock()
	if n.value != old {
		n.sendAll()
	}
}

func (n *calcNode) recalc() float64 {
	switch n.kind {
	case "sum":
		return n.input(n.leftA) + n.input(n.leftB) + n.input(n.leftC) + n.input(n.leftD)
	case "sub":
		return n.input(n.leftA) - n.input(n.leftB)
	case "div":
		a := n.input(n.leftA)
		b := n.input(n.leftB)
		if b == 0 {
			return 0
		}
		return a / b
	case "mult":
		return n.input(n.leftA) * n.input(n.leftB)
	default:
		return n.value
	}
}

func (n *calcNode) isMiddleware() bool {
	return n.kind == "sum" || n.kind == "sub" || n.kind == "div" || n.kind == "mult"
}

func (n *calcNode) sendAll() {
	if n.right == 0 {
		return
	}
	port, ok := n.w.PortSnapshot(n.right)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendToLink(link)
	}
}

func (n *calcNode) sendToLink(link uint64) {
	snapshot, ok := n.link(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.right)
	n.log("send link=%d payload=%g receiver=%d:%d", link, n.value, receiverNode, receiverPort)
	n.w.SendEvent(pasta.Event{
		SenderNode:   n.id,
		SenderPort:   n.right,
		ReceiverNode: receiverNode,
		ReceiverPort: receiverPort,
		Payload:      n.value,
	})
}

func (n *calcNode) link(link uint64) (pasta.LinkSnapshot, bool) {
	return n.w.LinkSnapshot(link)
}

func (n *calcNode) log(format string, args ...any) {
	n.l.Debugf("%s "+format, append([]any{n.kind}, args...)...)
}

func (n *calcNode) input(port uint64) float64 {
	if port == 0 {
		return 0
	}
	return n.inputs[port]
}

func (n *calcNode) setInput(port uint64, value float64) {
	if port != 0 {
		n.inputs[port] = value
	}
}

func (n *calcNode) updateLabel() error {
	if n.w == nil || n.id == 0 {
		return nil
	}
	return n.w.SetNodeLabel(n.id, fmt.Sprintf("%g", n.value))
}

func (n *calcNode) sendMenuSnapshot() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsg(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{
			Type:           formular.MessageMenuSnapshot,
			MenuID:         pasta.NodeMenuID(n.id),
			MenuGeneration: 1,
		},
		Blocks: []formular.Block{n.menuBlock()},
	})
}

func (n *calcNode) sendMenuBlock() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsg(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{
			Type:            formular.MessageBlockSnapshot,
			MenuID:          pasta.NodeMenuID(n.id),
			MenuGeneration:  1,
			BlockGeneration: 1,
		},
		Block: n.menuBlock(),
	})
}

func (n *calcNode) menuBlock() formular.Block {
	return formular.Block{
		ID:         "state",
		Order:      10,
		Generation: 1,
		Items: []formular.Item{
			{
				Type:  formular.ItemField,
				ID:    "value",
				Label: "Value",
				Field: &formular.Field{
					Kind:     formular.FieldFloat,
					Value:    n.value,
					Readonly: n.kind != "constant",
				},
			},
		},
	}
}

func formularFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

type calcGraph struct {
	w     *pasta.Workspace
	nodes map[string]*calcNode
	ports map[string]uint64
	links map[string]uint64
}

func TestWorkspaceEventsCalculatorTopology(t *testing.T) {
	/*
		Data flow, using events sent right -> left:

		  c10.right ----> sum.a
		  c5.right -----> sum.b
		      \-----------------------------------------------> mult.b
		  c2.right -----> sum.c
		      \---------------------> div.b
		  c3.right -----> sum.d
		  c4.right -----> sub.b

		  sum.right ----> sub.a ----> sub.right ----> div.a ----> div.right ----> mult.a ----> mult.right ----> final.left
		             \                         \                         \
		              -> sumResult.left         -> subResult.left          -> divResult.left

		Sum has four inputs. The c5, c2, sum/sub/div right ports fan out to
		more than one link.
	*/
	first, firstLogs := buildCalcGraph(t, []string{
		"c10:sum.a", "c5:sum.b", "c2:sum.c", "c3:sum.d",
		"sum:sub.a", "c4:sub.b", "sub:div.a", "c2:div.b",
		"div:mult.a", "c5:mult.b", "sum:sumResult.in", "sub:subResult.in",
		"div:divResult.in", "mult:final.in",
	})
	second, secondLogs := buildCalcGraph(t, []string{
		"mult:final.in", "div:divResult.in", "sub:subResult.in", "sum:sumResult.in",
		"div:mult.a", "sub:div.a", "sum:sub.a", "c2:div.b",
		"c5:mult.b", "c4:sub.b", "c3:sum.d", "c2:sum.c", "c5:sum.b", "c10:sum.a",
	})

	wantStates := map[string]float64{
		"c10":       10,
		"c5":        5,
		"c2":        2,
		"c3":        3,
		"c4":        4,
		"sum":       20,
		"sub":       16,
		"div":       8,
		"mult":      40,
		"sumResult": 20,
		"subResult": 16,
		"divResult": 8,
		"final":     40,
	}

	if got := first.states(); !reflect.DeepEqual(got, wantStates) {
		t.Fatalf("first states = %#v, want %#v", got, wantStates)
	}
	if got := second.states(); !reflect.DeepEqual(got, wantStates) {
		t.Fatalf("second states = %#v, want %#v", got, wantStates)
	}
	wantLabels := calcLabels(wantStates)
	if got := first.labels(); !reflect.DeepEqual(got, wantLabels) {
		t.Fatalf("first labels = %#v, want %#v", got, wantLabels)
	}
	if got := second.labels(); !reflect.DeepEqual(got, wantLabels) {
		t.Fatalf("second labels = %#v, want %#v", got, wantLabels)
	}
	if got, want := first.topology(), second.topology(); !reflect.DeepEqual(got, want) {
		t.Fatalf("topologies differ\ngot:  %#v\nwant: %#v", got, want)
	}
	if firstLogs == secondLogs {
		t.Fatal("different link histories produced identical logs")
	}

	first.w.RemoveLink(first.links["c2.out>div.b"])
	wantStates["div"] = 0
	wantStates["mult"] = 0
	wantStates["divResult"] = 0
	wantStates["final"] = 0
	if got := first.states(); !reflect.DeepEqual(got, wantStates) {
		t.Fatalf("states after input disconnect = %#v, want %#v", got, wantStates)
	}
	if got, want := first.labels(), calcLabels(wantStates); !reflect.DeepEqual(got, want) {
		t.Fatalf("labels after input disconnect = %#v, want %#v", got, want)
	}
}

func TestWorkspaceDeliversEventsBothDirections(t *testing.T) {
	logf := &StringLoggerFactory{}
	w := pasta.NewWorkspace(logf)
	leftNode := &workspaceNode{}
	rightNode := &workspaceNode{}
	leftNodeID, _ := w.AddNode(leftNode, "example.com/LeftNode")
	rightNodeID, _ := w.AddNode(rightNode, "example.com/RightNode")
	leftPort := mustAddPort(t, w, leftNodeID, "left", calcType)
	rightPort := mustAddPort(t, w, rightNodeID, "right", calcType)
	if _, _, err := w.AddLink(leftPort, rightPort); err != nil {
		t.Fatalf("AddLink: %v", err)
	}

	w.SendEvent(pasta.Event{
		SenderNode:   leftNodeID,
		SenderPort:   leftPort,
		ReceiverNode: rightNodeID,
		ReceiverPort: rightPort,
		Payload:      float64(7),
	})
	w.SendEvent(pasta.Event{
		SenderNode:   rightNodeID,
		SenderPort:   rightPort,
		ReceiverNode: leftNodeID,
		ReceiverPort: leftPort,
		Payload:      float64(9),
	})

	got := logf.Result()
	if !strings.Contains(got, "receiver_direction=right payload=7") {
		t.Fatalf("left->right event was not delivered with right receiver metadata:\n%s", got)
	}
	if !strings.Contains(got, "receiver_direction=left payload=9") {
		t.Fatalf("right->left event was not delivered with left receiver metadata:\n%s", got)
	}
}

func TestWorkspaceDropsEventWhenLinkChangesBeforeDelivery(t *testing.T) {
	logf := &StringLoggerFactory{}
	w := pasta.NewWorkspace(logf)
	sender := &workspaceNode{}
	receiver := &workspaceNode{}
	senderID, _ := w.AddNode(sender, "example.com/Sender")
	receiverID, _ := w.AddNode(receiver, "example.com/Receiver")
	senderPort := mustAddPort(t, w, senderID, "left", calcType)
	receiverPort := mustAddPort(t, w, receiverID, "right", calcType)
	link, _, err := w.AddLink(senderPort, receiverPort)
	if err != nil {
		t.Fatalf("AddLink: %v", err)
	}

	w.Lock()
	w.SendEvent(pasta.Event{
		SenderNode:   senderID,
		SenderPort:   senderPort,
		ReceiverNode: receiverID,
		ReceiverPort: receiverPort,
		Payload:      float64(1),
	})
	w.RemoveLink(link)
	w.Unlock()

	if got := logf.Result(); strings.Contains(got, "payload=1") {
		t.Fatalf("event was delivered after link removal:\n%s", got)
	}
}

func buildCalcGraph(t *testing.T, linkOrder []string) (*calcGraph, string) {
	t.Helper()

	logf := &StringLoggerFactory{}
	w := pasta.NewWorkspace(logf)
	graph := &calcGraph{
		w:     w,
		nodes: make(map[string]*calcNode),
		ports: make(map[string]uint64),
		links: make(map[string]uint64),
	}

	for _, spec := range []struct {
		name  string
		kind  string
		value float64
	}{
		{"c10", "constant", 10},
		{"c5", "constant", 5},
		{"c2", "constant", 2},
		{"c3", "constant", 3},
		{"c4", "constant", 4},
		{"sum", "sum", 0},
		{"sub", "sub", 0},
		{"mult", "mult", 0},
		{"div", "div", 0},
		{"sumResult", "result", 0},
		{"subResult", "result", 0},
		{"divResult", "result", 0},
		{"final", "result", 0},
	} {
		node := &calcNode{kind: spec.kind, value: spec.value, inputs: make(map[uint64]float64)}
		_, err := w.AddNode(node, "example.com/Calculator")
		if err != nil {
			t.Fatalf("AddNode %s: %v", spec.name, err)
		}
		graph.nodes[spec.name] = node
	}

	addRight := func(name string) {
		node := graph.nodes[name]
		port := mustAddPort(t, w, node.id, "right", calcType)
		node.right = port
		graph.ports[name+".out"] = port
	}
	addLeft := func(name, suffix string) uint64 {
		node := graph.nodes[name]
		port := mustAddPort(t, w, node.id, "left", calcType)
		graph.ports[name+"."+suffix] = port
		return port
	}

	for _, name := range []string{"c10", "c5", "c2", "c3", "c4", "sum", "sub", "mult", "div"} {
		addRight(name)
	}
	for _, name := range []string{"sub", "mult", "div"} {
		node := graph.nodes[name]
		node.leftA = addLeft(name, "a")
		node.leftB = addLeft(name, "b")
	}
	sum := graph.nodes["sum"]
	sum.leftA = addLeft("sum", "a")
	sum.leftB = addLeft("sum", "b")
	sum.leftC = addLeft("sum", "c")
	sum.leftD = addLeft("sum", "d")
	for _, name := range []string{"sumResult", "subResult", "divResult", "final"} {
		node := graph.nodes[name]
		node.leftA = addLeft(name, "in")
	}

	for _, spec := range linkOrder {
		from, to := splitLinkSpec(spec)
		link, _, err := w.AddLink(graph.ports[from+".out"], graph.ports[to])
		if err != nil {
			t.Fatalf("AddLink %s: %v", spec, err)
		}
		graph.links[fmt.Sprintf("%s>%s", from+".out", to)] = link
	}

	return graph, logf.Result()
}

func (g *calcGraph) states() map[string]float64 {
	states := make(map[string]float64, len(g.nodes))
	for name, node := range g.nodes {
		states[name] = node.value
	}
	return states
}

func (g *calcGraph) labels() map[string]string {
	snapshot := g.w.Snapshot()
	labels := make(map[string]string, len(g.nodes))
	for name, node := range g.nodes {
		labels[name] = snapshot.Nodes[node.id].Label
	}
	return labels
}

func calcLabels(states map[string]float64) map[string]string {
	labels := make(map[string]string, len(states))
	for name, value := range states {
		labels[name] = fmt.Sprintf("%g", value)
	}
	return labels
}

func (g *calcGraph) topology() []string {
	links := make([]string, 0, len(g.links))
	for link := range g.links {
		links = append(links, link)
	}
	slices.Sort(links)
	return links
}

func splitLinkSpec(spec string) (string, string) {
	for i := 0; i < len(spec); i++ {
		if spec[i] == ':' {
			return spec[:i], spec[i+1:]
		}
	}
	return spec, ""
}

func otherEndpoint(link pasta.LinkSnapshot, port uint64) (uint64, uint64) {
	if link.LeftPort == port {
		return link.RightPortNode, link.RightPort
	}
	return link.LeftPortNode, link.LeftPort
}
