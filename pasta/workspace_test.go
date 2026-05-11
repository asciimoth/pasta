package pasta

import (
	"errors"
	"fmt"
	"testing"
)

const testType = "example.com/int"

func testWorkspace(t *testing.T) (*Workspace, ClassSpec) {
	t.Helper()
	class := ClassSpec{
		Name: "example.com/Source",
		Inputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: InputPort},
			Name:      "in",
			Direction: InputPort,
			FixedType: testType,
		}},
		Outputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: OutputPort},
			Name:      "out",
			Direction: OutputPort,
			FixedType: testType,
		}},
	}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{class}}); err != nil {
		t.Fatal(err)
	}
	return w, class
}

func TestNameValidation(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
	}{
		{"example.com/Thing", true},
		{"example.com/thing", false},
		{"other.com/Thing", false},
		{"example.com/Thing_1", false},
	}
	for _, tt := range tests {
		if got := ValidClassName("example.com", tt.name); got != tt.ok {
			t.Fatalf("ValidClassName(%q) = %v, want %v", tt.name, got, tt.ok)
		}
	}
	if !ValidTypeName("e-x-a-m-p-l-e.com/bool") {
		t.Fatal("expected dashed type name to be valid")
	}
	if ValidTypeName("example.com/Bool") {
		t.Fatal("expected uppercase type local name to be invalid")
	}
}

func TestIDRoundTrip(t *testing.T) {
	full := FullLinkName{
		Link:   234234,
		Input:  FullPortID{Node: 1234, Port: PortID{Number: 5678, Kind: InputPort}},
		Output: FullPortID{Node: 4532, Port: PortID{Number: 9879, Kind: OutputPort}},
	}
	parsed, err := ParseFullLinkName(full.String())
	if err != nil {
		t.Fatal(err)
	}
	if parsed != full {
		t.Fatalf("parsed %#v, want %#v", parsed, full)
	}
}

func TestLinkValidationMultiplicityAndCycle(t *testing.T) {
	w, _ := testWorkspace(t)
	a, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	inB := FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}}
	outA := FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}}
	if _, err := w.CreateLink(inB, outA, LinkOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.CreateLink(inB, FullPortID{Node: c, Port: PortID{Number: 1, Kind: OutputPort}}, LinkOptions{}); !errors.Is(err, ErrMultiplicity) {
		t.Fatalf("expected multiplicity error, got %v", err)
	}
	if _, err := w.CreateLink(FullPortID{Node: a, Port: PortID{Number: 1, Kind: InputPort}}, FullPortID{Node: b, Port: PortID{Number: 1, Kind: OutputPort}}, LinkOptions{}); !errors.Is(err, ErrCycle) {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestRecallClassPreservesInactiveLink(t *testing.T) {
	w, _ := testWorkspace(t)
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	link, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.RecallClass("example.com", "example.com/Source"); err != nil {
		t.Fatal(err)
	}
	got, ok := w.Link(link)
	if !ok {
		t.Fatal("inactive link should be preserved")
	}
	if got.State != StateInactive {
		t.Fatalf("link state = %s, want inactive", got.State)
	}
}

func TestDeleteNodeRemovesBrokenLinks(t *testing.T) {
	w, _ := testWorkspace(t)
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	link, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.DeleteNode(a); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.Link(link); ok {
		t.Fatal("broken link should be removed")
	}
}

func TestSetNodePortsRejectsInvalidLinkedType(t *testing.T) {
	w, class := testWorkspace(t)
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	if _, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	); err != nil {
		t.Fatal(err)
	}
	inputs := append([]PortSpec(nil), class.Inputs...)
	inputs[0].FixedType = "example.com/float"
	if err := w.SetNodePorts(b, inputs, class.Outputs); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("expected type mismatch, got %v", err)
	}
	if err := w.SetNodePorts(b, class.Inputs, class.Outputs); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRestoreAndPasteRemapIDs(t *testing.T) {
	w, _ := testWorkspace(t)
	a, _ := w.CreateNode("example.com/Source", NodeOptions{State: NodeState{Coordinate: "x:1"}})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{State: NodeState{Coordinate: "x:2"}})
	link, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{Waypoints: []string{"p1"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	saved := w.Save()
	restored, _ := testWorkspace(t)
	if err := restored.Restore(saved); err != nil {
		t.Fatal(err)
	}
	if got, ok := restored.Link(link); !ok || got.Waypoints[0] != "p1" {
		t.Fatalf("restored link = %#v, ok %v", got, ok)
	}
	clip, err := restored.Copy([]NodeID{a, b})
	if err != nil {
		t.Fatal(err)
	}
	nodes, links, err := restored.Paste(clip)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || len(links) != 1 {
		t.Fatalf("pasted %d nodes and %d links", len(nodes), len(links))
	}
	if nodes[0] == a || nodes[1] == b || links[0] == link {
		t.Fatal("paste reused original IDs")
	}
}

type lifecycleClass struct {
	nodes map[NodeID]*lifecycleNode
	log   *[]string
	fail  bool
}

func (c *lifecycleClass) InitNode(ctx NodeContext, _ NodeState, mode InitMode) (NodeRuntime, error) {
	if c.fail {
		return nil, fmt.Errorf("init failed")
	}
	node := &lifecycleNode{id: ctx.ID, log: c.log}
	if c.nodes != nil {
		c.nodes[ctx.ID] = node
	}
	*c.log = append(*c.log, "init:"+string(mode))
	return node, nil
}

type lifecycleNode struct {
	id              NodeID
	log             *[]string
	object          any
	failAttach      bool
	failDetach      bool
	failInactive    bool
	panicOnAttach   bool
	panicOnProvider bool
}

func (n *lifecycleNode) LinkObject(endpoint LinkEndpoint) (any, error) {
	if n.panicOnProvider {
		panic("provider panic")
	}
	n.object = fmt.Sprintf("object:%s", endpoint.Type)
	*n.log = append(*n.log, "object")
	return n.object, nil
}

func (n *lifecycleNode) BeforeLinkAttach(endpoint LinkEndpoint, object any) error {
	if n.panicOnAttach {
		panic("attach panic")
	}
	*n.log = append(*n.log, fmt.Sprintf("before:%s:%v", endpoint.Direction, object))
	if n.failAttach {
		return fmt.Errorf("attach failed")
	}
	return nil
}

func (n *lifecycleNode) AfterLinkAttach(endpoint LinkEndpoint, object any) {
	*n.log = append(*n.log, fmt.Sprintf("after:%s:%d:%v", endpoint.Direction, endpoint.Link, object))
}

func (n *lifecycleNode) BeforeLinkDetach(endpoint LinkEndpoint) error {
	*n.log = append(*n.log, fmt.Sprintf("detach-before:%s:%d", endpoint.Direction, endpoint.Link))
	if n.failDetach {
		return fmt.Errorf("detach failed")
	}
	return nil
}

func (n *lifecycleNode) AfterLinkDetach(endpoint LinkEndpoint) {
	*n.log = append(*n.log, fmt.Sprintf("detach-after:%s:%d", endpoint.Direction, endpoint.Link))
}

func (n *lifecycleNode) AfterLinkInactive(endpoint LinkEndpoint, reason InactiveReason) {
	*n.log = append(*n.log, fmt.Sprintf("link-inactive:%s:%d:%s", endpoint.Direction, endpoint.Link, reason))
}

func (n *lifecycleNode) BeforeInactive(reason InactiveReason) error {
	*n.log = append(*n.log, fmt.Sprintf("inactive-before:%d:%s", n.id, reason))
	if n.failInactive {
		return fmt.Errorf("inactive failed")
	}
	return nil
}

func (n *lifecycleNode) AfterInactive(reason InactiveReason) {
	*n.log = append(*n.log, fmt.Sprintf("inactive-after:%d:%s", n.id, reason))
}

func (n *lifecycleNode) BeforeDelete() error {
	*n.log = append(*n.log, fmt.Sprintf("delete-before:%d", n.id))
	return nil
}

func (n *lifecycleNode) AfterDelete() {
	*n.log = append(*n.log, fmt.Sprintf("delete-after:%d", n.id))
}

func (n *lifecycleNode) Close() error {
	*n.log = append(*n.log, fmt.Sprintf("close:%d", n.id))
	return nil
}

func lifecycleWorkspace(t *testing.T, classRuntime *lifecycleClass) (*Workspace, map[NodeID]*lifecycleNode, *[]string) {
	t.Helper()
	log := []string{}
	nodes := map[NodeID]*lifecycleNode{}
	classRuntime.nodes = nodes
	classRuntime.log = &log
	class := ClassSpec{
		Name:    "example.com/Source",
		Runtime: classRuntime,
		Inputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: InputPort},
			Name:      "in",
			Direction: InputPort,
			FixedType: testType,
		}},
		Outputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: OutputPort},
			Name:      "out",
			Direction: OutputPort,
			FixedType: testType,
		}},
	}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{class}}); err != nil {
		t.Fatal(err)
	}
	return w, nodes, &log
}

func TestLifecycleCreateLinkUsesInputObjectAndAttachDetachHooks(t *testing.T) {
	w, _, log := lifecycleWorkspace(t, &lifecycleClass{})
	a, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	link, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.DeleteLink(link); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"init:new", "init:new",
		"object",
		"before:input:object:example.com/int",
		"before:output:object:example.com/int",
		"after:input:1:object:example.com/int",
		"after:output:1:object:example.com/int",
		"detach-before:input:1",
		"detach-before:output:1",
		"detach-after:input:1",
		"detach-after:output:1",
	}
	if fmt.Sprint(*log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", *log, want)
	}
}

func TestLifecycleCreateLinkRollsBackOnHookErrorOrPanic(t *testing.T) {
	w, nodes, _ := lifecycleWorkspace(t, &lifecycleClass{})
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	nodes[a].failAttach = true
	if _, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	); err == nil {
		t.Fatal("expected hook error")
	}
	if len(w.Snapshot().Links) != 0 {
		t.Fatal("failed attach should not create a link")
	}
	nodes[a].failAttach = false
	nodes[b].panicOnProvider = true
	if _, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	); err == nil {
		t.Fatal("expected panic hook error")
	}
	if len(w.Snapshot().Links) != 0 {
		t.Fatal("panicking provider should not create a link")
	}
}

func TestLifecyclePasteInitializesNodesAsRestore(t *testing.T) {
	w, _, log := lifecycleWorkspace(t, &lifecycleClass{})
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	clip, err := w.Copy([]NodeID{a})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.Paste(clip); err != nil {
		t.Fatal(err)
	}
	want := []string{"init:new", "init:restore"}
	if fmt.Sprint(*log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", *log, want)
	}
}

func TestLifecycleRestoreInitializesActiveNodesAsRestore(t *testing.T) {
	source, _, _ := lifecycleWorkspace(t, &lifecycleClass{})
	a, _ := source.CreateNode("example.com/Source", NodeOptions{})
	b, _ := source.CreateNode("example.com/Source", NodeOptions{})
	if _, err := source.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	); err != nil {
		t.Fatal(err)
	}
	saved := source.Save()

	restored, _, log := lifecycleWorkspace(t, &lifecycleClass{})
	if err := restored.Restore(saved); err != nil {
		t.Fatal(err)
	}
	want := []string{"init:restore", "init:restore"}
	if fmt.Sprint(*log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", *log, want)
	}
	if len(restored.Snapshot().Links) != 1 {
		t.Fatal("restore should preserve valid links")
	}
}

func TestLifecycleRestoreRollsBackOnInitError(t *testing.T) {
	source, _, _ := lifecycleWorkspace(t, &lifecycleClass{})
	if _, err := source.CreateNode("example.com/Source", NodeOptions{}); err != nil {
		t.Fatal(err)
	}
	saved := source.Save()

	restored, _, _ := lifecycleWorkspace(t, &lifecycleClass{})
	original := restored.Save()
	class := &lifecycleClass{fail: true}
	if err := restored.DefineClass("example.com", ClassSpec{
		Name:    "example.com/Source",
		Runtime: class,
		Inputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: InputPort},
			Name:      "in",
			Direction: InputPort,
			FixedType: testType,
		}},
		Outputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: OutputPort},
			Name:      "out",
			Direction: OutputPort,
			FixedType: testType,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := restored.Restore(saved); err == nil {
		t.Fatal("expected restore init error")
	}
	after := restored.Save()
	if fmt.Sprint(after) != fmt.Sprint(original) {
		t.Fatalf("restore should roll back on init error: got %#v, want %#v", after, original)
	}
}

func TestLifecycleDeleteNodeAndCloseHooks(t *testing.T) {
	w, _, log := lifecycleWorkspace(t, &lifecycleClass{})
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	if err := w.DeleteNode(a); err != nil {
		t.Fatal(err)
	}
	if _, err := w.CreateNode("example.com/Source", NodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"init:new",
		"delete-before:1",
		"delete-after:1",
		"init:new",
		"inactive-before:2:workspace-close",
		"inactive-after:2:workspace-close",
		"close:2",
	}
	if fmt.Sprint(*log) != fmt.Sprint(want) {
		t.Fatalf("log after delete = %#v, want %#v", *log, want)
	}
}

func TestLifecycleRecallClassRunsInactiveHooksAndPreservesLink(t *testing.T) {
	w, _, log := lifecycleWorkspace(t, &lifecycleClass{})
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	link, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	*log = nil
	if err := w.RecallClass("example.com", "example.com/Source"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"inactive-before:1:class-recall",
		"inactive-before:2:class-recall",
		"inactive-after:1:class-recall",
		"inactive-after:2:class-recall",
		"link-inactive:input:1:class-recall",
		"link-inactive:output:1:class-recall",
	}
	if fmt.Sprint(*log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", *log, want)
	}
	got, ok := w.Link(link)
	if !ok || got.State != StateInactive {
		t.Fatalf("link = %#v, ok %v; want inactive preserved link", got, ok)
	}
}

func TestLifecycleInactiveHookErrorPreventsRecall(t *testing.T) {
	w, nodes, _ := lifecycleWorkspace(t, &lifecycleClass{})
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	nodes[a].failInactive = true
	if err := w.RecallClass("example.com", "example.com/Source"); err == nil {
		t.Fatal("expected inactive hook error")
	}
	got, ok := w.Node(a)
	if !ok || got.State != StateActive {
		t.Fatalf("node = %#v, ok %v; want active after failed recall", got, ok)
	}
}
