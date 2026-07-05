package std

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

func TestTriggerNodeEmitsOnlyOnMenuButtonOrOnTrigger(t *testing.T) {
	w := newStdWorkspace(t)
	sinkClass := &triggerSinkClass{name: "example.com/TriggerSink"}
	if err := w.AddNodeClass(sinkClass); err != nil {
		t.Fatalf("AddNodeClass sink: %v", err)
	}

	source := addByClass(t, w, NodeTypeTrigger, "trigger")
	sink := addByClass(t, w, sinkClass.ClassName(), "sink")
	linkByPortName(t, w, source, "Trigger", sink, "Trigger")
	if got := sinkClass.node.count; got != 0 {
		t.Fatalf("trigger count after link add = %d, want 0", got)
	}

	if err := w.Trigger(source); err != nil {
		t.Fatalf("Trigger source: %v", err)
	}
	if got := sinkClass.node.count; got != 1 {
		t.Fatalf("trigger count after OnTrigger = %d, want 1", got)
	}

	w.SendNodeFormularMsg(source, formular.ButtonPressMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageButtonPress, MenuID: pasta.NodeMenuID(source), MenuGeneration: 1},
		BlockID:     "state",
		ButtonID:    "trigger",
	})
	if got := sinkClass.node.count; got != 2 {
		t.Fatalf("trigger count after menu button = %d, want 2", got)
	}
}

func TestPopUpNodeUsesLatestInputsAndDefaultLevels(t *testing.T) {
	w := newStdWorkspace(t)
	trigger := addByClass(t, w, NodeTypeTrigger, "trigger")
	level := addByClass(t, w, NodeTypeStringConstant, "level")
	text := addByClass(t, w, NodeTypeStringConstant, "text")
	popup := addByClass(t, w, NodeTypePopUp, "popup")
	setConstant(t, w, text, "ready")
	linkByPortName(t, w, trigger, "Trigger", popup, "Trigger")
	linkByPortName(t, w, level, "output", popup, "Lvl")
	linkByPortName(t, w, text, "output", popup, "Text")

	if err := w.Trigger(trigger); err != nil {
		t.Fatalf("Trigger source: %v", err)
	}
	assertPopupList(t, w, popup, []popupSpec{{typ: pasta.NodePopupInfo, text: "ready"}})

	setConstant(t, w, level, pasta.NodePopupWard)
	setConstant(t, w, text, "careful")
	if err := w.Trigger(trigger); err != nil {
		t.Fatalf("Trigger warning: %v", err)
	}
	assertPopupList(t, w, popup, []popupSpec{
		{typ: pasta.NodePopupInfo, text: "ready"},
		{typ: pasta.NodePopupWard, text: "careful"},
	})

	setConstant(t, w, level, "bad-level")
	setConstant(t, w, text, "broken")
	if err := w.Trigger(popup); err != nil {
		t.Fatalf("Trigger popup node: %v", err)
	}
	assertPopupList(t, w, popup, []popupSpec{
		{typ: pasta.NodePopupInfo, text: "ready"},
		{typ: pasta.NodePopupWard, text: "careful"},
		{typ: pasta.NodePopupErr, text: "broken"},
	})
}

func TestSelectTriggerRoutingIgnoresInactivePathsDoesNotReplayOrRequest(t *testing.T) {
	w := newStdWorkspace(t)
	source0Class := &triggerSourceClass{name: "example.com/TriggerSource0"}
	source1Class := &triggerSourceClass{name: "example.com/TriggerSource1"}
	sinkClass := &triggerSinkClass{name: "example.com/SelectTriggerSink"}
	for _, class := range []pasta.NodeClass{source0Class, source1Class, sinkClass} {
		if err := w.AddNodeClass(class); err != nil {
			t.Fatalf("AddNodeClass %s: %v", class.ClassName(), err)
		}
	}

	source0 := addByClass(t, w, source0Class.ClassName(), "source0")
	source1 := addByClass(t, w, source1Class.ClassName(), "source1")
	selector := addByClass(t, w, NodeTypeSelect, "select")
	boolTrue := addByClass(t, w, NodeTypeTrueConstant, "true")
	sink := addByClass(t, w, sinkClass.ClassName(), "sink")

	linkByPortName(t, w, source0, "Trigger", selector, "In 0")
	linkByPortName(t, w, source1, "Trigger", selector, "In 1")
	outLink := linkByPortName(t, w, selector, "Out", sink, "Trigger")
	if source0Class.node.requests != 0 || source1Class.node.requests != 0 {
		t.Fatalf("trigger sources received RequestValue on link add: source0=%d source1=%d", source0Class.node.requests, source1Class.node.requests)
	}

	if err := w.Trigger(source1); err != nil {
		t.Fatalf("Trigger inactive source1: %v", err)
	}
	if got := sinkClass.node.count; got != 0 {
		t.Fatalf("sink count after inactive trigger = %d, want 0", got)
	}

	if err := w.Trigger(source0); err != nil {
		t.Fatalf("Trigger active source0: %v", err)
	}
	if got := sinkClass.node.count; got != 1 {
		t.Fatalf("sink count after active trigger = %d, want 1", got)
	}

	w.RemoveLink(outLink)
	if err := w.Trigger(source0); err != nil {
		t.Fatalf("Trigger active source0 without output: %v", err)
	}
	if got := sinkClass.node.count; got != 1 {
		t.Fatalf("sink count after disconnected active trigger = %d, want 1", got)
	}
	linkByPortName(t, w, selector, "Out", sink, "Trigger")
	if got := sinkClass.node.count; got != 1 {
		t.Fatalf("sink count after reconnect = %d, want no replay", got)
	}

	linkByPortName(t, w, boolTrue, "output", selector, "Selector")
	if source0Class.node.requests != 0 || source1Class.node.requests != 0 {
		t.Fatalf("trigger sources received RequestValue on selector switch: source0=%d source1=%d", source0Class.node.requests, source1Class.node.requests)
	}
	if err := w.Trigger(source0); err != nil {
		t.Fatalf("Trigger inactive source0 after switch: %v", err)
	}
	if err := w.Trigger(source1); err != nil {
		t.Fatalf("Trigger active source1 after switch: %v", err)
	}
	if got := sinkClass.node.count; got != 2 {
		t.Fatalf("sink count after switched triggers = %d, want 2", got)
	}
}

func TestGatewayStoresDataUntilTriggerPassesRequestsAndDrainsClosables(t *testing.T) {
	w := newStdWorkspace(t)
	sourceClass := &customValueClass{name: "example.com/GatewaySource", value: "zero"}
	sinkClass := &customSinkClass{}
	if err := w.AddNodeClass(sourceClass); err != nil {
		t.Fatalf("AddNodeClass source: %v", err)
	}
	if err := w.AddNodeClass(sinkClass); err != nil {
		t.Fatalf("AddNodeClass sink: %v", err)
	}

	source := addByClass(t, w, sourceClass.ClassName(), "source")
	gateway := addByClass(t, w, NodeTypeGateway, "gateway")
	trigger := addByClass(t, w, NodeTypeTrigger, "trigger")
	sink := addByClass(t, w, sinkClass.ClassName(), "sink")

	inLink := linkByPortName(t, w, source, "output", gateway, "In")
	outLink := linkByPortName(t, w, gateway, "Out", sink, "input")
	linkByPortName(t, w, trigger, "Trigger", gateway, "Trigger")
	if got := sinkClass.node.value; got != "" {
		t.Fatalf("sink value before trigger = %q, want empty", got)
	}

	w.EmitEvent(sink, outLink, RequestValue{})
	if got := sourceClass.node.requests; got != 1 {
		t.Fatalf("source requests after gateway RequestValue = %d, want 1", got)
	}
	if got := sinkClass.node.value; got != "" {
		t.Fatalf("sink value after request before trigger = %q, want empty", got)
	}

	if err := w.Trigger(trigger); err != nil {
		t.Fatalf("Trigger gateway: %v", err)
	}
	if got := sinkClass.node.value; got != "zero" {
		t.Fatalf("sink value after trigger = %q, want zero", got)
	}

	closable1 := newTestClosablePayload()
	closable2 := newTestClosablePayload()
	w.EmitEvent(source, inLink, closable1)
	w.EmitEvent(source, inLink, closable2)
	if !closable1.wait(time.Second) {
		t.Fatal("replacing stored gateway payload did not close previous ClosablePayload")
	}
	if closable2.closed() {
		t.Fatal("latest stored gateway payload closed before drain")
	}
	w.RemoveLink(inLink)
	if !closable2.wait(time.Second) {
		t.Fatal("disconnecting gateway input did not drain latest ClosablePayload")
	}
}

func TestGatewayTriggersStoredIntIntoSumThroughConcreteOutputLink(t *testing.T) {
	w := newStdWorkspace(t)
	a := addByClass(t, w, NodeTypeIntConstant, "a")
	b := addByClass(t, w, NodeTypeIntConstant, "b")
	trigger := addByClass(t, w, NodeTypeTrigger, "trigger")
	gateway := addByClass(t, w, NodeTypeGateway, "gateway")
	sum := addByClass(t, w, NodeTypeSum, "sum")
	setConstant(t, w, a, 1)
	setConstant(t, w, b, 1)

	linkByPortName(t, w, a, "output", sum, "input 1")
	linkByPortName(t, w, trigger, "Trigger", gateway, "Trigger")
	linkByPortName(t, w, b, "output", gateway, "In")
	linkByPortName(t, w, gateway, "Out", sum, "input 2")

	snapshot := w.Snapshot()
	if got := snapshot.Nodes[sum].Label; got != "1" {
		t.Fatalf("sum before trigger = %q, want 1", got)
	}
	out := portByName(t, snapshot, gateway, "right", "Out")
	if got := snapshot.Ports[out].Types; !reflect.DeepEqual(got, []string{TypeInt}) {
		t.Fatalf("gateway Out types = %#v, want [%s]", got, TypeInt)
	}
	link := findLinkByPorts(t, snapshot, out, portByName(t, snapshot, sum, "left", "input 2"))
	if got := snapshot.Links[link].Type; got != TypeInt {
		t.Fatalf("gateway output link type = %q, want %q", got, TypeInt)
	}

	if err := w.Trigger(gateway); err != nil {
		t.Fatalf("Trigger gateway directly: %v", err)
	}
	if got := w.Snapshot().Nodes[sum].Label; got != "2" {
		t.Fatalf("sum after direct gateway trigger = %q, want 2", got)
	}

	setConstant(t, w, b, 2)
	if got := w.Snapshot().Nodes[sum].Label; got != "2" {
		t.Fatalf("sum after updating gated value before linked trigger = %q, want 2", got)
	}
	if err := w.Trigger(trigger); err != nil {
		t.Fatalf("Trigger gateway through trigger link: %v", err)
	}
	if got := w.Snapshot().Nodes[sum].Label; got != "3" {
		t.Fatalf("sum after linked trigger = %q, want 3", got)
	}
}

func TestComplexTriggerGatewayPopupTopologyWithStringNodes(t *testing.T) {
	w := newStdWorkspace(t)
	trigger := addByClass(t, w, NodeTypeTrigger, "trigger")
	text := addByClass(t, w, NodeTypeStringConstant, "text")
	level := addByClass(t, w, NodeTypeStringConstant, "level")
	gateway := addByClass(t, w, NodeTypeGateway, "gateway")
	trim := addByClass(t, w, NodeTypeStringTrimSpace, "trim")
	upper := addByClass(t, w, NodeTypeStringUpper, "upper")
	popup := addByClass(t, w, NodeTypePopUp, "popup")

	setConstant(t, w, text, " hello ")
	setConstant(t, w, level, "")
	linkByPortName(t, w, text, "output", gateway, "In")
	linkByPortName(t, w, trigger, "Trigger", gateway, "Trigger")
	linkByPortName(t, w, gateway, "Out", trim, "input 1")
	linkByPortName(t, w, trim, "output", upper, "input 1")
	linkByPortName(t, w, upper, "output", popup, "Text")
	linkByPortName(t, w, level, "output", popup, "Lvl")

	if got := w.Snapshot().Nodes[upper].Label; got != "" {
		t.Fatalf("upper label before gateway trigger = %q, want empty", got)
	}
	if err := w.Trigger(trigger); err != nil {
		t.Fatalf("Trigger gateway source: %v", err)
	}
	if got := w.Snapshot().Nodes[upper].Label; got != "HELLO" {
		t.Fatalf("upper label after gateway trigger = %q, want HELLO", got)
	}
	if err := w.Trigger(popup); err != nil {
		t.Fatalf("Trigger popup: %v", err)
	}
	assertPopupList(t, w, popup, []popupSpec{{typ: pasta.NodePopupInfo, text: "HELLO"}})
}

type triggerSourceClass struct {
	name string
	node *triggerSourceNode
}

func (c *triggerSourceClass) ClassName() string        { return c.name }
func (c *triggerSourceClass) ShortDescription() string { return "trigger source" }
func (c *triggerSourceClass) LongDescription() string  { return "trigger source" }
func (c *triggerSourceClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeTrigger, InitialPorts: []pasta.Port{{Direction: "right", Name: "Trigger", Types: []string{TypeTrigger}}}}
}
func (c *triggerSourceClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	c.node = &triggerSourceNode{}
	return c.node, nil
}

type triggerSourceNode struct {
	pasta.BasicNode
	w        *pasta.Workspace
	id       uint64
	out      uint64
	requests int
}

func (n *triggerSourceNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil && len(restored.RightPorts) > 0 {
		n.out = restored.RightPorts[0]
	}
	return nil
}

func (n *triggerSourceNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if port != n.out || portDirection != "right" || linkType != TypeTrigger {
		return pasta.LinkTypeErr(linkType)
	}
	return nil
}

func (n *triggerSourceNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if event.ReceiverPort == n.out && receiverPortDirection == "right" && linkType == TypeTrigger && IsRequest(event.Payload) {
		n.requests++
	}
	return nil
}

func (n *triggerSourceNode) OnTrigger() error {
	snapshot, ok := n.w.PortSnapshotLocked(n.out)
	if !ok {
		return nil
	}
	for _, link := range snapshot.Links {
		linkSnapshot, ok := n.w.LinkSnapshotLocked(link)
		if !ok {
			continue
		}
		receiverNode, receiverPort := otherEndpoint(linkSnapshot, n.out)
		n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: Trigger{}})
	}
	return nil
}

type triggerSinkClass struct {
	name string
	node *triggerSinkNode
}

func (c *triggerSinkClass) ClassName() string        { return c.name }
func (c *triggerSinkClass) ShortDescription() string { return "trigger sink" }
func (c *triggerSinkClass) LongDescription() string  { return "trigger sink" }
func (c *triggerSinkClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{InitialPorts: []pasta.Port{{Direction: "left", Name: "Trigger", Types: []string{TypeTrigger}}}}
}
func (c *triggerSinkClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	c.node = &triggerSinkNode{}
	return c.node, nil
}

type triggerSinkNode struct {
	pasta.BasicNode
	count int
}

func (n *triggerSinkNode) PreLinkAdd(_ uint64, linkType, portDirection string) error {
	if portDirection != "left" || linkType != TypeTrigger {
		return pasta.LinkTypeErr(linkType)
	}
	return nil
}

func (n *triggerSinkNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if receiverPortDirection == "left" && linkType == TypeTrigger && !IsRequest(event.Payload) {
		n.count++
	}
	return nil
}

type testClosablePayload struct {
	once     sync.Once
	closedCh chan struct{}
}

func newTestClosablePayload() *testClosablePayload {
	return &testClosablePayload{closedCh: make(chan struct{})}
}

func (p *testClosablePayload) Close() error {
	called := false
	p.once.Do(func() {
		called = true
		close(p.closedCh)
	})
	if !called {
		return errors.New("already closed")
	}
	return nil
}

func (p *testClosablePayload) closed() bool {
	select {
	case <-p.closedCh:
		return true
	default:
		return false
	}
}

func (p *testClosablePayload) wait(timeout time.Duration) bool {
	select {
	case <-p.closedCh:
		return true
	case <-time.After(timeout):
		return false
	}
}

type popupSpec struct {
	typ  string
	text string
}

func assertPopupList(t *testing.T, w *pasta.Workspace, node uint64, want []popupSpec) {
	t.Helper()
	gotPopups := w.Snapshot().Nodes[node].Popups
	got := make([]popupSpec, 0, len(gotPopups))
	for _, popup := range gotPopups {
		got = append(got, popupSpec{typ: popup.Type, text: popup.Text})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("popups = %#v, want %#v", got, want)
	}
}

func findLinkByPorts(t *testing.T, snapshot pasta.WorkspaceSnapshot, portA, portB uint64) uint64 {
	t.Helper()
	for id, link := range snapshot.Links {
		if (link.LeftPort == portA && link.RightPort == portB) || (link.LeftPort == portB && link.RightPort == portA) {
			return id
		}
	}
	t.Fatalf("missing link between ports %d and %d", portA, portB)
	return 0
}
