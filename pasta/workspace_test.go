package pasta

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
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

type panicLibrary struct{}

func (panicLibrary) Name() string { return "example.com" }

func (panicLibrary) DefineClasses(LibraryScope) error {
	panic("define classes panic")
}

func TestRegisterLibraryRecoversPanic(t *testing.T) {
	w := NewWorkspace()
	if err := w.RegisterLibrary(panicLibrary{}); err == nil {
		t.Fatal("expected register panic error")
	}
	if len(w.Snapshot().Libraries) != 0 {
		t.Fatal("panicking library registration should be rolled back")
	}
}

type captureScopeLibrary struct {
	name    string
	classes []ClassSpec
	scope   LibraryScope
}

func (l *captureScopeLibrary) Name() string { return l.name }

func (l *captureScopeLibrary) DefineClasses(scope LibraryScope) error {
	l.scope = scope
	for _, class := range l.classes {
		if err := scope.DefineClass(class); err != nil {
			return err
		}
	}
	return nil
}

func scopedTestClass(library, local string) ClassSpec {
	return ClassSpec{
		Name: library + "/" + local,
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
}

func TestLibraryScopeRejectsCrossLibraryMutation(t *testing.T) {
	w := NewWorkspace()
	own := &captureScopeLibrary{name: "example.com", classes: []ClassSpec{scopedTestClass("example.com", "Source")}}
	other := &captureScopeLibrary{name: "other.com", classes: []ClassSpec{scopedTestClass("other.com", "Source")}}
	if err := w.RegisterLibrary(own); err != nil {
		t.Fatal(err)
	}
	if err := w.RegisterLibrary(other); err != nil {
		t.Fatal(err)
	}
	ownA, err := own.scope.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ownB, err := own.scope.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	otherA, err := other.scope.CreateNode("other.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	otherB, err := other.scope.CreateNode("other.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := own.scope.CreateNode("other.com/Source", NodeOptions{}); !errors.Is(err, ErrOwnership) {
		t.Fatalf("CreateNode cross-library error = %v, want ownership", err)
	}
	if err := own.scope.DeleteNode(otherA); !errors.Is(err, ErrOwnership) {
		t.Fatalf("DeleteNode cross-library error = %v, want ownership", err)
	}
	if err := own.scope.SetNodePrivate(otherA, "private"); !errors.Is(err, ErrOwnership) {
		t.Fatalf("SetNodePrivate cross-library error = %v, want ownership", err)
	}
	if err := own.scope.RecallClass("other.com/Source"); !errors.Is(err, ErrOwnership) {
		t.Fatalf("RecallClass cross-library error = %v, want ownership", err)
	}
	if err := own.scope.DefineClass(scopedTestClass("other.com", "Other")); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("DefineClass cross-library error = %v, want invalid name", err)
	}
	ownLink, err := own.scope.CreateLink(
		FullPortID{Node: ownB, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: ownA, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	otherLink, err := other.scope.CreateLink(
		FullPortID{Node: otherB, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: otherA, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := own.scope.CreateLink(
		FullPortID{Node: otherA, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: ownA, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	); !errors.Is(err, ErrOwnership) {
		t.Fatalf("CreateLink cross-library error = %v, want ownership", err)
	}
	if err := own.scope.DeleteLink(otherLink); !errors.Is(err, ErrOwnership) {
		t.Fatalf("DeleteLink cross-library error = %v, want ownership", err)
	}
	if err := own.scope.DeleteLink(ownLink); err != nil {
		t.Fatal(err)
	}
}

func TestSetNodePrivateUpdatesSnapshotsSaveAndCopy(t *testing.T) {
	w, _ := testWorkspace(t)
	node, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetNodePrivate(node, map[string]string{"value": "from-runtime"}); err != nil {
		t.Fatal(err)
	}
	snapshot, ok := w.Node(node)
	if !ok {
		t.Fatal("node should exist")
	}
	private, ok := snapshot.Dynamic.Private.(map[string]string)
	if !ok || private["value"] != "from-runtime" {
		t.Fatalf("snapshot private = %#v", snapshot.Dynamic.Private)
	}
	saved := w.Save()
	if len(saved.Nodes) != 1 {
		t.Fatalf("saved %d nodes, want 1", len(saved.Nodes))
	}
	private, ok = saved.Nodes[0].State.Private.(map[string]string)
	if !ok || private["value"] != "from-runtime" {
		t.Fatalf("saved private = %#v", saved.Nodes[0].State.Private)
	}
	clip, err := w.Copy([]NodeID{node})
	if err != nil {
		t.Fatal(err)
	}
	private, ok = clip.Nodes[0].State.Private.(map[string]string)
	if !ok || private["value"] != "from-runtime" {
		t.Fatalf("clipboard private = %#v", clip.Nodes[0].State.Private)
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

func TestSaveOutputIsDeterministic(t *testing.T) {
	w, _ := testWorkspace(t)
	a, _ := w.CreateNode("example.com/Source", NodeOptions{State: NodeState{
		DisplayName: "source",
		Coordinate:  "x:1",
		Metadata:    map[string]string{"z": "last", "a": "first"},
		Private:     map[string]any{"count": float64(2), "label": "alpha"},
	}})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{State: NodeState{
		DisplayName: "sink",
		Coordinate:  "x:2",
	}})
	if _, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{Waypoints: []string{"p2", "p1"}},
	); err != nil {
		t.Fatal(err)
	}
	gotBytes, err := json.MarshalIndent(w.Save(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(gotBytes)
	want := `{
  "nextNode": 3,
  "nextLink": 2,
  "nodes": [
    {
      "id": "1N",
      "class": "example.com/Source",
      "state": {
        "DisplayName": "source",
        "Description": "",
        "PrimaryType": "",
        "Coordinate": "x:1",
        "Metadata": {
          "a": "first",
          "z": "last"
        },
        "Private": {
          "count": 2,
          "label": "alpha"
        }
      },
      "inputs": [
        {
          "ID": {
            "Number": 1,
            "Kind": "input"
          },
          "Name": "in",
          "Direction": "input",
          "FixedType": "example.com/int",
          "AcceptedTypes": null,
          "Multiple": false,
          "Metadata": null
        }
      ],
      "outputs": [
        {
          "ID": {
            "Number": 1,
            "Kind": "output"
          },
          "Name": "out",
          "Direction": "output",
          "FixedType": "example.com/int",
          "AcceptedTypes": null,
          "Multiple": false,
          "Metadata": null
        }
      ]
    },
    {
      "id": "2N",
      "class": "example.com/Source",
      "state": {
        "DisplayName": "sink",
        "Description": "",
        "PrimaryType": "",
        "Coordinate": "x:2",
        "Metadata": null,
        "Private": null
      },
      "inputs": [
        {
          "ID": {
            "Number": 1,
            "Kind": "input"
          },
          "Name": "in",
          "Direction": "input",
          "FixedType": "example.com/int",
          "AcceptedTypes": null,
          "Multiple": false,
          "Metadata": null
        }
      ],
      "outputs": [
        {
          "ID": {
            "Number": 1,
            "Kind": "output"
          },
          "Name": "out",
          "Direction": "output",
          "FixedType": "example.com/int",
          "AcceptedTypes": null,
          "Multiple": false,
          "Metadata": null
        }
      ]
    }
  ],
  "links": [
    {
      "name": "1L:2N1i:1N1o",
      "type": "example.com/int",
      "waypoints": [
        "p2",
        "p1"
      ]
    }
  ]
}`
	if got != want {
		t.Fatalf("save JSON:\n%s", got)
	}
}

type lifecycleClass struct {
	nodes map[NodeID]*lifecycleNode
	log   *[]string
	fail  bool
	panic bool
}

func (c *lifecycleClass) InitNode(ctx NodeContext, _ NodeState, mode InitMode) (NodeRuntime, error) {
	if c.panic {
		panic("init panic")
	}
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
	id                 NodeID
	log                *[]string
	object             any
	failAttach         bool
	failDetach         bool
	failInactive       bool
	panicOnAttach      bool
	panicOnProvider    bool
	panicAfterAttach   bool
	panicOnDetach      bool
	panicAfterDetach   bool
	panicOnInactive    bool
	panicAfterInactive bool
	panicOnDelete      bool
	panicAfterDelete   bool
	panicOnClose       bool
	inspectOnAttach    WorkspaceRO
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
	if n.inspectOnAttach != nil {
		_ = n.inspectOnAttach.Snapshot()
		*n.log = append(*n.log, "inspect")
	}
	*n.log = append(*n.log, fmt.Sprintf("before:%s:%v", endpoint.Direction, object))
	if n.failAttach {
		return fmt.Errorf("attach failed")
	}
	return nil
}

func (n *lifecycleNode) AfterLinkAttach(endpoint LinkEndpoint, object any) {
	if n.panicAfterAttach {
		panic("after attach panic")
	}
	*n.log = append(*n.log, fmt.Sprintf("after:%s:%d:%v", endpoint.Direction, endpoint.Link, object))
}

func (n *lifecycleNode) BeforeLinkDetach(endpoint LinkEndpoint) error {
	if n.panicOnDetach {
		panic("detach panic")
	}
	*n.log = append(*n.log, fmt.Sprintf("detach-before:%s:%d", endpoint.Direction, endpoint.Link))
	if n.failDetach {
		return fmt.Errorf("detach failed")
	}
	return nil
}

func (n *lifecycleNode) AfterLinkDetach(endpoint LinkEndpoint) {
	if n.panicAfterDetach {
		panic("after detach panic")
	}
	*n.log = append(*n.log, fmt.Sprintf("detach-after:%s:%d", endpoint.Direction, endpoint.Link))
}

func (n *lifecycleNode) AfterLinkInactive(endpoint LinkEndpoint, reason InactiveReason) {
	*n.log = append(*n.log, fmt.Sprintf("link-inactive:%s:%d:%s", endpoint.Direction, endpoint.Link, reason))
}

func (n *lifecycleNode) BeforeInactive(reason InactiveReason) error {
	if n.panicOnInactive {
		panic("inactive panic")
	}
	*n.log = append(*n.log, fmt.Sprintf("inactive-before:%d:%s", n.id, reason))
	if n.failInactive {
		return fmt.Errorf("inactive failed")
	}
	return nil
}

func (n *lifecycleNode) AfterInactive(reason InactiveReason) {
	if n.panicAfterInactive {
		panic("after inactive panic")
	}
	*n.log = append(*n.log, fmt.Sprintf("inactive-after:%d:%s", n.id, reason))
}

func (n *lifecycleNode) BeforeDelete() error {
	if n.panicOnDelete {
		panic("delete panic")
	}
	*n.log = append(*n.log, fmt.Sprintf("delete-before:%d", n.id))
	return nil
}

func (n *lifecycleNode) AfterDelete() {
	if n.panicAfterDelete {
		panic("after delete panic")
	}
	*n.log = append(*n.log, fmt.Sprintf("delete-after:%d", n.id))
}

func (n *lifecycleNode) Close() error {
	if n.panicOnClose {
		panic("close panic")
	}
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

func TestLifecycleAfterAttachPanicDoesNotRollbackLink(t *testing.T) {
	w, nodes, _ := lifecycleWorkspace(t, &lifecycleClass{})
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	nodes[b].panicAfterAttach = true
	link, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := w.Link(link); !ok {
		t.Fatal("after attach panic should be recovered after link commit")
	}
}

func TestLifecycleCreateLinkHooksMayReadWorkspace(t *testing.T) {
	w, nodes, log := lifecycleWorkspace(t, &lifecycleClass{})
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	nodes[a].inspectOnAttach = w
	done := make(chan error, 1)
	go func() {
		_, err := w.CreateLink(
			FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
			FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
			LinkOptions{},
		)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CreateLink hook could not read workspace; likely recursive lock deadlock")
	}
	if fmt.Sprint(*log) != fmt.Sprint([]string{
		"init:new", "init:new",
		"object",
		"before:input:object:example.com/int",
		"inspect",
		"before:output:object:example.com/int",
		"after:input:1:object:example.com/int",
		"after:output:1:object:example.com/int",
	}) {
		t.Fatalf("log = %#v", *log)
	}
}

type blockingAttachClass struct {
	mu    sync.Mutex
	nodes map[NodeID]*blockingAttachNode
}

func (c *blockingAttachClass) InitNode(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
	node := &blockingAttachNode{}
	c.mu.Lock()
	if c.nodes == nil {
		c.nodes = make(map[NodeID]*blockingAttachNode)
	}
	c.nodes[ctx.ID] = node
	c.mu.Unlock()
	return node, nil
}

func (c *blockingAttachClass) node(id NodeID) *blockingAttachNode {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nodes[id]
}

type blockingAttachNode struct {
	ready   chan struct{}
	release chan struct{}
	once    sync.Once
}

func (n *blockingAttachNode) BeforeLinkAttach(LinkEndpoint, any) error {
	if n.ready != nil {
		n.once.Do(func() { close(n.ready) })
		<-n.release
	}
	return nil
}

func (n *blockingAttachNode) AfterLinkAttach(LinkEndpoint, any) {}

func TestCreateLinkRevalidatesAfterConcurrentInterleaving(t *testing.T) {
	runtime := &blockingAttachClass{}
	w, _ := lifecycleFreeWorkspace(t, runtime)
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	c, _ := w.CreateNode("example.com/Source", NodeOptions{})
	ready := make(chan struct{})
	release := make(chan struct{})
	runtime.node(a).ready = ready
	runtime.node(a).release = release
	first := make(chan error, 1)
	go func() {
		_, err := w.CreateLink(
			FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
			FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
			LinkOptions{},
		)
		first <- err
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("first link did not reach attach hook")
	}
	second, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: c, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-first:
		if !errors.Is(err, ErrMultiplicity) {
			t.Fatalf("first link error = %v, want multiplicity", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first link did not finish")
	}
	snapshot := w.Snapshot()
	if len(snapshot.Links) != 1 || snapshot.Links[0].ID != second {
		t.Fatalf("links = %#v, want only second link %s", snapshot.Links, second)
	}
}

func lifecycleFreeWorkspace(t *testing.T, classRuntime NodeClass) (*Workspace, ClassSpec) {
	t.Helper()
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
	return w, class
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

type restoreOrderClass struct {
	log *[]NodeID
}

func (c restoreOrderClass) InitNode(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
	*c.log = append(*c.log, ctx.ID)
	return nil, nil
}

func TestRestoreInitializesNodesInDeterministicDAGOrder(t *testing.T) {
	log := []NodeID{}
	class := ClassSpec{
		Name:    "example.com/Source",
		Runtime: restoreOrderClass{log: &log},
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
	data := SaveData{
		Nodes: []SaveNode{
			{ID: "1N", Class: "example.com/Source"},
			{ID: "2N", Class: "example.com/Source"},
			{ID: "3N", Class: "example.com/Source"},
		},
		Links: []SaveLink{
			{Name: "1L:2N1i:1N1o", Type: testType},
			{Name: "2L:3N1i:2N1o", Type: testType},
		},
	}
	if err := w.Restore(data); err != nil {
		t.Fatal(err)
	}
	want := []NodeID{3, 2, 1}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("restore init order = %#v, want %#v", log, want)
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

func TestLifecycleRestoreRollsBackOnInitPanic(t *testing.T) {
	source, _, _ := lifecycleWorkspace(t, &lifecycleClass{})
	if _, err := source.CreateNode("example.com/Source", NodeOptions{}); err != nil {
		t.Fatal(err)
	}
	saved := source.Save()

	restored, _, _ := lifecycleWorkspace(t, &lifecycleClass{})
	original := restored.Save()
	if err := restored.DefineClass("example.com", ClassSpec{
		Name:    "example.com/Source",
		Runtime: &lifecycleClass{panic: true},
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
		t.Fatal("expected restore init panic error")
	}
	after := restored.Save()
	if fmt.Sprint(after) != fmt.Sprint(original) {
		t.Fatalf("restore should roll back on init panic: got %#v, want %#v", after, original)
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

func TestLifecycleDeleteAndDetachPanicsAreRecovered(t *testing.T) {
	w, nodes, _ := lifecycleWorkspace(t, &lifecycleClass{})
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
	nodes[b].panicOnDetach = true
	if err := w.DeleteLink(link); err == nil {
		t.Fatal("expected before detach panic error")
	}
	if _, ok := w.Link(link); !ok {
		t.Fatal("before detach panic should leave link intact")
	}
	nodes[b].panicOnDetach = false
	nodes[b].panicAfterDetach = true
	if err := w.DeleteLink(link); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.Link(link); ok {
		t.Fatal("after detach panic should not preserve deleted link")
	}

	nodes[a].panicOnDelete = true
	if err := w.DeleteNode(a); err == nil {
		t.Fatal("expected before delete panic error")
	}
	if _, ok := w.Node(a); !ok {
		t.Fatal("before delete panic should leave node intact")
	}
	nodes[a].panicOnDelete = false
	nodes[a].panicAfterDelete = true
	if err := w.DeleteNode(a); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.Node(a); ok {
		t.Fatal("after delete panic should not preserve deleted node")
	}
}

func TestLifecycleInactiveAndClosePanicsAreRecovered(t *testing.T) {
	w, nodes, _ := lifecycleWorkspace(t, &lifecycleClass{})
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	nodes[a].panicOnInactive = true
	if err := w.RecallClass("example.com", "example.com/Source"); err == nil {
		t.Fatal("expected before inactive panic error")
	}
	if got, ok := w.Node(a); !ok || got.State != StateActive {
		t.Fatalf("node = %#v, ok %v; want active after before inactive panic", got, ok)
	}
	nodes[a].panicOnInactive = false
	nodes[a].panicAfterInactive = true
	if err := w.RecallClass("example.com", "example.com/Source"); err != nil {
		t.Fatal(err)
	}
	if got, ok := w.Node(a); !ok || got.State != StateInactive {
		t.Fatalf("node = %#v, ok %v; want inactive after after-inactive panic", got, ok)
	}

	w2, nodes2, _ := lifecycleWorkspace(t, &lifecycleClass{})
	b, _ := w2.CreateNode("example.com/Source", NodeOptions{})
	nodes2[b].panicOnClose = true
	if err := w2.Close(); err == nil {
		t.Fatal("expected close panic error")
	}
	if got, ok := w2.Node(b); !ok || got.State != StateInactive {
		t.Fatalf("node = %#v, ok %v; want inactive after close panic", got, ok)
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
