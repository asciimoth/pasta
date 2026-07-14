package std

import (
	"errors"
	"reflect"
	"testing"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

type loopPair struct {
	Outer int
	Inner int
}

type loopProbeNode struct {
	pasta.BasicNode

	w  *pasta.Workspace
	id uint64

	bodyIn      uint64
	indexIn     uint64
	outerIn     uint64
	completedIn uint64
	continueOut uint64
	breakOut    uint64

	index int
	outer int

	indices     []int
	pairs       []loopPair
	bodies      int
	completions int

	noEnd            bool
	recordPairs      bool
	breakAt          *int
	breakAfterBodies int
	scheduleLoop     uint64
	scheduleOnBody   int
	scheduled        bool
	scheduleErr      error
}

func (n *loopProbeNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, _ *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	return nil
}

func (n *loopProbeNode) OnPortAdd(port uint64, _ string, _ []string) error {
	snapshot, ok := n.w.PortSnapshotLocked(port)
	if !ok {
		return nil
	}
	switch snapshot.Name {
	case "Body":
		n.bodyIn = port
	case "Index":
		n.indexIn = port
	case "Outer":
		n.outerIn = port
	case "Completed":
		n.completedIn = port
	case "Continue":
		n.continueOut = port
	case "Break":
		n.breakOut = port
	}
	return nil
}

func (n *loopProbeNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	switch port {
	case n.bodyIn, n.completedIn:
		if portDirection == "left" && linkType == TypeTrigger {
			return nil
		}
	case n.indexIn, n.outerIn:
		if portDirection == "left" && linkType == TypeInt {
			return nil
		}
	case n.continueOut, n.breakOut:
		if portDirection == "right" && linkType == TypeTrigger {
			return nil
		}
	}
	return pasta.LinkTypeErr(linkType)
}

func (n *loopProbeNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if receiverPortDirection != "left" {
		return nil
	}
	switch event.ReceiverPort {
	case n.indexIn:
		if linkType == TypeInt {
			if value, ok := parseIntAny(event.Payload); ok {
				n.index = value
			}
		}
	case n.outerIn:
		if linkType == TypeInt {
			if value, ok := parseIntAny(event.Payload); ok {
				n.outer = value
			}
		}
	case n.completedIn:
		if linkType == TypeTrigger && !IsRequest(event.Payload) {
			n.completions++
		}
	case n.bodyIn:
		if linkType == TypeTrigger && !IsRequest(event.Payload) {
			return n.onBody()
		}
	}
	return nil
}

func (n *loopProbeNode) onBody() error {
	n.bodies++
	if n.recordPairs {
		n.pairs = append(n.pairs, loopPair{Outer: n.outer, Inner: n.index})
	} else {
		n.indices = append(n.indices, n.index)
	}
	if n.scheduleLoop != 0 && !n.scheduled && n.bodies == n.scheduleOnBody {
		n.scheduled = true
		n.scheduleErr = n.w.TriggerLocked(n.scheduleLoop)
	}
	if n.noEnd {
		return nil
	}
	if n.breakAfterBodies > 0 && n.bodies >= n.breakAfterBodies {
		sendToPort(n.w, n.id, n.breakOut, Trigger{})
		return nil
	}
	if n.breakAt != nil && n.index == *n.breakAt {
		sendToPort(n.w, n.id, n.breakOut, Trigger{})
		return nil
	}
	sendToPort(n.w, n.id, n.continueOut, Trigger{})
	return nil
}

func TestForLoopContinuesUntilEnd(t *testing.T) {
	w := newStdWorkspace(t)
	loop, iter := addForLoopGraph(t, w, "for", 0, 3, 1)
	probe := &loopProbeNode{}
	probeID := addLoopProbeNode(t, w, "probe", probe)
	linkByPortName(t, w, loop, "Index", probeID, "Index")
	linkByPortName(t, w, loop, "Body", probeID, "Body")
	linkByPortName(t, w, probeID, "Continue", iter, "Continue")
	linkByPortName(t, w, loop, "Completed", probeID, "Completed")

	if err := w.Trigger(loop); err != nil {
		t.Fatalf("Trigger ForLoop: %v", err)
	}
	assertLoopProbeIndices(t, probe, []int{0, 1, 2})
	if probe.completions != 1 {
		t.Fatalf("completions = %d, want 1", probe.completions)
	}
	assertNodeLabel(t, w, loop, loopLabelWaiting)
	assertNodeLabel(t, w, iter, "")
}

func TestLoopNodePortOrder(t *testing.T) {
	w := newStdWorkspace(t)
	forLoop := addByClass(t, w, NodeTypeForLoop, "for")
	whileLoop := addByClass(t, w, NodeTypeWhileLoop, "while")
	iter := addByClass(t, w, NodeTypeIter, "iter")

	assertLeftPortNames(t, w, forLoop, []string{"Trigger", "Start index", "End index", "Step"})
	assertRightPortNames(t, w, forLoop, []string{"Loop", "Body", "Index", "Completed"})
	assertLeftPortNames(t, w, whileLoop, []string{"Trigger"})
	assertRightPortNames(t, w, whileLoop, []string{"Loop", "Body", "Completed"})
	assertLeftPortNames(t, w, iter, []string{"Loop", "Break", "Continue"})
	assertRightPortNames(t, w, iter, []string{})
}

func TestForLoopBreakCompletesImmediately(t *testing.T) {
	w := newStdWorkspace(t)
	loop, iter := addForLoopGraph(t, w, "for", 0, 5, 1)
	breakAt := 2
	probe := &loopProbeNode{breakAt: &breakAt}
	probeID := addLoopProbeNode(t, w, "probe", probe)
	linkByPortName(t, w, loop, "Index", probeID, "Index")
	linkByPortName(t, w, loop, "Body", probeID, "Body")
	linkByPortName(t, w, probeID, "Continue", iter, "Continue")
	linkByPortName(t, w, probeID, "Break", iter, "Break")
	linkByPortName(t, w, loop, "Completed", probeID, "Completed")

	if err := w.Trigger(loop); err != nil {
		t.Fatalf("Trigger ForLoop: %v", err)
	}
	assertLoopProbeIndices(t, probe, []int{0, 1, 2})
	if probe.completions != 1 {
		t.Fatalf("completions = %d, want 1", probe.completions)
	}
}

func TestForLoopSchedulesTriggerReceivedWhileLooping(t *testing.T) {
	w := newStdWorkspace(t)
	loop, iter := addForLoopGraph(t, w, "for", 0, 2, 1)
	probe := &loopProbeNode{
		scheduleLoop:   loop,
		scheduleOnBody: 1,
	}
	probeID := addLoopProbeNode(t, w, "probe", probe)
	linkByPortName(t, w, loop, "Index", probeID, "Index")
	linkByPortName(t, w, loop, "Body", probeID, "Body")
	linkByPortName(t, w, probeID, "Continue", iter, "Continue")
	linkByPortName(t, w, loop, "Completed", probeID, "Completed")

	if err := w.Trigger(loop); err != nil {
		t.Fatalf("Trigger ForLoop: %v", err)
	}
	if probe.scheduleErr != nil {
		t.Fatalf("scheduled TriggerLocked error: %v", probe.scheduleErr)
	}
	assertLoopProbeIndices(t, probe, []int{0, 1, 0, 1})
	if probe.completions != 2 {
		t.Fatalf("completions = %d, want 2", probe.completions)
	}
}

func TestWhileLoopRunsUntilBreak(t *testing.T) {
	w := newStdWorkspace(t)
	loop := addByClass(t, w, NodeTypeWhileLoop, "while")
	iter := addByClass(t, w, NodeTypeIter, "iter")
	linkByPortName(t, w, loop, "Loop", iter, "Loop")
	probe := &loopProbeNode{breakAfterBodies: 3}
	probeID := addLoopProbeNode(t, w, "probe", probe)
	linkByPortName(t, w, loop, "Body", probeID, "Body")
	linkByPortName(t, w, probeID, "Continue", iter, "Continue")
	linkByPortName(t, w, probeID, "Break", iter, "Break")
	linkByPortName(t, w, loop, "Completed", probeID, "Completed")

	if err := w.Trigger(loop); err != nil {
		t.Fatalf("Trigger WhileLoop: %v", err)
	}
	if probe.bodies != 3 {
		t.Fatalf("bodies = %d, want 3", probe.bodies)
	}
	if probe.completions != 1 {
		t.Fatalf("completions = %d, want 1", probe.completions)
	}
	assertNodeLabel(t, w, loop, loopLabelWaiting)
}

func TestLoopNodesRequireLoopLinkAndResetOnLinkChange(t *testing.T) {
	w := newStdWorkspace(t)
	loop := addForLoopWithoutIter(t, w, "for", 0, 2, 1)
	probe := &loopProbeNode{}
	probeID := addLoopProbeNode(t, w, "probe", probe)
	linkByPortName(t, w, loop, "Index", probeID, "Index")
	bodyLink := linkByPortName(t, w, loop, "Body", probeID, "Body")
	linkByPortName(t, w, loop, "Completed", probeID, "Completed")

	if err := w.Trigger(loop); err != nil {
		t.Fatalf("Trigger ForLoop without loop link: %v", err)
	}
	if probe.bodies != 0 || probe.completions != 0 {
		t.Fatalf("without loop link bodies=%d completions=%d, want 0/0", probe.bodies, probe.completions)
	}
	assertNodeLabel(t, w, loop, loopLabelWaiting)

	iter := addByClass(t, w, NodeTypeIter, "iter")
	linkByPortName(t, w, loop, "Loop", iter, "Loop")
	probe.noEnd = true
	if err := w.Trigger(loop); err != nil {
		t.Fatalf("Trigger blocking ForLoop: %v", err)
	}
	assertLoopProbeIndices(t, probe, []int{0})
	assertNodeLabel(t, w, loop, loopLabelLooping)

	w.RemoveLink(bodyLink)
	assertNodeLabel(t, w, loop, loopLabelWaiting)
	if probe.completions != 0 {
		t.Fatalf("completions after link removal = %d, want 0", probe.completions)
	}
}

func TestNestedForLoops(t *testing.T) {
	w := newStdWorkspace(t)
	outer, outerIter := addForLoopGraph(t, w, "outer", 0, 2, 1)
	inner, innerIter := addForLoopGraph(t, w, "inner", 0, 2, 1)
	probe := &loopProbeNode{recordPairs: true}
	probeID := addLoopProbeNode(t, w, "probe", probe)

	linkByPortName(t, w, outer, "Body", inner, "Trigger")
	linkByPortName(t, w, outer, "Index", probeID, "Outer")
	linkByPortName(t, w, inner, "Index", probeID, "Index")
	linkByPortName(t, w, inner, "Body", probeID, "Body")
	linkByPortName(t, w, probeID, "Continue", innerIter, "Continue")
	linkByPortName(t, w, inner, "Completed", outerIter, "Continue")
	linkByPortName(t, w, outer, "Completed", probeID, "Completed")

	if err := w.Trigger(outer); err != nil {
		t.Fatalf("Trigger outer ForLoop: %v", err)
	}
	want := []loopPair{{0, 0}, {0, 1}, {1, 0}, {1, 1}}
	if !reflect.DeepEqual(probe.pairs, want) {
		t.Fatalf("pairs = %#v, want %#v", probe.pairs, want)
	}
	if probe.completions != 1 {
		t.Fatalf("outer completions = %d, want 1", probe.completions)
	}
	assertNodeLabel(t, w, outer, loopLabelWaiting)
	assertNodeLabel(t, w, inner, loopLabelWaiting)
}

func TestLoopLinksAreReservedForLoopPorts(t *testing.T) {
	w := newStdWorkspace(t)
	loop := addByClass(t, w, NodeTypeForLoop, "for")
	compare := addByClass(t, w, NodeTypeEqual, "compare")
	snapshot := w.Snapshot()
	loopPort := portByName(t, snapshot, loop, "right", "Loop")
	compareInput := portByName(t, snapshot, compare, "left", "input 1")

	if _, _, err := w.AddLink(loopPort, compareInput); !errors.Is(err, pasta.ErrTypeCompat) {
		t.Fatalf("loop link to any/any compare input error = %v, want %v", err, pasta.ErrTypeCompat)
	}
}

func TestLoopStateIsEphemeralAcrossSaveRestoreAndPaste(t *testing.T) {
	w := newStdWorkspace(t)
	loop, _ := addForLoopGraph(t, w, "for", 0, 2, 1)
	probe := &loopProbeNode{noEnd: true}
	probeID := addLoopProbeNode(t, w, "probe", probe)
	linkByPortName(t, w, loop, "Index", probeID, "Index")
	linkByPortName(t, w, loop, "Body", probeID, "Body")
	if err := w.Trigger(loop); err != nil {
		t.Fatalf("Trigger ForLoop: %v", err)
	}
	assertNodeLabel(t, w, loop, loopLabelLooping)

	cfg := configer.NewMemory(nil)
	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	loopCfg := configObjectAt(t, cfg, "for")
	for key := range loopCfg {
		switch key {
		case "Class", "Links":
		default:
			t.Fatalf("saved loop config key %q = %#v, want only workspace topology", key, loopCfg[key])
		}
	}

	restored, err := pasta.WorkspaceFromConfig(allStdClasses(), cfg, testLogFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig: %v", err)
	}
	restoredLoop, ok := restored.NodeIDByName("for")
	if !ok {
		t.Fatal("restored loop missing")
	}
	assertNodeLabel(t, restored, restoredLoop, loopLabelWaiting)

	clip := w.Copy([]uint64{loop})
	pasted := w.Paste(clip)
	if len(pasted) != 1 {
		t.Fatalf("Paste returned %v, want one node", pasted)
	}
	assertNodeLabel(t, w, pasted[0], loopLabelWaiting)
}

func addForLoopGraph(t *testing.T, w *pasta.Workspace, name string, start, end, step int) (uint64, uint64) {
	t.Helper()
	loop := addForLoopWithoutIter(t, w, name, start, end, step)
	iter := addByClass(t, w, NodeTypeIter, name+" iter")
	linkByPortName(t, w, loop, "Loop", iter, "Loop")
	return loop, iter
}

func addForLoopWithoutIter(t *testing.T, w *pasta.Workspace, name string, start, end, step int) uint64 {
	t.Helper()
	loop := addByClass(t, w, NodeTypeForLoop, name)
	startNode := addIntConstant(t, w, name+" start", start)
	endNode := addIntConstant(t, w, name+" end", end)
	stepNode := addIntConstant(t, w, name+" step", step)
	linkByPortName(t, w, startNode, "output", loop, "Start index")
	linkByPortName(t, w, endNode, "output", loop, "End index")
	linkByPortName(t, w, stepNode, "output", loop, "Step")
	return loop
}

func addIntConstant(t *testing.T, w *pasta.Workspace, name string, value int) uint64 {
	t.Helper()
	node := addByClass(t, w, NodeTypeIntConstant, name)
	setConstant(t, w, node, value)
	return node
}

func addLoopProbeNode(t *testing.T, w *pasta.Workspace, name string, node *loopProbeNode) uint64 {
	t.Helper()
	id, err := w.AddNode(node, "example.com/LoopProbe", name)
	if err != nil {
		t.Fatalf("AddNode loop probe: %v", err)
	}
	ports := []pasta.Port{
		{Direction: "left", Name: "Body", Types: []string{TypeTrigger}},
		{Direction: "left", Name: "Index", Types: []string{TypeInt}},
		{Direction: "left", Name: "Outer", Types: []string{TypeInt}},
		{Direction: "left", Name: "Completed", Types: []string{TypeTrigger}},
		{Direction: "right", Name: "Continue", Types: []string{TypeTrigger}},
		{Direction: "right", Name: "Break", Types: []string{TypeTrigger}},
	}
	for _, port := range ports {
		port.Node = id
		if _, err := w.AddPort(port); err != nil {
			t.Fatalf("AddPort %s: %v", port.Name, err)
		}
	}
	return id
}

func assertLoopProbeIndices(t *testing.T, probe *loopProbeNode, want []int) {
	t.Helper()
	if !reflect.DeepEqual(probe.indices, want) {
		t.Fatalf("indices = %#v, want %#v", probe.indices, want)
	}
}

func configObjectAt(t *testing.T, cfg configer.Config, key string) map[string]any {
	t.Helper()
	raw, err := cfg.Get(configer.Path{key})
	if err != nil {
		t.Fatalf("Get(%s): %v", key, err)
	}
	object, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("Get(%s) has type %T, want map[string]any", key, raw)
	}
	return object
}
