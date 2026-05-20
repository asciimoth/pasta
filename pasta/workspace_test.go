package pasta

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asciimoth/configer/configer"
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

func TestSingleNodeClasses(t *testing.T) {
	const singleClass = "example.com/Singleton"
	const regularClass = "example.com/Regular"
	initIDs := []NodeID{}
	checkRestoreSnapshot := false
	singleSpec := ClassSpec{
		Name:       singleClass,
		SingleNode: true,
		Runtime: nodeClassFunc(func(ctx NodeContext, _ NodeState, mode InitMode) (NodeRuntime, error) {
			if mode == InitRestore && checkRestoreSnapshot {
				snap := ctx.ReadOnly.Snapshot()
				seen := 0
				for _, node := range snap.Nodes {
					if node.Class == singleClass {
						seen++
					}
				}
				if seen != 1 {
					return nil, fmt.Errorf("restore init saw %d single nodes, want 1", seen)
				}
			}
			initIDs = append(initIDs, ctx.ID)
			return struct{}{}, nil
		}),
	}
	regularSpec := ClassSpec{Name: regularClass}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{singleSpec, regularSpec}}); err != nil {
		t.Fatal(err)
	}
	first, err := w.CreateNode(singleClass, NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.CreateNode(singleClass, NodeOptions{}); !errors.Is(err, ErrMultiplicity) {
		t.Fatalf("second CreateNode error = %v, want multiplicity", err)
	}
	if err := w.CanCreateNode(singleClass); !errors.Is(err, ErrMultiplicity) {
		t.Fatalf("CanCreateNode with existing node = %v, want multiplicity", err)
	}
	if nodes := w.Snapshot().Nodes; len(nodes) != 1 || nodes[0].ID != first {
		t.Fatalf("nodes after rejected create = %#v, want only %s", nodes, first)
	}
	regular, err := w.CreateNode(regularClass, NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	clip, err := w.Copy([]NodeID{first, regular})
	if err != nil {
		t.Fatal(err)
	}
	pasted, links, err := w.Paste(clip)
	if err != nil {
		t.Fatalf("Paste over existing single node = %v, want skipped duplicate", err)
	}
	if len(pasted) != 1 || len(links) != 0 {
		t.Fatalf("paste with existing single node = nodes %#v links %#v, want one regular node", pasted, links)
	}
	if snap, ok := w.Node(pasted[0]); !ok || snap.Class != regularClass {
		t.Fatalf("pasted duplicate-filtered node = %#v, ok %v; want regular", snap, ok)
	}
	if nodes := w.Snapshot().Nodes; len(nodes) != 3 {
		t.Fatalf("nodes after duplicate-filtered paste = %#v, want original single and two regular nodes", nodes)
	}

	empty := NewWorkspace()
	if err := empty.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{singleSpec}}); err != nil {
		t.Fatal(err)
	}
	pasted, links, err = empty.Paste(Clipboard{Nodes: []SaveNode{
		{ID: "1N", Class: singleClass},
		{ID: "2N", Class: singleClass},
	}})
	if err != nil {
		t.Fatalf("Paste duplicate single nodes = %v, want skipped duplicate", err)
	}
	if len(pasted) != 1 || len(links) != 0 {
		t.Fatalf("duplicate single paste = nodes %#v links %#v, want one node", pasted, links)
	}
	if nodes := empty.Snapshot().Nodes; len(nodes) != 1 || nodes[0].ID != pasted[0] {
		t.Fatalf("duplicate paste nodes = %#v, want pasted singleton %s", nodes, pasted[0])
	}

	initIDs = nil
	restored := NewWorkspace()
	if err := restored.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{singleSpec}}); err != nil {
		t.Fatal(err)
	}
	checkRestoreSnapshot = true
	if err := restored.Restore(SaveData{Nodes: []SaveNode{
		{ID: "3N", Class: singleClass},
		{ID: "1N", Class: singleClass},
		{ID: "2N", Class: singleClass},
	}}); err != nil {
		t.Fatal(err)
	}
	nodes := restored.Snapshot().Nodes
	if len(nodes) != 1 || nodes[0].ID != 1 {
		t.Fatalf("restored nodes = %#v, want only lowest ID 1N", nodes)
	}
	if len(initIDs) != 1 || initIDs[0] != 1 {
		t.Fatalf("restore init IDs = %#v, want [1N]", initIDs)
	}

	redefine := NewWorkspace()
	if err := redefine.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{Name: singleClass}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := redefine.CreateNode(singleClass, NodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := redefine.CreateNode(singleClass, NodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := redefine.DefineClass("example.com", singleSpec); !errors.Is(err, ErrMultiplicity) {
		t.Fatalf("redefine active duplicate class as single = %v, want multiplicity", err)
	}
	if nodes := redefine.Snapshot().Nodes; len(nodes) != 2 {
		t.Fatalf("failed single redefine should preserve active nodes: %#v", nodes)
	}
}

func TestKeyNodeAccessTracksActiveConnectivity(t *testing.T) {
	const (
		keyClass     = "example.com/Key"
		regularClass = "example.com/Regular"
	)
	hooks := &keyAccessHooks{events: map[NodeID][]bool{}}
	keySpec := keyAccessClass(keyClass, true, hooks)
	regularSpec := keyAccessClass(regularClass, false, hooks)
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{keySpec, regularSpec}}); err != nil {
		t.Fatal(err)
	}
	key, err := w.CreateNode(keyClass, NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	middle, err := w.CreateNode(regularClass, NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := w.CreateNode(regularClass, NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertKeyAccess(t, w, map[NodeID]bool{key: true, middle: false, leaf: false})
	if got := hooks.nodeEvents(key); len(got) != 1 || !got[0] {
		t.Fatalf("key hook events = %#v, want [true]", got)
	}
	if got := hooks.nodeEvents(middle); len(got) != 0 {
		t.Fatalf("unconnected regular hook events = %#v, want none", got)
	}

	linkToKey, err := w.CreateLink(
		FullPortID{Node: middle, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: key, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{Type: testType},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.CreateLink(
		FullPortID{Node: leaf, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: middle, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{Type: testType},
	); err != nil {
		t.Fatal(err)
	}
	assertKeyAccess(t, w, map[NodeID]bool{key: true, middle: true, leaf: true})
	if got := hooks.nodeEvents(middle); len(got) != 1 || !got[0] {
		t.Fatalf("middle hook events = %#v, want [true]", got)
	}
	if got := hooks.nodeEvents(leaf); len(got) != 1 || !got[0] {
		t.Fatalf("leaf hook events = %#v, want [true]", got)
	}

	if err := w.DeleteLink(linkToKey); err != nil {
		t.Fatal(err)
	}
	assertKeyAccess(t, w, map[NodeID]bool{key: true, middle: false, leaf: false})
	if got := hooks.nodeEvents(middle); len(got) != 2 || got[1] {
		t.Fatalf("middle hook events after detach = %#v, want trailing false", got)
	}
	if got := hooks.nodeEvents(leaf); len(got) != 2 || got[1] {
		t.Fatalf("leaf hook events after detach = %#v, want trailing false", got)
	}
}

func TestKeyNodeWithoutRuntimeHookGetsAccess(t *testing.T) {
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{
		Name:    "example.com/Key",
		KeyNode: true,
	}}}); err != nil {
		t.Fatal(err)
	}
	node, err := w.CreateNode("example.com/Key", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertKeyAccess(t, w, map[NodeID]bool{node: true})
}

func TestSingleNodePendingCreateRejectsReentrantCreate(t *testing.T) {
	const singleClass = "example.com/Singleton"
	w := NewWorkspace()
	var sawMultiplicity bool
	class := ClassSpec{
		Name:       singleClass,
		SingleNode: true,
		Runtime: nodeClassFunc(func(NodeContext, NodeState, InitMode) (NodeRuntime, error) {
			sawMultiplicity = errors.Is(w.CanCreateNode(singleClass), ErrMultiplicity)
			return struct{}{}, nil
		}),
	}
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{class}}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.CreateNode(singleClass, NodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !sawMultiplicity {
		t.Fatal("CanCreateNode did not see pending single-node create")
	}
}

func TestKeyNodeAccessRestoreAfterSingleNodeDedup(t *testing.T) {
	const (
		keyClass     = "example.com/Key"
		regularClass = "example.com/Regular"
	)
	hooks := &keyAccessHooks{events: map[NodeID][]bool{}}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{
		func() ClassSpec {
			spec := keyAccessClass(keyClass, true, hooks)
			spec.SingleNode = true
			return spec
		}(),
		keyAccessClass(regularClass, false, hooks),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Restore(SaveData{
		Nodes: []SaveNode{
			{ID: "2N", Class: keyClass},
			{ID: "1N", Class: keyClass},
			{ID: "3N", Class: regularClass},
		},
		Links: []SaveLink{{
			Name: FullLinkName{
				Link:   1,
				Input:  FullPortID{Node: 3, Port: PortID{Number: 1, Kind: InputPort}},
				Output: FullPortID{Node: 2, Port: PortID{Number: 1, Kind: OutputPort}},
			}.String(),
			Type: testType,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.Node(2); ok {
		t.Fatal("duplicate single-node key should be pruned before reachability")
	}
	assertKeyAccess(t, w, map[NodeID]bool{1: true, 3: false})
	if got := hooks.nodeEvents(1); len(got) != 1 || !got[0] {
		t.Fatalf("restored key hook events = %#v, want [true]", got)
	}
	if got := hooks.nodeEvents(3); len(got) != 0 {
		t.Fatalf("regular linked only to pruned key hook events = %#v, want none", got)
	}
}

func TestSingleNodeRestorePruningIgnoresNilAndNonSingleRecords(t *testing.T) {
	const singleClass = "example.com/Singleton"
	const regularClass = "example.com/Regular"
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{
		{Name: singleClass, SingleNode: true},
		{Name: regularClass},
	}}); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	nodes := map[NodeID]*nodeRecord{
		1: nil,
		2: {id: 2, class: regularClass, state: StateActive},
		3: {id: 3, class: singleClass, state: StateActive},
		4: nil,
		5: {id: 5, class: singleClass, state: StateActive},
	}
	w.pruneSingleNodeClassDuplicatesLocked(nodes)
	w.mu.Unlock()
	if _, ok := nodes[1]; !ok {
		t.Fatal("nil node in first pass should be ignored, not removed")
	}
	if _, ok := nodes[4]; !ok {
		t.Fatal("nil node in second pass should be ignored, not removed")
	}
	if _, ok := nodes[2]; !ok {
		t.Fatal("regular node should not be pruned")
	}
	if _, ok := nodes[3]; !ok {
		t.Fatal("lowest single-node class instance should be kept")
	}
	if _, ok := nodes[5]; ok {
		t.Fatal("higher single-node class duplicate should be pruned")
	}
}

func TestInternalActivityHelpersCoverEdgeBranches(t *testing.T) {
	const (
		keyClass     = "example.com/Key"
		regularClass = "example.com/Regular"
	)
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{
		{Name: keyClass, KeyNode: true},
		{Name: regularClass},
	}}); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	w.nodes[1] = nil
	w.nodes[2] = &nodeRecord{id: 2, class: regularClass, state: StateActive, keyAccess: true}
	w.nodes[3] = &nodeRecord{id: 3, class: keyClass, state: StateActive, keyAccess: true}
	if got := w.lowestNodeOfClassLocked(regularClass); got != 2 {
		w.mu.Unlock()
		t.Fatalf("lowestNodeOfClassLocked = %s, want 2N", got)
	}
	delete(w.nodes, 1)
	w.links[1] = &linkRecord{id: 1, state: StateInactive, input: FullPortID{Node: 2, Port: PortID{Number: 1, Kind: InputPort}}, output: FullPortID{Node: 3, Port: PortID{Number: 1, Kind: OutputPort}}}
	access := w.keyAccessLocked()
	if !access[3] || access[2] {
		w.mu.Unlock()
		t.Fatalf("keyAccessLocked with inactive link = %#v, want only key node", access)
	}
	events := w.keyAccessEventsForNodesLocked([]NodeID{3, 2, 3})
	w.mu.Unlock()
	if len(events) != 2 || events[0].id != 2 || events[1].id != 3 {
		t.Fatalf("keyAccessEventsForNodesLocked = %#v, want sorted unique nodes 2N and 3N", events)
	}
}

type keyAccessHooks struct {
	mu     sync.Mutex
	events map[NodeID][]bool
}

func (h *keyAccessHooks) add(id NodeID, access bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events[id] = append(h.events[id], access)
}

func (h *keyAccessHooks) nodeEvents(id NodeID) []bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]bool(nil), h.events[id]...)
}

type keyAccessRuntime struct {
	id    NodeID
	hooks *keyAccessHooks
}

func (r keyAccessRuntime) HasKeyNodeAccess(access bool) {
	r.hooks.add(r.id, access)
}

func keyAccessClass(name string, key bool, hooks *keyAccessHooks) ClassSpec {
	return ClassSpec{
		Name:    name,
		KeyNode: key,
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
		Runtime: nodeClassFunc(func(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
			return keyAccessRuntime{id: ctx.ID, hooks: hooks}, nil
		}),
	}
}

func assertKeyAccess(t *testing.T, w *Workspace, want map[NodeID]bool) {
	t.Helper()
	for id, access := range want {
		snap, ok := w.Node(id)
		if !ok {
			t.Fatalf("Node(%s) missing", id)
		}
		if snap.HasKeyNodeAccess != access {
			t.Fatalf("Node(%s).HasKeyNodeAccess = %v, want %v", id, snap.HasKeyNodeAccess, access)
		}
	}
}

type panicLibrary struct{}

func (panicLibrary) Name() string { return "example.com" }

func (panicLibrary) DefineClasses(LibraryScope) error {
	panic("define classes panic")
}

func TestRegisterLibraryRecoversPanic(t *testing.T) {
	logger := &testLogger{}
	w := NewWorkspace(WithLogger(logger))
	if err := w.RegisterLibrary(panicLibrary{}); err == nil {
		t.Fatal("expected register panic error")
	}
	if len(logger.errs) == 0 {
		t.Fatal("expected library panic to be logged")
	}
	if len(w.Snapshot().Libraries) != 0 {
		t.Fatal("panicking library registration should be rolled back")
	}
}

func TestClassLookupReturnsDefensiveSnapshot(t *testing.T) {
	class := ClassSpec{
		Name: "example.com/Source",
		Default: NodeState{
			Metadata: map[string]string{"default": "value"},
			Private:  map[string]any{"nested": map[string]any{"key": "value"}},
		},
		Inputs: []PortSpec{{
			ID:            PortID{Number: 1, Kind: InputPort},
			Name:          "in",
			Direction:     InputPort,
			AcceptedTypes: []string{testType},
			Metadata:      map[string]string{"side": "input"},
		}},
		Outputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: OutputPort},
			Name:      "out",
			Direction: OutputPort,
			FixedType: testType,
		}},
		Metadata: map[string]string{"class": "source"},
	}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{class}}); err != nil {
		t.Fatal(err)
	}
	snap, ok := w.Class("example.com/Source")
	if !ok {
		t.Fatal("class should exist")
	}
	if snap.Library != "example.com" || !snap.Active || snap.Spec.Metadata["class"] != "source" {
		t.Fatalf("class snapshot = %#v", snap)
	}
	snap.Spec.Metadata["class"] = "mutated"
	snap.Spec.Inputs[0].AcceptedTypes[0] = "example.com/float"
	snap.Spec.Inputs[0].Metadata["side"] = "mutated"
	snap.Spec.Default.Metadata["default"] = "mutated"
	snap.Spec.Default.Private.(map[string]any)["nested"].(map[string]any)["key"] = "mutated"

	next, ok := w.Class("example.com/Source")
	if !ok {
		t.Fatal("class should still exist")
	}
	if next.Spec.Metadata["class"] != "source" ||
		next.Spec.Inputs[0].AcceptedTypes[0] != testType ||
		next.Spec.Inputs[0].Metadata["side"] != "input" ||
		next.Spec.Default.Metadata["default"] != "value" ||
		next.Spec.Default.Private.(map[string]any)["nested"].(map[string]any)["key"] != "value" {
		t.Fatalf("class lookup leaked mutable state: %#v", next)
	}
	if _, ok := w.Class("example.com/Missing"); ok {
		t.Fatal("missing class lookup should report false")
	}
}

func TestClassQueriesReturnDeterministicDefensiveSnapshots(t *testing.T) {
	classes := []ClassSpec{
		{
			Name:     "example.com/Zed",
			Metadata: map[string]string{"order": "last"},
		},
		{
			Name:     "example.com/Alpha",
			Metadata: map[string]string{"order": "first"},
		},
	}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: classes}); err != nil {
		t.Fatal(err)
	}
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "other.com", Classes: []ClassSpec{{
		Name:     "other.com/Middle",
		Metadata: map[string]string{"library": "other"},
	}}}); err != nil {
		t.Fatal(err)
	}

	all := w.Classes()
	if len(all) != 3 {
		t.Fatalf("classes = %#v, want 3", all)
	}
	if all[0].Spec.Name != "example.com/Alpha" || all[1].Spec.Name != "example.com/Zed" || all[2].Spec.Name != "other.com/Middle" {
		t.Fatalf("classes not sorted by class name: %#v", all)
	}
	own := w.ClassesByLibrary("example.com")
	if len(own) != 2 || own[0].Spec.Name != "example.com/Alpha" || own[1].Spec.Name != "example.com/Zed" {
		t.Fatalf("classes by library = %#v", own)
	}
	if got := w.ClassesByLibrary("missing.com"); len(got) != 0 {
		t.Fatalf("missing library classes = %#v, want empty", got)
	}
	own[0].Spec.Metadata["order"] = "mutated"
	next := w.ClassesByLibrary("example.com")
	if next[0].Spec.Metadata["order"] != "first" {
		t.Fatalf("class query leaked mutable metadata: %#v", next[0].Spec.Metadata)
	}
}

type classLookupRuntime struct {
	snap ClassSnapshot
	ok   bool
}

func (r *classLookupRuntime) InitNode(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
	r.snap, r.ok = ctx.ReadOnly.Class(ctx.Class)
	return struct{}{}, nil
}

func TestNodeRuntimeCanQueryClassThroughReadOnlyContext(t *testing.T) {
	runtime := &classLookupRuntime{}
	class := ClassSpec{
		Name:        "example.com/Source",
		DisplayName: "Source",
		Runtime:     runtime,
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
	if _, err := w.CreateNode("example.com/Source", NodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !runtime.ok || runtime.snap.Spec.DisplayName != "Source" || runtime.snap.Library != "example.com" {
		t.Fatalf("runtime class lookup = %#v, ok %v", runtime.snap, runtime.ok)
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
	ownClasses := own.scope.Classes()
	if len(ownClasses) != 1 || ownClasses[0].Library != "example.com" || ownClasses[0].Spec.Name != "example.com/Source" {
		t.Fatalf("scoped classes = %#v, want only owned class", ownClasses)
	}
	ownClasses[0].Spec.Metadata = map[string]string{"mutated": "true"}
	if next := own.scope.Classes(); len(next) != 1 || len(next[0].Spec.Metadata) != 0 {
		t.Fatalf("scoped classes leaked mutable state: %#v", next)
	}
	if _, err := own.scope.CreateNode("other.com/Source", NodeOptions{}); !errors.Is(err, ErrOwnership) {
		t.Fatalf("CreateNode cross-library error = %v, want ownership", err)
	}
	if err := own.scope.CanCreateNode("other.com/Source"); !errors.Is(err, ErrOwnership) {
		t.Fatalf("CanCreateNode cross-library error = %v, want ownership", err)
	}
	if err := own.scope.DeleteNode(otherA); !errors.Is(err, ErrOwnership) {
		t.Fatalf("DeleteNode cross-library error = %v, want ownership", err)
	}
	if err := own.scope.CanDeleteNode(otherA); !errors.Is(err, ErrOwnership) {
		t.Fatalf("CanDeleteNode cross-library error = %v, want ownership", err)
	}
	if err := own.scope.SetNodeState(otherA, NodeState{DisplayName: "owned"}); !errors.Is(err, ErrOwnership) {
		t.Fatalf("SetNodeState cross-library error = %v, want ownership", err)
	}
	if err := own.scope.SetNodePrivate(otherA, "private"); !errors.Is(err, ErrOwnership) {
		t.Fatalf("SetNodePrivate cross-library error = %v, want ownership", err)
	}
	if err := own.scope.SetNodeCoordinate(otherA, "x:1"); !errors.Is(err, ErrOwnership) {
		t.Fatalf("SetNodeCoordinate cross-library error = %v, want ownership", err)
	}
	if err := own.scope.SetNodeMetadata(otherA, map[string]string{"key": "value"}); !errors.Is(err, ErrOwnership) {
		t.Fatalf("SetNodeMetadata cross-library error = %v, want ownership", err)
	}
	if err := own.scope.SetNodeMetadataValue(otherA, "key", "value"); !errors.Is(err, ErrOwnership) {
		t.Fatalf("SetNodeMetadataValue cross-library error = %v, want ownership", err)
	}
	if err := own.scope.DeleteNodeMetadataValue(otherA, "key"); !errors.Is(err, ErrOwnership) {
		t.Fatalf("DeleteNodeMetadataValue cross-library error = %v, want ownership", err)
	}
	if err := own.scope.CanSetNodePorts(otherA, nil, nil); !errors.Is(err, ErrOwnership) {
		t.Fatalf("CanSetNodePorts cross-library error = %v, want ownership", err)
	}
	if err := own.scope.SetNodePorts(otherA, nil, nil); !errors.Is(err, ErrOwnership) {
		t.Fatalf("SetNodePorts cross-library error = %v, want ownership", err)
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
	if err := own.scope.CanCreateLink(
		FullPortID{Node: otherA, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: ownA, Port: PortID{Number: 1, Kind: OutputPort}},
		testType,
	); !errors.Is(err, ErrOwnership) {
		t.Fatalf("CanCreateLink cross-library error = %v, want ownership", err)
	}
	if err := own.scope.SetLinkWaypoints(otherLink, []string{"p1"}); !errors.Is(err, ErrOwnership) {
		t.Fatalf("SetLinkWaypoints cross-library error = %v, want ownership", err)
	}
	if err := own.scope.CanSetLinkWaypoints(otherLink); !errors.Is(err, ErrOwnership) {
		t.Fatalf("CanSetLinkWaypoints cross-library error = %v, want ownership", err)
	}
	if err := own.scope.DeleteLink(otherLink); !errors.Is(err, ErrOwnership) {
		t.Fatalf("DeleteLink cross-library error = %v, want ownership", err)
	}
	if err := own.scope.CanDeleteLink(otherLink); !errors.Is(err, ErrOwnership) {
		t.Fatalf("CanDeleteLink cross-library error = %v, want ownership", err)
	}
	if err := own.scope.SetNodeState(ownA, NodeState{DisplayName: "owned node"}); err != nil {
		t.Fatal(err)
	}
	if err := own.scope.SetNodeCoordinate(ownA, "x:1"); err != nil {
		t.Fatal(err)
	}
	nextInputs := []PortSpec{{
		ID:        PortID{Number: 1, Kind: InputPort},
		Name:      "in",
		Direction: InputPort,
		FixedType: testType,
		Multiple:  true,
	}}
	nextOutputs := []PortSpec{{
		ID:        PortID{Number: 1, Kind: OutputPort},
		Name:      "out",
		Direction: OutputPort,
		FixedType: testType,
	}}
	if err := own.scope.CanSetNodePorts(ownA, nextInputs, nextOutputs); err != nil {
		t.Fatal(err)
	}
	if err := own.scope.SetNodePorts(ownA, nextInputs, nextOutputs); err != nil {
		t.Fatal(err)
	}
	if got, ok := w.Node(ownA); !ok || got.Dynamic.DisplayName != "owned node" || got.Dynamic.Coordinate != "x:1" || len(got.Inputs) != 1 || !got.Inputs[0].Multiple {
		t.Fatalf("owned node after scoped updates = %#v, ok %v", got, ok)
	}
	if err := own.scope.DeleteLink(ownLink); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryScopeSetLinkWaypoints(t *testing.T) {
	w := NewWorkspace()
	lib := &captureScopeLibrary{name: "example.com", classes: []ClassSpec{scopedTestClass("example.com", "Source")}}
	if err := w.RegisterLibrary(lib); err != nil {
		t.Fatal(err)
	}
	output, err := lib.scope.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	input, err := lib.scope.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	link, err := lib.scope.CreateLink(
		FullPortID{Node: input, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: output, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	waypoints := []string{"p1", "p2"}
	if err := lib.scope.SetLinkWaypoints(link, waypoints); err != nil {
		t.Fatal(err)
	}
	waypoints[0] = "mutated"
	snap, ok := w.Link(link)
	if !ok {
		t.Fatal("link should exist")
	}
	if len(snap.Waypoints) != 2 || snap.Waypoints[0] != "p1" || snap.Waypoints[1] != "p2" {
		t.Fatalf("waypoints = %#v", snap.Waypoints)
	}
	snap.Waypoints[0] = "mutated"
	next, _ := w.Link(link)
	if next.Waypoints[0] != "p1" {
		t.Fatalf("link snapshot leaked mutable waypoints: %#v", next.Waypoints)
	}
}

func TestSetNodeMetadataUpdatesSnapshotsSaveAndCopy(t *testing.T) {
	w, _ := testWorkspace(t)
	node, err := w.CreateNode("example.com/Source", NodeOptions{
		State: NodeState{
			DisplayName: "node",
			Private:     map[string]string{"private": "preserved"},
		},
		UseState: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	metadata := map[string]string{"key": "value"}
	if err := w.SetNodeMetadata(node, metadata); err != nil {
		t.Fatal(err)
	}
	metadata["key"] = "mutated"

	snapshot, ok := w.Node(node)
	if !ok {
		t.Fatal("node should exist")
	}
	if snapshot.Dynamic.Metadata["key"] != "value" || snapshot.Dynamic.DisplayName != "node" {
		t.Fatalf("snapshot state = %#v", snapshot.Dynamic)
	}
	if snapshot.Dynamic.Private.(map[string]string)["private"] != "preserved" {
		t.Fatalf("private state was not preserved: %#v", snapshot.Dynamic.Private)
	}
	snapshot.Dynamic.Metadata["key"] = "mutated"
	next, _ := w.Node(node)
	if next.Dynamic.Metadata["key"] != "value" {
		t.Fatalf("metadata snapshot leaked mutable state: %#v", next.Dynamic.Metadata)
	}

	saved := w.Save()
	if len(saved.Nodes) != 1 || saved.Nodes[0].State.Metadata["key"] != "value" {
		t.Fatalf("saved metadata = %#v", saved.Nodes)
	}
	clip, err := w.Copy([]NodeID{node})
	if err != nil {
		t.Fatal(err)
	}
	if len(clip.Nodes) != 1 || clip.Nodes[0].State.Metadata["key"] != "value" {
		t.Fatalf("clipboard metadata = %#v", clip.Nodes)
	}
	if err := w.SetNodeMetadata(node, nil); err != nil {
		t.Fatal(err)
	}
	cleared, _ := w.Node(node)
	if cleared.Dynamic.Metadata != nil {
		t.Fatalf("metadata = %#v, want nil", cleared.Dynamic.Metadata)
	}
	if err := w.SetNodeMetadataValue(node, "one", "1"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetNodeMetadataValue(node, "two", "2"); err != nil {
		t.Fatal(err)
	}
	if err := w.DeleteNodeMetadataValue(node, "one"); err != nil {
		t.Fatal(err)
	}
	edited, _ := w.Node(node)
	if len(edited.Dynamic.Metadata) != 1 || edited.Dynamic.Metadata["two"] != "2" {
		t.Fatalf("metadata after single-key edits = %#v", edited.Dynamic.Metadata)
	}
	if err := w.DeleteNodeMetadataValue(node, "two"); err != nil {
		t.Fatal(err)
	}
	edited, _ = w.Node(node)
	if edited.Dynamic.Metadata != nil {
		t.Fatalf("metadata after deleting last key = %#v, want nil", edited.Dynamic.Metadata)
	}
	if err := w.SetNodeMetadataValue(999, "key", "value"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetNodeMetadataValue missing node error = %v, want not found", err)
	}
	if err := w.DeleteNodeMetadataValue(999, "key"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteNodeMetadataValue missing node error = %v, want not found", err)
	}
}

func TestEphemeralNodeMessages(t *testing.T) {
	scopes := map[NodeID]NodeScope{}
	class := ClassSpec{
		Name:    "example.com/Scoped",
		Runtime: &nodeScopeClass{scopes: scopes},
	}
	w := NewWorkspace()
	lib := &captureScopeLibrary{name: "example.com", classes: []ClassSpec{class}}
	other := &captureScopeLibrary{name: "other.com", classes: []ClassSpec{scopedTestClass("other.com", "Source")}}
	if err := w.RegisterLibrary(lib); err != nil {
		t.Fatal(err)
	}
	if err := w.RegisterLibrary(other); err != nil {
		t.Fatal(err)
	}
	node, err := lib.scope.CreateNode("example.com/Scoped", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	otherNode, err := other.scope.CreateNode("other.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sub := w.WatchMessages(16)
	defer sub.Close()

	note, err := w.AddNodeMessage(node, MessageNote, "hello")
	if err != nil {
		t.Fatal(err)
	}
	warn, err := lib.scope.AddNodeMessage(node, MessageWarn, "careful")
	if err != nil {
		t.Fatal(err)
	}
	errID, err := scopes[node].AddMessage(MessageErr, "failed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.AddNodeMessage(node, MessageType("info"), "bad"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("AddNodeMessage invalid type error = %v, want invalid name", err)
	}
	if _, err := lib.scope.AddNodeMessage(otherNode, MessageNote, "cross"); !errors.Is(err, ErrOwnership) {
		t.Fatalf("scoped AddNodeMessage cross-library error = %v, want ownership", err)
	}

	for _, want := range []struct {
		kind MessageEventKind
		id   MessageID
		typ  MessageType
		text string
	}{
		{MessageAdded, note, MessageNote, "hello"},
		{MessageAdded, warn, MessageWarn, "careful"},
		{MessageAdded, errID, MessageErr, "failed"},
	} {
		got := <-sub.Events()
		if got.Kind != want.kind || got.Message.ID != want.id || got.Message.Node != node || got.Message.Type != want.typ || got.Message.Text != want.text {
			t.Fatalf("message event = %#v, want kind %s id %d type %s text %q", got, want.kind, want.id, want.typ, want.text)
		}
	}

	snap, ok := w.Node(node)
	if !ok {
		t.Fatal("node should exist")
	}
	if len(snap.Messages) != 3 || snap.Messages[0].ID != note || snap.Messages[1].ID != warn || snap.Messages[2].ID != errID {
		t.Fatalf("node snapshot messages = %#v", snap.Messages)
	}
	messages := w.NodeMessages(node)
	if len(messages) != 3 || messages[0].Text != "hello" || messages[2].Type != MessageErr {
		t.Fatalf("node messages = %#v", messages)
	}

	if err := scopes[node].RemoveMessage(errID); err != nil {
		t.Fatal(err)
	}
	if got := <-sub.Events(); got.Kind != MessageRemoved || got.Message.ID != errID || got.Message.Text != "failed" {
		t.Fatalf("remove event = %#v", got)
	}
	if err := lib.scope.RemoveNodeMessage(otherNode, note); !errors.Is(err, ErrOwnership) {
		t.Fatalf("scoped RemoveNodeMessage cross-library error = %v, want ownership", err)
	}

	saved := w.Save()
	clip, err := w.Copy([]NodeID{node})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Nodes) != 2 || len(clip.Nodes) != 1 {
		t.Fatalf("unexpected save/copy sizes: saved=%d copied=%d", len(saved.Nodes), len(clip.Nodes))
	}
	if err := w.Restore(saved); err != nil {
		t.Fatal(err)
	}
	if got := <-sub.Events(); got.Kind != MessageRemoved || got.Message.ID != note {
		t.Fatalf("restore removal event = %#v, want note removed", got)
	}
	if got := <-sub.Events(); got.Kind != MessageRemoved || got.Message.ID != warn {
		t.Fatalf("restore removal event = %#v, want warn removed", got)
	}
	if got := w.NodeMessages(node); len(got) != 0 {
		t.Fatalf("messages after restore = %#v, want none", got)
	}
}

func TestDeletingNodeRemovesEphemeralMessages(t *testing.T) {
	w, _ := testWorkspace(t)
	node, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sub := w.WatchMessages(8)
	defer sub.Close()
	message, err := w.AddNodeMessage(node, MessageWarn, "delete me")
	if err != nil {
		t.Fatal(err)
	}
	<-sub.Events()
	if err := w.DeleteNode(node); err != nil {
		t.Fatal(err)
	}
	if got := <-sub.Events(); got.Kind != MessageRemoved || got.Message.ID != message {
		t.Fatalf("delete removal event = %#v", got)
	}
	if got := w.NodeMessages(node); len(got) != 0 {
		t.Fatalf("messages after delete = %#v, want none", got)
	}
}

type menuRuntimeClass struct {
	runtime NodeRuntime
	scopes  map[NodeID]NodeScope
}

func (c menuRuntimeClass) InitNode(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
	if c.scopes != nil {
		c.scopes[ctx.ID] = ctx.Node
	}
	if c.runtime != nil {
		return c.runtime, nil
	}
	return struct{}{}, nil
}

type menuHookRuntime struct {
	updates []MenuStateUpdate
	buttons []MenuButtonRef
}

func (r *menuHookRuntime) ApplyMenuUpdate(update MenuStateUpdate) (MenuStateUpdate, error) {
	r.updates = append(r.updates, update)
	return MenuStateUpdate{
		Version: update.Version,
		Fields: []MenuFieldUpdate{{
			Block: "default",
			Field: "name",
			Value: "normalized",
		}},
	}, nil
}

func (r *menuHookRuntime) TriggerMenuButton(ref MenuButtonRef) error {
	r.buttons = append(r.buttons, ref)
	return nil
}

func testMenu() NodeMenu {
	return NodeMenu{
		Committable: true,
		Blocks: []MenuBlock{{
			ID: "default",
			Fields: []MenuField{
				{ID: "status", Kind: MenuFieldReadOnly, Value: "ready"},
				{ID: "name", Kind: MenuFieldString, Value: "initial", Options: []MenuOption{{Value: "initial"}, {Value: "normalized"}}},
				{ID: "count", Kind: MenuFieldInt64, Value: int64(2)},
				{ID: "ratio", Kind: MenuFieldFloat64, Value: 1.5},
				{ID: "enabled", Kind: MenuFieldBool, Value: true, Render: MenuRenderCheckbox},
			},
			Buttons: []MenuButton{{ID: "refresh", Label: "Refresh"}},
			Repeats: []MenuRepeat{{
				ID: "hosts",
				Template: []MenuField{
					{ID: "host", Kind: MenuFieldString, Value: ""},
					{ID: "ip", Kind: MenuFieldString, Value: ""},
				},
				Items: []MenuRepeatItem{{
					ID: "one",
					Fields: []MenuField{
						{ID: "host", Value: "alpha"},
						{ID: "ip", Value: "10.0.0.1"},
					},
				}},
			}},
		}},
	}
}

func TestNodeMenusUpdateHooksWatchersAndSnapshots(t *testing.T) {
	hook := &menuHookRuntime{}
	class := ClassSpec{Name: "example.com/Menu", Runtime: menuRuntimeClass{runtime: hook}}
	w := NewWorkspace()
	lib := &captureScopeLibrary{name: "example.com", classes: []ClassSpec{class}}
	other := &captureScopeLibrary{name: "other.com", classes: []ClassSpec{scopedTestClass("other.com", "Source")}}
	if err := w.RegisterLibrary(lib); err != nil {
		t.Fatal(err)
	}
	if err := w.RegisterLibrary(other); err != nil {
		t.Fatal(err)
	}
	node, err := lib.scope.CreateNode("example.com/Menu", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	otherNode, err := other.scope.CreateNode("other.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sub := w.WatchMenus(16)
	defer sub.Close()

	if err := lib.scope.SetNodeMenu(node, testMenu()); err != nil {
		t.Fatal(err)
	}
	replaced := <-sub.Events()
	if replaced.Kind != MenuReplaced || replaced.Node != node || replaced.Menu == nil || replaced.Menu.Version != 1 {
		t.Fatalf("replace event = %#v", replaced)
	}
	if err := lib.scope.SetNodeMenu(otherNode, testMenu()); !errors.Is(err, ErrOwnership) {
		t.Fatalf("scoped SetNodeMenu cross-library error = %v, want ownership", err)
	}

	menu, ok := w.NodeMenu(node)
	if !ok || menu.Version != 1 || !menu.Committable {
		t.Fatalf("NodeMenu = %#v ok=%v", menu, ok)
	}
	menu.Blocks[0].Fields[1].Value = "mutated"
	snap, ok := w.Node(node)
	if !ok || snap.Menu == nil || snap.Menu.Blocks[0].Fields[1].Value != "initial" {
		t.Fatalf("menu snapshot leaked mutable state: %#v", snap.Menu)
	}

	updated, err := w.UpdateNodeMenuState(node, MenuStateUpdate{
		Version: 1,
		Fields:  []MenuFieldUpdate{{Block: "default", Field: "name", Value: "initial"}},
		Repeats: []MenuRepeatUpdate{{
			Block:  "default",
			Repeat: "hosts",
			Items: []MenuRepeatItemState{{
				ID:     "two",
				Fields: map[string]any{"host": "beta", "ip": "10.0.0.2"},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 || updated.Blocks[0].Fields[1].Value != "normalized" {
		t.Fatalf("updated menu = %#v", updated)
	}
	if len(hook.updates) != 1 || hook.updates[0].Version != 1 {
		t.Fatalf("menu update hook calls = %#v", hook.updates)
	}
	changed := <-sub.Events()
	if changed.Kind != MenuStateChanged || changed.Menu == nil || changed.Menu.Version != 2 {
		t.Fatalf("state event = %#v", changed)
	}

	if err := w.TriggerNodeMenuButton(node, MenuButtonRef{Block: "default", Button: "refresh"}); err != nil {
		t.Fatal(err)
	}
	if len(hook.buttons) != 1 || hook.buttons[0].Button != "refresh" {
		t.Fatalf("button hook calls = %#v", hook.buttons)
	}
	triggered := <-sub.Events()
	if triggered.Kind != MenuButtonTriggered || triggered.Button.Button != "refresh" {
		t.Fatalf("button event = %#v", triggered)
	}

	if _, err := w.UpdateNodeMenuState(node, MenuStateUpdate{Version: 1, Fields: []MenuFieldUpdate{{Block: "default", Field: "name", Value: "initial"}}}); !errors.Is(err, ErrStaleMenu) {
		t.Fatalf("stale update error = %v, want stale menu", err)
	}
	if _, err := w.UpdateNodeMenuState(node, MenuStateUpdate{Version: 2, Fields: []MenuFieldUpdate{{Block: "default", Field: "status", Value: "bad"}}}); !errors.Is(err, ErrInvalidMenu) {
		t.Fatalf("read-only update error = %v, want invalid menu", err)
	}
	if _, err := w.UpdateNodeMenuState(node, MenuStateUpdate{Version: 2, Fields: []MenuFieldUpdate{{Block: "default", Field: "count", Value: "bad"}}}); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("type mismatch update error = %v, want type mismatch", err)
	}

	if err := w.ClearNodeMenu(node); err != nil {
		t.Fatal(err)
	}
	cleared := <-sub.Events()
	if cleared.Kind != MenuCleared || cleared.Menu == nil || cleared.Menu.Version != 2 {
		t.Fatalf("clear event = %#v", cleared)
	}
	if _, ok := w.NodeMenu(node); ok {
		t.Fatal("menu should be cleared")
	}
}

func TestNodeMenusNodeScopeMarshalAndNonPersistence(t *testing.T) {
	scopes := map[NodeID]NodeScope{}
	class := ClassSpec{Name: "example.com/Menu", Runtime: menuRuntimeClass{scopes: scopes}}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{class}}); err != nil {
		t.Fatal(err)
	}
	node, err := w.CreateNode("example.com/Menu", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := scopes[node].SetMenu(testMenu()); err != nil {
		t.Fatal(err)
	}
	if _, err := scopes[node].UpdateMenuState(MenuStateUpdate{Version: 1, Fields: []MenuFieldUpdate{{Block: "default", Field: "enabled", Value: false}}}); err != nil {
		t.Fatal(err)
	}
	menu, ok := w.NodeMenu(node)
	if !ok || menu.Version != 2 || menu.Blocks[0].Fields[4].Value != false {
		t.Fatalf("node-scoped menu = %#v ok=%v", menu, ok)
	}
	text, err := MarshalNodeMenu(menu)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := UnmarshalNodeMenu(text)
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip.Blocks[0].Fields[2].Value != int64(2) {
		t.Fatalf("round-trip int64 value = %#v", roundTrip.Blocks[0].Fields[2].Value)
	}
	if !roundTrip.Committable {
		t.Fatalf("round-trip committable = false, want true")
	}
	updateText, err := MarshalMenuStateUpdate(MenuStateUpdate{Version: 2, Fields: []MenuFieldUpdate{{Block: "default", Field: "count", Value: int64(4)}}})
	if err != nil {
		t.Fatal(err)
	}
	update, err := UnmarshalMenuStateUpdate(updateText)
	if err != nil {
		t.Fatal(err)
	}
	if update.Fields[0].Value != json.Number("4") {
		t.Fatalf("unmarshaled update value = %#v", update.Fields[0].Value)
	}

	saved := w.Save()
	clip, err := w.Copy([]NodeID{node})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Restore(saved); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.NodeMenu(node); ok {
		t.Fatal("menu should not survive restore")
	}
	pasted, _, err := w.Paste(clip)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := w.NodeMenu(pasted[0]); ok {
		t.Fatal("menu should not be copied or pasted")
	}
}

func TestNodeMenuValidation(t *testing.T) {
	w, _ := testWorkspace(t)
	node, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetNodeMenu(node, NodeMenu{Blocks: []MenuBlock{{ID: "dup"}, {ID: "dup"}}}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate block error = %v, want duplicate", err)
	}
	if err := w.SetNodeMenu(node, NodeMenu{Blocks: []MenuBlock{{
		ID:     "default",
		Fields: []MenuField{{ID: "bad", Kind: MenuFieldString, Value: 1}},
	}}}); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("field type error = %v, want type mismatch", err)
	}
	if err := w.SetNodeMenu(node, NodeMenu{Blocks: []MenuBlock{{
		ID: "default",
		Fields: []MenuField{{
			ID:      "choice",
			Kind:    MenuFieldString,
			Value:   "c",
			Options: []MenuOption{{Value: "a"}},
		}},
	}}}); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("option error = %v, want type mismatch", err)
	}
	if err := w.SetNodeMenu(node, NodeMenu{Blocks: []MenuBlock{{
		ID:     "default",
		Fields: []MenuField{{ID: "fn", Kind: MenuFieldReadOnly, Value: func() {}}},
	}}}); !errors.Is(err, ErrInvalidMenu) {
		t.Fatalf("non-json error = %v, want invalid menu", err)
	}
	if err := w.SetNodeMenu(node, NodeMenu{}); err != nil {
		t.Fatal(err)
	}
	menu, ok := w.NodeMenu(node)
	if !ok || len(menu.Blocks) != 1 || menu.Blocks[0].ID != "default" {
		t.Fatalf("default block menu = %#v ok=%v", menu, ok)
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

type nodeScopeClass struct {
	scopes              map[NodeID]NodeScope
	privateInInit       any
	metadataKeyInInit   string
	metadataValueInInit string
}

func (c *nodeScopeClass) InitNode(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
	if c.scopes != nil {
		c.scopes[ctx.ID] = ctx.Node
	}
	if c.privateInInit != nil {
		if err := ctx.Node.SetPrivate(c.privateInInit); err != nil {
			return nil, err
		}
	}
	if c.metadataKeyInInit != "" {
		if err := ctx.Node.SetMetadataValue(c.metadataKeyInInit, c.metadataValueInInit); err != nil {
			return nil, err
		}
	}
	return struct{}{}, nil
}

func TestNodeScopeUpdatesOwnNodeState(t *testing.T) {
	scopes := map[NodeID]NodeScope{}
	class := ClassSpec{
		Name:    "example.com/Scoped",
		Runtime: &nodeScopeClass{scopes: scopes},
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
	node, err := w.CreateNode("example.com/Scoped", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scope := scopes[node]
	if scope == nil {
		t.Fatal("node scope was not provided")
	}
	if scope.ID() != node {
		t.Fatalf("scope ID = %d, want %d", scope.ID(), node)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := scope.SetPrivate(map[string]string{"value": "from-node"}); err != nil {
			t.Errorf("SetPrivate: %v", err)
		}
	}()
	wg.Wait()
	if err := scope.SetCoordinate("x:3"); err != nil {
		t.Fatal(err)
	}
	if err := scope.SetMetadata(map[string]string{"from": "node"}); err != nil {
		t.Fatal(err)
	}
	if err := scope.SetMetadataValue("single", "value"); err != nil {
		t.Fatal(err)
	}
	if err := scope.DeleteMetadataValue("from"); err != nil {
		t.Fatal(err)
	}
	nextInputs := []PortSpec{{
		ID:        PortID{Number: 2, Kind: InputPort},
		Name:      "next",
		Direction: InputPort,
		FixedType: testType,
	}}
	if err := scope.SetPorts(nextInputs, class.Outputs); err != nil {
		t.Fatal(err)
	}
	snap, ok := scope.Snapshot()
	if !ok {
		t.Fatal("node snapshot should exist")
	}
	if snap.Dynamic.Coordinate != "x:3" {
		t.Fatalf("coordinate = %q, want x:3", snap.Dynamic.Coordinate)
	}
	private, ok := snap.Dynamic.Private.(map[string]string)
	if !ok || private["value"] != "from-node" {
		t.Fatalf("private = %#v", snap.Dynamic.Private)
	}
	if len(snap.Dynamic.Metadata) != 1 || snap.Dynamic.Metadata["single"] != "value" {
		t.Fatalf("metadata = %#v", snap.Dynamic.Metadata)
	}
	if len(snap.Inputs) != 1 || snap.Inputs[0].ID.Number != 2 {
		t.Fatalf("inputs = %#v", snap.Inputs)
	}
}

func TestNodeScopeCanUpdatePrivateStateDuringInit(t *testing.T) {
	class := ClassSpec{
		Name: "example.com/Scoped",
		Runtime: &nodeScopeClass{
			privateInInit:       "from-init",
			metadataKeyInInit:   "init",
			metadataValueInInit: "metadata",
		},
	}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{class}}); err != nil {
		t.Fatal(err)
	}
	node, err := w.CreateNode("example.com/Scoped", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	snap, ok := w.Node(node)
	if !ok {
		t.Fatal("node should exist")
	}
	if snap.Dynamic.Private != "from-init" {
		t.Fatalf("private = %#v, want from-init", snap.Dynamic.Private)
	}
	if snap.Dynamic.Metadata["init"] != "metadata" {
		t.Fatalf("metadata = %#v, want init metadata", snap.Dynamic.Metadata)
	}
}

func TestNodeScopeReportsDeletedAndClosedNodes(t *testing.T) {
	scopes := map[NodeID]NodeScope{}
	class := ClassSpec{
		Name:    "example.com/Scoped",
		Runtime: &nodeScopeClass{scopes: scopes},
	}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{class}}); err != nil {
		t.Fatal(err)
	}
	deleted, err := w.CreateNode("example.com/Scoped", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	closed, err := w.CreateNode("example.com/Scoped", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	deletedScope := scopes[deleted]
	closedScope := scopes[closed]
	if err := w.DeleteNode(deleted); err != nil {
		t.Fatal(err)
	}
	if err := deletedScope.SetPrivate("late"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted SetPrivate error = %v, want ErrNotFound", err)
	}
	if err := deletedScope.SetMetadata(map[string]string{"late": "true"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted SetMetadata error = %v, want ErrNotFound", err)
	}
	if err := deletedScope.SetMetadataValue("late", "true"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted SetMetadataValue error = %v, want ErrNotFound", err)
	}
	if err := deletedScope.DeleteMetadataValue("late"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted DeleteMetadataValue error = %v, want ErrNotFound", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := closedScope.SetPrivate("late"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetPrivate error = %v, want ErrClosed", err)
	}
	if err := closedScope.SetMetadata(map[string]string{"late": "true"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetMetadata error = %v, want ErrClosed", err)
	}
	if err := closedScope.SetMetadataValue("late", "true"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetMetadataValue error = %v, want ErrClosed", err)
	}
	if err := closedScope.DeleteMetadataValue("late"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed DeleteMetadataValue error = %v, want ErrClosed", err)
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

func TestCanSetNodePortsValidatesWithoutMutation(t *testing.T) {
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

	badInputs := append([]PortSpec(nil), class.Inputs...)
	badInputs[0].FixedType = "example.com/float"
	if err := w.CanSetNodePorts(b, badInputs, class.Outputs); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("CanSetNodePorts error = %v, want type mismatch", err)
	}
	snap, ok := w.Node(b)
	if !ok {
		t.Fatal("node should exist")
	}
	if snap.Inputs[0].FixedType != testType {
		t.Fatalf("CanSetNodePorts mutated inputs: %#v", snap.Inputs)
	}

	nextInputs := []PortSpec{{
		ID:            PortID{Number: 1, Kind: InputPort},
		Name:          "in",
		Direction:     InputPort,
		AcceptedTypes: []string{testType, "example.com/float"},
	}}
	if err := w.CanSetNodePorts(b, nextInputs, class.Outputs); err != nil {
		t.Fatalf("CanSetNodePorts compatible update: %v", err)
	}
	snap, ok = w.Node(b)
	if !ok {
		t.Fatal("node should exist")
	}
	if snap.Inputs[0].FixedType != testType || len(snap.Inputs[0].AcceptedTypes) != 0 {
		t.Fatalf("successful CanSetNodePorts should not mutate inputs: %#v", snap.Inputs)
	}
}

func TestControllerValidationQueriesDoNotMutate(t *testing.T) {
	w, _ := testWorkspace(t)
	if err := w.CanCreateNode("example.com/Source"); err != nil {
		t.Fatalf("CanCreateNode active class: %v", err)
	}
	if err := w.CanCreateNode("example.com/Missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CanCreateNode missing class error = %v, want not found", err)
	}
	beforeCreate := w.Snapshot()
	if len(beforeCreate.Nodes) != 0 {
		t.Fatalf("CanCreateNode mutated nodes: %#v", beforeCreate.Nodes)
	}

	a, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	input := FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}}
	output := FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}}
	if err := w.CanCreateLink(input, output, testType); err != nil {
		t.Fatalf("CanCreateLink valid link: %v", err)
	}
	beforeLink := w.Snapshot()
	if len(beforeLink.Links) != 0 {
		t.Fatalf("CanCreateLink mutated links: %#v", beforeLink.Links)
	}
	link, err := w.CreateLink(input, output, LinkOptions{Waypoints: []string{"p1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.CanCreateLink(input, FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}}, testType); !errors.Is(err, ErrMultiplicity) {
		t.Fatalf("CanCreateLink duplicate input error = %v, want multiplicity", err)
	}
	if err := w.CanSetLinkWaypoints(link); err != nil {
		t.Fatalf("CanSetLinkWaypoints existing link: %v", err)
	}
	if err := w.CanSetLinkWaypoints(999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CanSetLinkWaypoints missing link error = %v, want not found", err)
	}
	snap, ok := w.Link(link)
	if !ok {
		t.Fatal("link should exist")
	}
	if len(snap.Waypoints) != 1 || snap.Waypoints[0] != "p1" {
		t.Fatalf("CanSetLinkWaypoints mutated waypoints: %#v", snap.Waypoints)
	}
	if err := w.CanDeleteLink(link); err != nil {
		t.Fatalf("CanDeleteLink existing link: %v", err)
	}
	if _, ok := w.Link(link); !ok {
		t.Fatal("CanDeleteLink removed link")
	}
	if err := w.CanDeleteNode(a); err != nil {
		t.Fatalf("CanDeleteNode existing node: %v", err)
	}
	if _, ok := w.Node(a); !ok {
		t.Fatal("CanDeleteNode removed node")
	}
	if err := w.CanDeleteNode(999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CanDeleteNode missing node error = %v, want not found", err)
	}
}

func TestValidationQueriesReportClosedWorkspace(t *testing.T) {
	w, _ := testWorkspace(t)
	node, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.CanCreateNode("example.com/Source"); !errors.Is(err, ErrClosed) {
		t.Fatalf("CanCreateNode closed error = %v, want closed", err)
	}
	if err := w.CanDeleteNode(node); !errors.Is(err, ErrClosed) {
		t.Fatalf("CanDeleteNode closed error = %v, want closed", err)
	}
	if err := w.CanCreateLink(
		FullPortID{Node: node, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: node, Port: PortID{Number: 1, Kind: OutputPort}},
		testType,
	); !errors.Is(err, ErrClosed) {
		t.Fatalf("CanCreateLink closed error = %v, want closed", err)
	}
	if err := w.CanSetNodePorts(node, nil, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("CanSetNodePorts closed error = %v, want closed", err)
	}
	if err := w.CanSetLinkWaypoints(1); !errors.Is(err, ErrClosed) {
		t.Fatalf("CanSetLinkWaypoints closed error = %v, want closed", err)
	}
	if err := w.CanDeleteLink(1); !errors.Is(err, ErrClosed) {
		t.Fatalf("CanDeleteLink closed error = %v, want closed", err)
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

func TestPasteRejectsInvalidSavedPortsAndPreservesWorkspace(t *testing.T) {
	w, _ := testWorkspace(t)
	before := w.Save()
	clip := Clipboard{Nodes: []SaveNode{{
		ID:    "1N",
		Class: "example.com/Source",
		Inputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: OutputPort},
			Name:      "wrong",
			Direction: InputPort,
			FixedType: testType,
		}},
	}}}
	if _, _, err := w.Paste(clip); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("Paste error = %v, want invalid port", err)
	}
	after := w.Save()
	if fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("paste should preserve workspace, got %#v want %#v", after, before)
	}
}

func TestSaveConfigRestoreConfigRoundTrip(t *testing.T) {
	w, _ := testWorkspace(t)
	a, _ := w.CreateNode("example.com/Source", NodeOptions{State: NodeState{
		Coordinate: "x:1",
		Private: map[string]any{
			"value": "persisted",
			"count": 2,
		},
	}})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{State: NodeState{Coordinate: "x:2"}})
	link, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{Waypoints: []string{"p1"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := w.SaveConfig()
	if err != nil {
		t.Fatal(err)
	}
	got, err := cfg.Get(configer.Path{"nodes", "0", "state", "Private", "value"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "persisted" {
		t.Fatalf("config private value = %#v, want persisted", got)
	}
	linkValue, err := cfg.Get(configer.Path{"nodes", "1", "ports", "0", "Links", "1L:2N1i:1N1o", "type"})
	if err != nil {
		t.Fatal(err)
	}
	if linkValue != testType {
		t.Fatalf("config link type = %#v, want %q", linkValue, testType)
	}
	if err := cfg.Set(configer.Path{"nodes", "0", "state", "Private", "value"}, "mutated-copy"); err != nil {
		t.Fatal(err)
	}
	snap, ok := w.Node(a)
	if !ok {
		t.Fatal("node should exist")
	}
	if snap.Dynamic.Private.(map[string]any)["value"] != "persisted" {
		t.Fatalf("SaveConfig exposed workspace private state: %#v", snap.Dynamic.Private)
	}

	restored, _ := testWorkspace(t)
	if err := restored.RestoreConfig(cfg); err != nil {
		t.Fatal(err)
	}
	restoredNode, ok := restored.Node(a)
	if !ok {
		t.Fatal("restored node should exist")
	}
	private, ok := restoredNode.Dynamic.Private.(map[string]any)
	if !ok || private["value"] != "mutated-copy" || private["count"] != float64(2) {
		t.Fatalf("restored private = %#v", restoredNode.Dynamic.Private)
	}
	restoredLink, ok := restored.Link(link)
	if !ok || restoredLink.Waypoints[0] != "p1" {
		t.Fatalf("restored link = %#v, ok %v", restoredLink, ok)
	}
}

func TestSaveToClearConfigUsesCompactShapeAndRestores(t *testing.T) {
	w, _ := testWorkspace(t)
	a, _ := w.CreateNode("example.com/Source", NodeOptions{State: NodeState{
		DisplayName: "source",
		Coordinate:  "x:1",
		Private:     map[string]any{"value": "persisted"},
	}})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{State: NodeState{DisplayName: "sink"}})
	if _, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{Waypoints: []string{"p1"}},
	); err != nil {
		t.Fatal(err)
	}
	cfg := configer.NewMemory(nil)
	if err := w.SaveToConfig(cfg); err != nil {
		t.Fatal(err)
	}
	snapshot := cfg.Snapshot().(map[string]any)
	if _, ok := snapshot["nextNode"]; ok {
		t.Fatalf("compact config stored nextNode: %#v", snapshot)
	}
	if _, ok := snapshot["links"]; ok {
		t.Fatalf("compact config stored root links: %#v", snapshot)
	}
	nodes := snapshot["nodes"].([]any)
	first := nodes[0].(map[string]any)
	if _, ok := first["inputs"]; ok {
		t.Fatalf("compact config stored inputs: %#v", first)
	}
	if _, ok := first["outputs"]; ok {
		t.Fatalf("compact config stored outputs: %#v", first)
	}
	ports := first["ports"].([]any)
	if got := ports[0].(map[string]any)["id"]; got != "1i" {
		t.Fatalf("first port id = %#v, want 1i", got)
	}

	restored, _ := testWorkspace(t)
	if err := restored.RestoreConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if got, ok := restored.Node(a); !ok || got.Dynamic.Private.(map[string]any)["value"] != "persisted" {
		t.Fatalf("restored node = %#v ok %v", got, ok)
	}
	if got := restored.Snapshot(); len(got.Links) != 1 || got.Links[0].Waypoints[0] != "p1" {
		t.Fatalf("restored links = %#v", got.Links)
	}
}

func TestRestoreConfigAcceptsLegacySaveDataShape(t *testing.T) {
	w, _ := testWorkspace(t)
	legacy := configer.NewMemory(SaveData{
		NextNode: 99,
		NextLink: 99,
		Nodes: []SaveNode{
			{ID: "1N", Class: "example.com/Source"},
			{ID: "2N", Class: "example.com/Source"},
		},
		Links: []SaveLink{{Name: "1L:2N1i:1N1o", Type: testType}},
	})
	if err := w.RestoreConfig(legacy); err != nil {
		t.Fatal(err)
	}
	if got := w.Snapshot(); len(got.Links) != 1 {
		t.Fatalf("legacy restored links = %#v", got.Links)
	}
}

type commentMemory struct {
	*configer.Memory
	comments map[string]string
}

func newCommentMemory(value any) *commentMemory {
	return &commentMemory{Memory: configer.NewMemory(value), comments: map[string]string{}}
}

func (c *commentMemory) SetComment(path configer.Path, comment string) error {
	c.comments[strings.Join(path, "\x00")] = comment
	return nil
}

func (c *commentMemory) GetComment(path configer.Path) (string, error) {
	comment, ok := c.comments[strings.Join(path, "\x00")]
	if !ok {
		return "", configer.ErrNotFound
	}
	return comment, nil
}

func TestSaveToPreexistingCommentedConfigPreservesUsefulComments(t *testing.T) {
	w, _ := testWorkspace(t)
	if _, err := w.CreateNode("example.com/Source", NodeOptions{}); err != nil {
		t.Fatal(err)
	}
	cfg := newCommentMemory(map[string]any{
		"nextNode": float64(99),
		"nodes": []any{map[string]any{
			"id": "old",
			"state": map[string]any{
				"Description": "",
			},
			"ports": []any{map[string]any{
				"id":       "1i",
				"Multiple": false,
			}},
		}},
		"links": []any{map[string]any{"name": "old"}},
	})
	if err := cfg.SetComment(configer.Path{"nextNode"}, "old derived ID"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetComment(configer.Path{"nodes", "0", "state", "Description"}, "document why empty"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetComment(configer.Path{"nodes", "0", "ports", "0", "Multiple"}, "document single input"); err != nil {
		t.Fatal(err)
	}
	if err := w.SaveToConfig(cfg); err != nil {
		t.Fatal(err)
	}
	snapshot := cfg.Snapshot().(map[string]any)
	if _, ok := snapshot["nextNode"]; ok {
		t.Fatalf("derived nextNode was preserved: %#v", snapshot)
	}
	if _, ok := snapshot["links"]; ok {
		t.Fatalf("legacy root links were preserved: %#v", snapshot)
	}
	node := snapshot["nodes"].([]any)[0].(map[string]any)
	state := node["state"].(map[string]any)
	if got, ok := state["Description"]; !ok || got != "" {
		t.Fatalf("commented void Description = %#v ok %v", got, ok)
	}
	port := node["ports"].([]any)[0].(map[string]any)
	if got, ok := port["Multiple"]; !ok || got != false {
		t.Fatalf("commented void Multiple = %#v ok %v", got, ok)
	}
	if comment, err := cfg.GetComment(configer.Path{"nodes", "0", "state", "Description"}); err != nil || comment == "" {
		t.Fatalf("Description comment = %q err %v", comment, err)
	}
}

func TestRestoreSkipsBrokenPersistedLinks(t *testing.T) {
	w, _ := testWorkspace(t)
	data := SaveData{
		Nodes: []SaveNode{
			{ID: "1N", Class: "example.com/Source"},
			{ID: "2N", Class: "example.com/Source"},
		},
		Links: []SaveLink{
			{Name: "1L:2N1i:1N1o", Type: testType},
			{Name: "2L:3N1i:1N1o", Type: testType},
			{Name: "3L:2N9i:1N1o", Type: testType},
		},
	}
	if err := w.Restore(data); err != nil {
		t.Fatal(err)
	}
	snapshot := w.Snapshot()
	if len(snapshot.Links) != 1 {
		t.Fatalf("links = %#v, want only the valid persisted link", snapshot.Links)
	}
	if snapshot.Links[0].ID != 1 || snapshot.Links[0].State != StateActive {
		t.Fatalf("link = %#v, want active 1L", snapshot.Links[0])
	}
}

func TestRestoreRejectsInvalidPersistedLinkConstraintsAndRollsBack(t *testing.T) {
	tests := []struct {
		name  string
		links []SaveLink
		err   error
	}{
		{
			name: "type mismatch",
			links: []SaveLink{
				{Name: "1L:2N1i:1N1o", Type: "example.com/float"},
			},
			err: ErrTypeMismatch,
		},
		{
			name: "multiplicity",
			links: []SaveLink{
				{Name: "1L:2N1i:1N1o", Type: testType},
				{Name: "2L:2N1i:3N1o", Type: testType},
			},
			err: ErrMultiplicity,
		},
		{
			name: "cycle",
			links: []SaveLink{
				{Name: "1L:2N1i:1N1o", Type: testType},
				{Name: "2L:1N1i:2N1o", Type: testType},
			},
			err: ErrCycle,
		},
		{
			name: "duplicate link id",
			links: []SaveLink{
				{Name: "1L:2N1i:1N1o", Type: testType},
				{Name: "1L:3N1i:2N1o", Type: testType},
			},
			err: ErrDuplicate,
		},
		{
			name: "duplicate skipped broken link id",
			links: []SaveLink{
				{Name: "1L:9N1i:1N1o", Type: testType},
				{Name: "1L:2N1i:1N1o", Type: testType},
			},
			err: ErrDuplicate,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := testWorkspace(t)
			data := SaveData{
				Nodes: []SaveNode{
					{ID: "1N", Class: "example.com/Source"},
					{ID: "2N", Class: "example.com/Source"},
					{ID: "3N", Class: "example.com/Source"},
				},
				Links: tt.links,
			}
			if err := w.Restore(data); !errors.Is(err, tt.err) {
				t.Fatalf("Restore error = %v, want %v", err, tt.err)
			}
			snapshot := w.Snapshot()
			if len(snapshot.Nodes) != 0 || len(snapshot.Links) != 0 {
				t.Fatalf("restore should roll back model, got %#v", snapshot)
			}
		})
	}
}

func TestRestoreRejectsInvalidPersistedNodesAndPreservesWorkspace(t *testing.T) {
	badInput := PortSpec{
		ID:        PortID{Number: 1, Kind: OutputPort},
		Name:      "wrong",
		Direction: InputPort,
		FixedType: testType,
	}
	tests := []struct {
		name  string
		nodes []SaveNode
		err   error
	}{
		{
			name: "invalid node id",
			nodes: []SaveNode{
				{ID: "0N", Class: "example.com/Source"},
			},
			err: ErrInvalidID,
		},
		{
			name: "duplicate node id",
			nodes: []SaveNode{
				{ID: "1N", Class: "example.com/Source"},
				{ID: "1N", Class: "example.com/Source"},
			},
			err: ErrDuplicate,
		},
		{
			name: "invalid missing class name",
			nodes: []SaveNode{
				{ID: "1N", Class: "example.com/source"},
			},
			err: ErrInvalidName,
		},
		{
			name: "invalid saved port",
			nodes: []SaveNode{
				{ID: "1N", Class: "example.com/Source", Inputs: []PortSpec{badInput}},
			},
			err: ErrInvalidPort,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := testWorkspace(t)
			if _, err := w.CreateNode("example.com/Source", NodeOptions{}); err != nil {
				t.Fatal(err)
			}
			before := w.Save()
			if err := w.Restore(SaveData{Nodes: tt.nodes}); !errors.Is(err, tt.err) {
				t.Fatalf("Restore error = %v, want %v", err, tt.err)
			}
			after := w.Save()
			if fmt.Sprint(after) != fmt.Sprint(before) {
				t.Fatalf("restore should preserve workspace, got %#v want %#v", after, before)
			}
		})
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
	panicLinkInactive  bool
	panicOnDelete      bool
	panicAfterDelete   bool
	panicOnClose       bool
	inspectOnAttach    WorkspaceRO
	inspectOnInactive  WorkspaceRO
	inspectOnDelete    WorkspaceRO
	inspectOnClose     WorkspaceRO
}

type privateHookClass struct {
	nodes      map[NodeID]*privateHookNode
	imports    *[]any
	failImport bool
}

func (c *privateHookClass) InitNode(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
	node := &privateHookNode{imports: c.imports, failImport: c.failImport}
	if c.nodes != nil {
		c.nodes[ctx.ID] = node
	}
	return node, nil
}

type privateHookNode struct {
	exported    any
	imports     *[]any
	failExport  bool
	panicExport bool
	failImport  bool
}

func (n *privateHookNode) ExportPrivateState() (any, error) {
	if n.panicExport {
		panic("export private panic")
	}
	if n.failExport {
		return nil, fmt.Errorf("export private failed")
	}
	return n.exported, nil
}

func (n *privateHookNode) ImportPrivateState(private any) error {
	if n.failImport {
		return fmt.Errorf("import private failed")
	}
	if n.imports != nil {
		*n.imports = append(*n.imports, private)
	}
	return nil
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
	if n.panicLinkInactive {
		panic("link inactive panic")
	}
	*n.log = append(*n.log, fmt.Sprintf("link-inactive:%s:%d:%s", endpoint.Direction, endpoint.Link, reason))
}

func (n *lifecycleNode) BeforeInactive(reason InactiveReason) error {
	if n.panicOnInactive {
		panic("inactive panic")
	}
	if n.inspectOnInactive != nil {
		_ = n.inspectOnInactive.Snapshot()
		*n.log = append(*n.log, "inspect-inactive")
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
	if n.inspectOnDelete != nil {
		_ = n.inspectOnDelete.Snapshot()
		*n.log = append(*n.log, "inspect-delete")
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
	if n.inspectOnClose != nil {
		_ = n.inspectOnClose.Snapshot()
		*n.log = append(*n.log, "inspect-close")
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

func privateHookWorkspace(t *testing.T, classRuntime *privateHookClass, defaultPrivate any) (*Workspace, map[NodeID]*privateHookNode, *[]any) {
	t.Helper()
	imports := []any{}
	nodes := map[NodeID]*privateHookNode{}
	classRuntime.nodes = nodes
	classRuntime.imports = &imports
	class := ClassSpec{
		Name: "example.com/Private",
		Default: NodeState{
			Private: defaultPrivate,
		},
		Runtime: classRuntime,
	}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{class}}); err != nil {
		t.Fatal(err)
	}
	return w, nodes, &imports
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestLifecyclePrivateStateExportAndImportHooks(t *testing.T) {
	w, nodes, imports := privateHookWorkspace(t, &privateHookClass{}, map[string]any{"source": "default"})
	id, err := w.CreateNode("example.com/Private", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(*imports) != 1 {
		t.Fatalf("imports = %#v, want one default import", *imports)
	}
	imported, ok := (*imports)[0].(map[string]any)
	if !ok || imported["source"] != "default" {
		t.Fatalf("imported private = %#v", (*imports)[0])
	}
	nodes[id].exported = map[string]any{"source": "runtime", "items": []any{"a"}}
	if saved := w.Save(); saved.Nodes[0].State.Private.(map[string]any)["source"] != "default" {
		t.Fatalf("Save private = %#v, want stored default", saved.Nodes[0].State.Private)
	}
	saved, err := w.SaveWithRuntimeState()
	if err != nil {
		t.Fatal(err)
	}
	private := saved.Nodes[0].State.Private.(map[string]any)
	if private["source"] != "runtime" {
		t.Fatalf("exported private = %#v", private)
	}
	private["source"] = "changed"
	again, err := w.SaveWithRuntimeState()
	if err != nil {
		t.Fatal(err)
	}
	if again.Nodes[0].State.Private.(map[string]any)["source"] != "runtime" {
		t.Fatalf("exported private was not defensively copied: %#v", again.Nodes[0].State.Private)
	}
	saved = again
	clip, err := w.Copy([]NodeID{id})
	if err != nil {
		t.Fatal(err)
	}
	if clip.Nodes[0].State.Private.(map[string]any)["source"] != "runtime" {
		t.Fatalf("clipboard private = %#v, want runtime export", clip.Nodes[0].State.Private)
	}

	restored, _, restoredImports := privateHookWorkspace(t, &privateHookClass{}, nil)
	if err := restored.Restore(saved); err != nil {
		t.Fatal(err)
	}
	if len(*restoredImports) != 1 {
		t.Fatalf("restored imports = %#v, want one restore import", *restoredImports)
	}
	if (*restoredImports)[0].(map[string]any)["source"] != "runtime" {
		t.Fatalf("restored import = %#v, want runtime export", (*restoredImports)[0])
	}
}

func TestLifecyclePrivateStateHookErrorsRollback(t *testing.T) {
	w, nodes, _ := privateHookWorkspace(t, &privateHookClass{}, nil)
	id, err := w.CreateNode("example.com/Private", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	nodes[id].failExport = true
	if _, err := w.SaveWithRuntimeState(); err == nil {
		t.Fatal("expected export error")
	}
	if _, err := w.Copy([]NodeID{id}); err == nil {
		t.Fatal("expected copy export error")
	}
	nodes[id].failExport = false
	nodes[id].panicExport = true
	if _, err := w.SaveWithRuntimeState(); err == nil {
		t.Fatal("expected export panic error")
	}

	importFailing := &privateHookClass{failImport: true}
	restored, _, _ := privateHookWorkspace(t, importFailing, nil)
	original := restored.Save()
	data := SaveData{Nodes: []SaveNode{{ID: "1N", Class: "example.com/Private", State: NodeState{Private: "persisted"}}}}
	if err := restored.Restore(data); err == nil {
		t.Fatal("expected import error")
	}
	after := restored.Save()
	if fmt.Sprint(after) != fmt.Sprint(original) {
		t.Fatalf("restore should roll back after import error, got %#v want %#v", after, original)
	}
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
	logger := &testLogger{}
	w.logger = logger
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
	if len(logger.errs) == 0 {
		t.Fatal("expected failed attach hook to be logged")
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
	if len(logger.errs) < 2 {
		t.Fatal("expected provider panic to be logged")
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

type flowClass struct {
	runtime NodeRuntime
}

func (c flowClass) InitNode(NodeContext, NodeState, InitMode) (NodeRuntime, error) {
	return c.runtime, nil
}

func flowWorkspace(t *testing.T, classes ...ClassSpec) *Workspace {
	t.Helper()
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: classes}); err != nil {
		t.Fatal(err)
	}
	return w
}

func flowClassSpec(local string, class NodeClass) ClassSpec {
	return ClassSpec{
		Name:    "example.com/" + local,
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
	}
}

type flowPullSource interface {
	Pull() (string, error)
}

type flowPullObject struct {
	source flowPullSource
}

type flowPullOutput struct {
	scope NodeScope
	value string
}

func (n *flowPullOutput) InitNode(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
	n.scope = ctx.Node
	return n, nil
}

func (n *flowPullOutput) Pull() (string, error) {
	if err := n.scope.SetPrivate("pull-called"); err != nil {
		return "", err
	}
	return n.value, nil
}

func (n *flowPullOutput) BeforeLinkAttach(endpoint LinkEndpoint, object any) error {
	if endpoint.Direction != OutputPort {
		return nil
	}
	pull, ok := object.(*flowPullObject)
	if !ok {
		return fmt.Errorf("unexpected pull object %T", object)
	}
	pull.source = n
	return nil
}

func (n *flowPullOutput) AfterLinkAttach(LinkEndpoint, any) {}

type flowPullInput struct {
	object flowPullObject
}

func (n *flowPullInput) LinkObject(LinkEndpoint) (any, error) {
	return &n.object, nil
}

func (n *flowPullInput) Pull() (string, error) {
	if n.object.source == nil {
		return "", fmt.Errorf("pull source not attached")
	}
	return n.object.source.Pull()
}

func TestFlowPullInputCanCallOutputAfterLinkCreation(t *testing.T) {
	output := &flowPullOutput{value: "from-output"}
	input := &flowPullInput{}
	w := flowWorkspace(t,
		flowClassSpec("Source", output),
		flowClassSpec("Sink", flowClass{runtime: input}),
	)
	source, _ := w.CreateNode("example.com/Source", NodeOptions{})
	sink, _ := w.CreateNode("example.com/Sink", NodeOptions{})
	if _, err := w.CreateLink(
		FullPortID{Node: sink, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: source, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	); err != nil {
		t.Fatal(err)
	}
	got, err := input.Pull()
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-output" {
		t.Fatalf("pull value = %q, want from-output", got)
	}
	snap, ok := w.Node(source)
	if !ok || snap.Dynamic.Private != "pull-called" {
		t.Fatalf("source private = %#v, want pull-called", snap.Dynamic.Private)
	}
}

var errFlowDetached = errors.New("flow detached")

type flowBlockingPullOutput struct {
	ready     chan struct{}
	detached  chan struct{}
	readyOnce sync.Once
	closeOnce sync.Once
}

func (n *flowBlockingPullOutput) Pull() (string, error) {
	n.readyOnce.Do(func() { close(n.ready) })
	<-n.detached
	return "", errFlowDetached
}

func (n *flowBlockingPullOutput) BeforeLinkAttach(endpoint LinkEndpoint, object any) error {
	if endpoint.Direction != OutputPort {
		return nil
	}
	pull, ok := object.(*flowPullObject)
	if !ok {
		return fmt.Errorf("unexpected pull object %T", object)
	}
	pull.source = n
	return nil
}

func (n *flowBlockingPullOutput) AfterLinkAttach(LinkEndpoint, any) {}

func (n *flowBlockingPullOutput) BeforeLinkDetach(LinkEndpoint) error {
	return nil
}

func (n *flowBlockingPullOutput) AfterLinkDetach(endpoint LinkEndpoint) {
	if endpoint.Direction == OutputPort {
		n.closeOnce.Do(func() { close(n.detached) })
	}
}

func TestFlowInFlightPullCanBeReleasedByDetachHook(t *testing.T) {
	output := &flowBlockingPullOutput{
		ready:    make(chan struct{}),
		detached: make(chan struct{}),
	}
	input := &flowPullInput{}
	w := flowWorkspace(t,
		flowClassSpec("Source", flowClass{runtime: output}),
		flowClassSpec("Sink", flowClass{runtime: input}),
	)
	source, _ := w.CreateNode("example.com/Source", NodeOptions{})
	sink, _ := w.CreateNode("example.com/Sink", NodeOptions{})
	link, err := w.CreateLink(
		FullPortID{Node: sink, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: source, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := input.Pull()
		done <- err
	}()
	select {
	case <-output.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("pull did not enter the blocking contract")
	}
	if err := w.DeleteLink(link); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, errFlowDetached) {
			t.Fatalf("pull error = %v, want detached", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("detach hook did not release blocked pull")
	}
}

type flowPushReceiver interface {
	Receive(string) error
}

type flowPushInput struct {
	scope  NodeScope
	values []string
}

func (n *flowPushInput) InitNode(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
	n.scope = ctx.Node
	return n, nil
}

func (n *flowPushInput) LinkObject(LinkEndpoint) (any, error) {
	return flowPushReceiver(n), nil
}

func (n *flowPushInput) Receive(value string) error {
	n.values = append(n.values, value)
	return n.scope.SetPrivate(append([]string(nil), n.values...))
}

type flowPushOutput struct {
	receivers []flowPushReceiver
}

func (n *flowPushOutput) BeforeLinkAttach(endpoint LinkEndpoint, object any) error {
	if endpoint.Direction != OutputPort {
		return nil
	}
	receiver, ok := object.(flowPushReceiver)
	if !ok {
		return fmt.Errorf("unexpected push object %T", object)
	}
	n.receivers = append(n.receivers, receiver)
	return nil
}

func (n *flowPushOutput) AfterLinkAttach(LinkEndpoint, any) {}

func (n *flowPushOutput) Push(value string) error {
	for _, receiver := range n.receivers {
		if err := receiver.Receive(value); err != nil {
			return err
		}
	}
	return nil
}

func TestFlowPushOutputCanCallInputAfterLinkCreation(t *testing.T) {
	output := &flowPushOutput{}
	input := &flowPushInput{}
	w := flowWorkspace(t,
		flowClassSpec("Source", flowClass{runtime: output}),
		flowClassSpec("Sink", input),
	)
	source, _ := w.CreateNode("example.com/Source", NodeOptions{})
	sink, _ := w.CreateNode("example.com/Sink", NodeOptions{})
	if _, err := w.CreateLink(
		FullPortID{Node: sink, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: source, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	); err != nil {
		t.Fatal(err)
	}
	if err := output.Push("one"); err != nil {
		t.Fatal(err)
	}
	if err := output.Push("two"); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(input.values) != fmt.Sprint([]string{"one", "two"}) {
		t.Fatalf("input values = %#v", input.values)
	}
	snap, ok := w.Node(sink)
	if !ok || fmt.Sprint(snap.Dynamic.Private) != fmt.Sprint([]string{"one", "two"}) {
		t.Fatalf("sink private = %#v, want pushed values", snap.Dynamic.Private)
	}
}

type flowMixedConn interface {
	Read() (string, error)
}

type flowMixedAcceptor interface {
	NewConn(flowMixedConn) error
}

type flowMixedObject struct {
	conn flowMixedConn
}

func (o *flowMixedObject) NewConn(conn flowMixedConn) error {
	o.conn = conn
	return nil
}

type flowMixedOutput struct {
	values []string
}

func (n *flowMixedOutput) BeforeLinkAttach(endpoint LinkEndpoint, object any) error {
	if endpoint.Direction != OutputPort {
		return nil
	}
	acceptor, ok := object.(flowMixedAcceptor)
	if !ok {
		return fmt.Errorf("unexpected mixed object %T", object)
	}
	return acceptor.NewConn(&flowMixedConnObject{values: append([]string(nil), n.values...)})
}

func (n *flowMixedOutput) AfterLinkAttach(LinkEndpoint, any) {}

type flowMixedConnObject struct {
	values []string
	next   int
}

func (c *flowMixedConnObject) Read() (string, error) {
	if c.next >= len(c.values) {
		return "", fmt.Errorf("empty connection")
	}
	value := c.values[c.next]
	c.next++
	return value, nil
}

type flowMixedInput struct {
	object flowMixedObject
}

func (n *flowMixedInput) LinkObject(LinkEndpoint) (any, error) {
	return &n.object, nil
}

func (n *flowMixedInput) PullConn() (string, error) {
	if n.object.conn == nil {
		return "", fmt.Errorf("mixed connection not attached")
	}
	return n.object.conn.Read()
}

func TestFlowMixedOutputOpensConnectionThatInputPulls(t *testing.T) {
	output := &flowMixedOutput{values: []string{"alpha", "beta"}}
	input := &flowMixedInput{}
	w := flowWorkspace(t,
		flowClassSpec("Source", flowClass{runtime: output}),
		flowClassSpec("Sink", flowClass{runtime: input}),
	)
	source, _ := w.CreateNode("example.com/Source", NodeOptions{})
	sink, _ := w.CreateNode("example.com/Sink", NodeOptions{})
	if _, err := w.CreateLink(
		FullPortID{Node: sink, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: source, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	); err != nil {
		t.Fatal(err)
	}
	first, err := input.PullConn()
	if err != nil {
		t.Fatal(err)
	}
	second, err := input.PullConn()
	if err != nil {
		t.Fatal(err)
	}
	if first != "alpha" || second != "beta" {
		t.Fatalf("mixed values = %q, %q; want alpha, beta", first, second)
	}
}

func TestLifecycleNodeHooksMayReadWorkspace(t *testing.T) {
	w, nodes, log := lifecycleWorkspace(t, &lifecycleClass{})
	deleted, _ := w.CreateNode("example.com/Source", NodeOptions{})
	recalled, _ := w.CreateNode("example.com/Source", NodeOptions{})
	nodes[deleted].inspectOnDelete = w
	nodes[recalled].inspectOnInactive = w

	done := make(chan error, 1)
	go func() { done <- w.DeleteNode(deleted) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DeleteNode hook could not read workspace; likely recursive lock deadlock")
	}

	go func() { done <- w.RecallClass("example.com", "example.com/Source") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RecallClass hook could not read workspace; likely recursive lock deadlock")
	}

	w2, nodes2, log2 := lifecycleWorkspace(t, &lifecycleClass{})
	closed, _ := w2.CreateNode("example.com/Source", NodeOptions{})
	nodes2[closed].inspectOnClose = w2
	go func() { done <- w2.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close hook could not read workspace; likely recursive lock deadlock")
	}

	if !containsString(*log, "inspect-delete") || !containsString(*log, "inspect-inactive") {
		t.Fatalf("log = %#v, want delete and inactive inspections", *log)
	}
	if !containsString(*log2, "inspect-close") {
		t.Fatalf("close log = %#v, want close inspection", *log2)
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

func TestWorkspaceConcurrentReadWriteSmoke(t *testing.T) {
	class := ClassSpec{
		Name: "example.com/Source",
		Inputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: InputPort},
			Name:      "in",
			Direction: InputPort,
			FixedType: testType,
			Multiple:  true,
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
	nodes := make([]NodeID, 6)
	for i := range nodes {
		id, err := w.CreateNode("example.com/Source", NodeOptions{})
		if err != nil {
			t.Fatal(err)
		}
		nodes[i] = id
	}
	stableLink, err := w.CreateLink(
		FullPortID{Node: nodes[1], Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: nodes[0], Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	errs := make(chan error, 64)
	var wg sync.WaitGroup
	for reader := 0; reader < 4; reader++ {
		wg.Add(1)
		go func(reader int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = w.Snapshot()
				_ = w.Save()
				_, _ = w.Node(nodes[i%len(nodes)])
				_, _ = w.Link(stableLink)
				_ = w.CanCreateLink(
					FullPortID{Node: nodes[3], Port: PortID{Number: 1, Kind: InputPort}},
					FullPortID{Node: nodes[2], Port: PortID{Number: 1, Kind: OutputPort}},
					testType,
				)
			}
		}(reader)
	}
	for writer := 0; writer < 4; writer++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				node := nodes[(writer+i)%len(nodes)]
				if err := w.SetNodeCoordinate(node, fmt.Sprintf("w:%d:%d", writer, i)); err != nil {
					errs <- err
					return
				}
				if err := w.SetNodePrivate(node, map[string]any{"writer": writer, "i": i}); err != nil {
					errs <- err
					return
				}
				if err := w.SetLinkWaypoints(stableLink, []string{fmt.Sprintf("p:%d:%d", writer, i)}); err != nil {
					errs <- err
					return
				}
				inputNode := nodes[3+(writer%3)]
				outputNode := nodes[2]
				link, err := w.CreateLink(
					FullPortID{Node: inputNode, Port: PortID{Number: 1, Kind: InputPort}},
					FullPortID{Node: outputNode, Port: PortID{Number: 1, Kind: OutputPort}},
					LinkOptions{},
				)
				if err != nil {
					errs <- err
					return
				}
				if err := w.DeleteLink(link); err != nil {
					errs <- err
					return
				}
			}
		}(writer)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
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
	want := []string{
		"init:restore",
		"init:restore",
		"object",
		"before:input:object:example.com/int",
		"before:output:object:example.com/int",
		"after:input:1:object:example.com/int",
		"after:output:1:object:example.com/int",
	}
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

type closingInitClass struct {
	log    *[]string
	failID NodeID
}

func (c closingInitClass) InitNode(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
	*c.log = append(*c.log, fmt.Sprintf("init:%d", ctx.ID))
	if ctx.ID == c.failID {
		return nil, fmt.Errorf("init failed")
	}
	return closingInitNode{id: ctx.ID, log: c.log}, nil
}

type closingInitNode struct {
	id  NodeID
	log *[]string
}

func (n closingInitNode) Close() error {
	*n.log = append(*n.log, fmt.Sprintf("close:%d", n.id))
	return nil
}

type nonComparableRuntimeClass struct{}

func (nonComparableRuntimeClass) InitNode(NodeContext, NodeState, InitMode) (NodeRuntime, error) {
	return nonComparableRuntime{values: []int{1}}, nil
}

type nonComparableRuntime struct {
	values []int
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

func TestRestoreClosesInitializedRuntimesOnLaterInitError(t *testing.T) {
	log := []string{}
	class := ClassSpec{
		Name:    "example.com/Source",
		Runtime: closingInitClass{log: &log, failID: 2},
	}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{class}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Restore(SaveData{
		Nodes: []SaveNode{
			{ID: "1N", Class: "example.com/Source"},
			{ID: "2N", Class: "example.com/Source"},
		},
	}); err == nil {
		t.Fatal("expected restore init error")
	}
	want := []string{"init:1", "init:2", "close:1"}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", log, want)
	}
	if snapshot := w.Snapshot(); len(snapshot.Nodes) != 0 {
		t.Fatalf("restore should roll back model, got %#v", snapshot)
	}
}

func TestDefineClassReactivatesRestoredNodesAndLinks(t *testing.T) {
	input := PortSpec{
		ID:        PortID{Number: 1, Kind: InputPort},
		Name:      "in",
		Direction: InputPort,
		FixedType: testType,
	}
	output := PortSpec{
		ID:        PortID{Number: 1, Kind: OutputPort},
		Name:      "out",
		Direction: OutputPort,
		FixedType: testType,
	}
	data := SaveData{
		NextNode: 3,
		NextLink: 2,
		Nodes: []SaveNode{
			{ID: "1N", Class: "example.com/Source", Inputs: []PortSpec{input}, Outputs: []PortSpec{output}},
			{ID: "2N", Class: "example.com/Source", Inputs: []PortSpec{input}, Outputs: []PortSpec{output}},
		},
		Links: []SaveLink{{
			Name: FullLinkName{
				Link:   1,
				Input:  FullPortID{Node: 2, Port: input.ID},
				Output: FullPortID{Node: 1, Port: output.ID},
			}.String(),
			Type: testType,
		}},
	}
	runtime := &lifecycleClass{}
	w := NewWorkspace()
	lib := &captureScopeLibrary{name: "example.com"}
	if err := w.RegisterLibrary(lib); err != nil {
		t.Fatal(err)
	}
	if err := w.Restore(data); err != nil {
		t.Fatal(err)
	}
	before := w.Snapshot()
	if before.Nodes[0].State != StateInactive || before.Links[0].State != StateInactive {
		t.Fatalf("before define = %#v", before)
	}
	log := []string{}
	runtime.log = &log
	runtime.nodes = map[NodeID]*lifecycleNode{}
	if err := w.DefineClass("example.com", ClassSpec{
		Name:    "example.com/Source",
		Runtime: runtime,
		Inputs:  []PortSpec{input},
		Outputs: []PortSpec{output},
	}); err != nil {
		t.Fatal(err)
	}
	after := w.Snapshot()
	if after.Nodes[0].State != StateActive || after.Nodes[1].State != StateActive || after.Links[0].State != StateActive {
		t.Fatalf("after define = %#v", after)
	}
	if after.Nodes[0].Library != "example.com" || after.Nodes[1].Library != "example.com" {
		t.Fatalf("recovered node libraries = %#v, want example.com ownership", after.Nodes)
	}
	if err := lib.scope.SetNodeMetadata(after.Nodes[0].ID, map[string]string{"owner": "library"}); err != nil {
		t.Fatalf("library scope should own recovered node: %v", err)
	}
	want := []string{"init:restore", "init:restore"}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", log, want)
	}
}

func TestDefineClassDoesNotReactivateRestoredInvalidLink(t *testing.T) {
	input := PortSpec{
		ID:        PortID{Number: 1, Kind: InputPort},
		Name:      "in",
		Direction: InputPort,
		FixedType: testType,
	}
	output := PortSpec{
		ID:        PortID{Number: 1, Kind: OutputPort},
		Name:      "out",
		Direction: OutputPort,
		FixedType: testType,
	}
	data := SaveData{
		NextNode: 3,
		NextLink: 2,
		Nodes: []SaveNode{
			{ID: "1N", Class: "example.com/Source", Inputs: []PortSpec{input}, Outputs: []PortSpec{output}},
			{ID: "2N", Class: "example.com/Source", Inputs: []PortSpec{input}, Outputs: []PortSpec{output}},
		},
		Links: []SaveLink{{
			Name: FullLinkName{
				Link:   1,
				Input:  FullPortID{Node: 2, Port: input.ID},
				Output: FullPortID{Node: 1, Port: output.ID},
			}.String(),
			Type: testType,
		}},
	}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Restore(data); err != nil {
		t.Fatal(err)
	}

	floatInput := input
	floatInput.FixedType = "example.com/float"
	if err := w.DefineClass("example.com", ClassSpec{
		Name:    "example.com/Source",
		Inputs:  []PortSpec{floatInput},
		Outputs: []PortSpec{output},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := w.Snapshot()
	if len(snapshot.Links) != 0 {
		t.Fatalf("restored incompatible link should be removed during recovery, got %#v", snapshot.Links)
	}
	if snapshot.Nodes[0].State != StateActive || snapshot.Nodes[1].State != StateActive {
		t.Fatalf("nodes should recover even when incompatible link is removed: %#v", snapshot.Nodes)
	}
}

func TestDefineClassPrunesRestoredLinksThatViolateMultiplicity(t *testing.T) {
	input := PortSpec{
		ID:        PortID{Number: 1, Kind: InputPort},
		Name:      "in",
		Direction: InputPort,
		FixedType: testType,
		Multiple:  true,
	}
	singleInput := input
	singleInput.Multiple = false
	output := PortSpec{
		ID:        PortID{Number: 1, Kind: OutputPort},
		Name:      "out",
		Direction: OutputPort,
		FixedType: testType,
	}
	data := SaveData{
		NextNode: 4,
		NextLink: 3,
		Nodes: []SaveNode{
			{ID: "1N", Class: "example.com/Source", Inputs: []PortSpec{input}, Outputs: []PortSpec{output}},
			{ID: "2N", Class: "example.com/Source", Inputs: []PortSpec{input}, Outputs: []PortSpec{output}},
			{ID: "3N", Class: "example.com/Source", Inputs: []PortSpec{input}, Outputs: []PortSpec{output}},
		},
		Links: []SaveLink{
			{
				Name: FullLinkName{
					Link:   2,
					Input:  FullPortID{Node: 3, Port: input.ID},
					Output: FullPortID{Node: 2, Port: output.ID},
				}.String(),
				Type: testType,
			},
			{
				Name: FullLinkName{
					Link:   1,
					Input:  FullPortID{Node: 3, Port: input.ID},
					Output: FullPortID{Node: 1, Port: output.ID},
				}.String(),
				Type: testType,
			},
		},
	}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Restore(data); err != nil {
		t.Fatal(err)
	}
	if err := w.DefineClass("example.com", ClassSpec{
		Name:    "example.com/Source",
		Inputs:  []PortSpec{singleInput},
		Outputs: []PortSpec{output},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := w.Snapshot()
	if len(snapshot.Links) != 1 || snapshot.Links[0].ID != 1 {
		t.Fatalf("links = %#v, want only lowest-ID restored link to remain", snapshot.Links)
	}
}

func TestDefineClassReinitializesRecalledNodes(t *testing.T) {
	runtime := &lifecycleClass{}
	w, _, log := lifecycleWorkspace(t, runtime)
	node, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.RecallClass("example.com", "example.com/Source"); err != nil {
		t.Fatal(err)
	}
	if got, ok := w.Node(node); !ok || got.State != StateInactive {
		t.Fatalf("node = %#v, ok %v; want inactive", got, ok)
	}
	if err := w.DefineClass("example.com", ClassSpec{
		Name:    "example.com/Source",
		Runtime: runtime,
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
	if got, ok := w.Node(node); !ok || got.State != StateActive {
		t.Fatalf("node = %#v, ok %v; want active", got, ok)
	}
	want := []string{
		"init:new",
		"inactive-before:1:class-recall",
		"inactive-after:1:class-recall",
		"close:1",
		"init:restore",
	}
	if fmt.Sprint(*log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", *log, want)
	}
}

func TestRegisterLibraryReactivatesUnregisteredNodesAndLinks(t *testing.T) {
	runtime := &lifecycleClass{}
	w, _, log := lifecycleWorkspace(t, runtime)
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
	if err := w.UnregisterLibrary("example.com"); err != nil {
		t.Fatal(err)
	}
	if got, ok := w.Link(link); !ok || got.State != StateInactive {
		t.Fatalf("link = %#v, ok %v; want inactive", got, ok)
	}
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{
		Name:    "example.com/Source",
		Runtime: runtime,
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
	}}}); err != nil {
		t.Fatal(err)
	}
	if got, ok := w.Link(link); !ok || got.State != StateActive {
		t.Fatalf("link = %#v, ok %v; want active", got, ok)
	}
	want := []string{
		"init:new",
		"init:new",
		"object",
		"before:input:object:example.com/int",
		"before:output:object:example.com/int",
		"after:input:1:object:example.com/int",
		"after:output:1:object:example.com/int",
		"inactive-before:1:library-unregister",
		"inactive-before:2:library-unregister",
		"inactive-after:1:library-unregister",
		"inactive-after:2:library-unregister",
		"link-inactive:input:1:library-unregister",
		"link-inactive:output:1:library-unregister",
		"close:1",
		"close:2",
		"init:restore",
		"init:restore",
	}
	if fmt.Sprint(*log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", *log, want)
	}
}

type defineThenFailLibrary struct {
	name  string
	class ClassSpec
}

func (l defineThenFailLibrary) Name() string { return l.name }

func (l defineThenFailLibrary) DefineClasses(scope LibraryScope) error {
	if err := scope.DefineClass(l.class); err != nil {
		return err
	}
	return fmt.Errorf("define failed")
}

type createThenFailLibrary struct {
	name  string
	class ClassSpec
}

func (l createThenFailLibrary) Name() string { return l.name }

func (l createThenFailLibrary) DefineClasses(scope LibraryScope) error {
	if err := scope.DefineClass(l.class); err != nil {
		return err
	}
	if _, err := scope.CreateNode(l.class.Name, NodeOptions{}); err != nil {
		return err
	}
	return fmt.Errorf("create failed")
}

func TestRegisterLibraryRollbackRestoresInactiveNodesOnDefineError(t *testing.T) {
	initialRuntime := &lifecycleClass{}
	w, _, _ := lifecycleWorkspace(t, initialRuntime)
	logger := &testLogger{}
	w.logger = logger
	node, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.UnregisterLibrary("example.com"); err != nil {
		t.Fatal(err)
	}
	before := w.Snapshot()
	retryRuntime := &lifecycleClass{log: &[]string{}, nodes: map[NodeID]*lifecycleNode{}}
	err = w.RegisterLibrary(defineThenFailLibrary{
		name: "example.com",
		class: ClassSpec{
			Name:    "example.com/Source",
			Runtime: retryRuntime,
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
		},
	})
	if err == nil {
		t.Fatal("expected register failure")
	}
	if len(logger.errs) == 0 {
		t.Fatal("expected library define error to be logged")
	}
	after := w.Snapshot()
	if len(after.Libraries) != 0 || len(after.Classes) != len(before.Classes) {
		t.Fatalf("snapshot = %#v, want rolled back to %#v", after, before)
	}
	if got, ok := w.Node(node); !ok || got.State != StateInactive {
		t.Fatalf("node = %#v, ok %v; want inactive", got, ok)
	}
}

func TestRegisterLibraryClosesReactivatedRuntimesOnDefineError(t *testing.T) {
	w := NewWorkspace()
	if err := w.Restore(SaveData{
		NextNode: 2,
		Nodes: []SaveNode{{
			ID:    "1N",
			Class: "example.com/Source",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	log := []string{}
	err := w.RegisterLibrary(defineThenFailLibrary{
		name: "example.com",
		class: ClassSpec{
			Name:    "example.com/Source",
			Runtime: closingInitClass{log: &log},
		},
	})
	if err == nil {
		t.Fatal("expected register failure")
	}
	want := []string{"init:1", "close:1"}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", log, want)
	}
	if got, ok := w.Node(1); !ok || got.State != StateInactive {
		t.Fatalf("node = %#v, ok %v; want inactive after rollback", got, ok)
	}
}

func TestRegisterLibraryRollbackAllowsNonComparableExistingRuntime(t *testing.T) {
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{
		LibraryName: "example.com",
		Classes: []ClassSpec{{
			Name:    "example.com/Source",
			Runtime: nonComparableRuntimeClass{},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.CreateNode("example.com/Source", NodeOptions{}); err != nil {
		t.Fatal(err)
	}
	err := w.RegisterLibrary(defineThenFailLibrary{
		name: "other.com",
		class: ClassSpec{
			Name: "other.com/Other",
		},
	})
	if err == nil {
		t.Fatal("expected register failure")
	}
	if len(w.Snapshot().Libraries) != 1 {
		t.Fatal("failed registration should roll back without disturbing existing runtime")
	}
}

func TestRegisterLibraryClosesScopedCreatedRuntimeOnDefineError(t *testing.T) {
	w := NewWorkspace()
	log := []string{}
	err := w.RegisterLibrary(createThenFailLibrary{
		name: "example.com",
		class: ClassSpec{
			Name:    "example.com/Source",
			Runtime: closingInitClass{log: &log},
		},
	})
	if err == nil {
		t.Fatal("expected register failure")
	}
	want := []string{"init:1", "close:1"}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", log, want)
	}
	if snapshot := w.Snapshot(); len(snapshot.Nodes) != 0 || len(snapshot.Libraries) != 0 {
		t.Fatalf("failed registration should roll back model, got %#v", snapshot)
	}
}

func TestDefineClassReactivationRollsBackOnInitError(t *testing.T) {
	data := SaveData{
		NextNode: 2,
		Nodes: []SaveNode{{
			ID:    "1N",
			Class: "example.com/Source",
		}},
	}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Restore(data); err != nil {
		t.Fatal(err)
	}
	before := w.Save()
	log := []string{}
	if err := w.DefineClass("example.com", ClassSpec{
		Name:    "example.com/Source",
		Runtime: &lifecycleClass{log: &log, fail: true},
	}); err == nil {
		t.Fatal("expected define class init error")
	}
	after := w.Save()
	if fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("define class should roll back on init error: got %#v, want %#v", after, before)
	}
	snapshot := w.Snapshot()
	if len(snapshot.Classes) != 0 || snapshot.Nodes[0].State != StateInactive {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestDefineClassClosesInitializedRuntimesOnLaterInitError(t *testing.T) {
	data := SaveData{
		NextNode: 3,
		Nodes: []SaveNode{
			{ID: "1N", Class: "example.com/Source"},
			{ID: "2N", Class: "example.com/Source"},
		},
	}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Restore(data); err != nil {
		t.Fatal(err)
	}
	before := w.Save()
	log := []string{}
	if err := w.DefineClass("example.com", ClassSpec{
		Name:    "example.com/Source",
		Runtime: closingInitClass{log: &log, failID: 2},
	}); err == nil {
		t.Fatal("expected define class init error")
	}
	want := []string{"init:1", "init:2", "close:1"}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", log, want)
	}
	after := w.Save()
	if fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("define class should roll back on init error: got %#v, want %#v", after, before)
	}
}

type testLogger struct {
	errs []string
}

func (l *testLogger) Debug(args ...any)                 {}
func (l *testLogger) Debugf(format string, args ...any) {}
func (l *testLogger) Info(args ...any)                  {}
func (l *testLogger) Infof(format string, args ...any)  {}
func (l *testLogger) Warn(args ...any)                  {}
func (l *testLogger) Warnf(format string, args ...any)  {}
func (l *testLogger) Err(args ...any)                   {}
func (l *testLogger) Errf(format string, args ...any) {
	l.errs = append(l.errs, fmt.Sprintf(format, args...))
}
func (l *testLogger) Fatal(args ...any)                 {}
func (l *testLogger) Fatalf(format string, args ...any) {}

func TestCoverageErrorsNamesAndIDs(t *testing.T) {
	var nilErr *Error
	if nilErr.Error() != "<nil>" || nilErr.Unwrap() != nil {
		t.Fatalf("nil error methods = %q, %v", nilErr.Error(), nilErr.Unwrap())
	}
	if got := (&Error{Op: "op", Err: ErrNotFound}).Error(); got != "op: "+ErrNotFound.Error() {
		t.Fatalf("error without phase = %q", got)
	}
	if got := opErr("op", "phase", nil); got != nil {
		t.Fatalf("opErr nil = %v, want nil", got)
	}
	if got := (PortID{Number: 3, Kind: "sideways"}).String(); got != "3?" {
		t.Fatalf("unknown port string = %q", got)
	}

	badPortIDs := []string{"", "1x", "0i", "xi"}
	for _, value := range badPortIDs {
		if _, err := ParsePortID(value); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("ParsePortID(%q) = %v, want invalid id", value, err)
		}
	}
	badFullPorts := []string{"", "N1i", "1N", "0N1i", "1N0i"}
	for _, value := range badFullPorts {
		if _, err := ParseFullPortID(value); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("ParseFullPortID(%q) = %v, want invalid id", value, err)
		}
	}
	badLinks := []string{"1L:2N1i", "0L:2N1i:1N1o", "1L:0N1i:1N1o", "1L:2N1i:1N0o", "1L:2N1o:1N1i"}
	for _, value := range badLinks {
		if _, err := ParseFullLinkName(value); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("ParseFullLinkName(%q) = %v, want invalid id", value, err)
		}
	}

	if ValidLibraryName("") || ValidLibraryName("1example.com") || ValidLibraryName("example_com") {
		t.Fatal("invalid library names accepted")
	}
	if ValidQualifiedName("") || ValidQualifiedName("1/example") || ValidQualifiedName("example.com/Bad_Name") {
		t.Fatal("invalid qualified names accepted")
	}
	if ValidTypeName("notqualified") || ValidTypeName("example.com/") || ValidTypeName("example.com/Bool") {
		t.Fatal("invalid type names accepted")
	}
}

func TestCoverageBasicValidationAndClosedErrors(t *testing.T) {
	w := NewWorkspace()
	if err := w.RegisterLibrary(nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nil library error = %v, want not found", err)
	}
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "bad_name"}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("bad library error = %v, want invalid name", err)
	}
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate library error = %v, want duplicate", err)
	}
	if err := w.DefineClass("missing.com", ClassSpec{Name: "missing.com/Thing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing library define error = %v, want not found", err)
	}
	if err := w.DefineClass("example.com", ClassSpec{Name: "example.com/thing"}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("invalid class define error = %v, want invalid name", err)
	}
	invalidInput := PortSpec{ID: PortID{Number: 0, Kind: InputPort}, Direction: InputPort}
	if err := w.DefineClass("example.com", ClassSpec{Name: "example.com/Thing", Inputs: []PortSpec{invalidInput}}); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("invalid input define error = %v, want invalid port", err)
	}
	invalidOutput := PortSpec{ID: PortID{Number: 1, Kind: OutputPort}, Direction: OutputPort, FixedType: "example.com/Bad"}
	if err := w.DefineClass("example.com", ClassSpec{Name: "example.com/Thing", Outputs: []PortSpec{invalidOutput}}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("invalid output define error = %v, want invalid name", err)
	}
	class := scopedTestClass("example.com", "Source")
	if err := w.DefineClass("example.com", class); err != nil {
		t.Fatal(err)
	}
	if err := w.RecallClass("example.com", "missing.com/Missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing recall error = %v, want not found", err)
	}
	if err := w.RecallClass("other.com", "example.com/Source"); !errors.Is(err, ErrOwnership) {
		t.Fatalf("wrong owner recall error = %v, want ownership", err)
	}
	node, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetNodeCoordinate(999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing SetNodeCoordinate error = %v, want not found", err)
	}
	if err := w.SetNodeState(999, NodeState{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing SetNodeState error = %v, want not found", err)
	}
	if err := w.SetNodePorts(999, nil, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing SetNodePorts error = %v, want not found", err)
	}
	if err := w.SetNodePorts(node, []PortSpec{invalidInput}, nil); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("invalid SetNodePorts error = %v, want invalid port", err)
	}
	if _, err := w.CreateNode("missing.com/Missing", NodeOptions{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing CreateNode error = %v, want not found", err)
	}
	if _, err := w.CreateLink(FullPortID{Node: node, Port: PortID{Number: 1, Kind: InputPort}}, FullPortID{Node: node, Port: PortID{Number: 1, Kind: OutputPort}}, LinkOptions{}); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("self link error = %v, want invalid port", err)
	}
	if err := w.DeleteNode(999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing DeleteNode error = %v, want not found", err)
	}
	if err := w.DeleteLink(999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing DeleteLink error = %v, want not found", err)
	}
	if err := w.SetLinkWaypoints(999, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing SetLinkWaypoints error = %v, want not found", err)
	}
	if err := w.UnregisterLibrary("missing.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing unregister error = %v, want not found", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second close = %v, want nil", err)
	}
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "other.com"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed register error = %v, want closed", err)
	}
	if err := w.UnregisterLibrary("example.com"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed unregister error = %v, want closed", err)
	}
	if err := w.DefineClass("example.com", class); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed define error = %v, want closed", err)
	}
	if err := w.RecallClass("example.com", "example.com/Source"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed recall error = %v, want closed", err)
	}
	if _, err := w.CreateNode("example.com/Source", NodeOptions{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed create node error = %v, want closed", err)
	}
	if err := w.SetNodeCoordinate(node, "x"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed coordinate error = %v, want closed", err)
	}
	if err := w.SetNodeState(node, NodeState{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed state error = %v, want closed", err)
	}
	if err := w.SetNodePorts(node, nil, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed ports error = %v, want closed", err)
	}
	if _, err := w.Copy([]NodeID{999}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing copy error = %v, want not found", err)
	}
	if _, _, err := w.Paste(Clipboard{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed paste error = %v, want closed", err)
	}
	if err := w.Restore(SaveData{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed restore error = %v, want closed", err)
	}
}

type initScopeExerciseClass struct {
	scope NodeScope
}

func (c *initScopeExerciseClass) InitNode(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
	c.scope = ctx.Node
	if ctx.Node.ReadOnly() == nil {
		return nil, fmt.Errorf("missing read-only scope")
	}
	snap, ok := ctx.Node.Snapshot()
	if !ok || snap.ID != ctx.ID {
		return nil, fmt.Errorf("missing init snapshot")
	}
	if err := ctx.Node.SetState(NodeState{DisplayName: "from-init", Metadata: map[string]string{"remove": "true"}}); err != nil {
		return nil, err
	}
	if err := ctx.Node.SetCoordinate("x:init"); err != nil {
		return nil, err
	}
	if err := ctx.Node.SetMetadata(map[string]string{"remove": "true", "keep": "true"}); err != nil {
		return nil, err
	}
	if err := ctx.Node.DeleteMetadataValue("remove"); err != nil {
		return nil, err
	}
	if err := ctx.Node.SetPorts([]PortSpec{{ID: PortID{Number: 0, Kind: InputPort}, Direction: InputPort}}, nil); !errors.Is(err, ErrInvalidPort) {
		return nil, fmt.Errorf("invalid init SetPorts = %v", err)
	}
	if err := ctx.Node.SetPorts([]PortSpec{{
		ID:        PortID{Number: 2, Kind: InputPort},
		Name:      "init-in",
		Direction: InputPort,
		FixedType: testType,
	}}, []PortSpec{{
		ID:        PortID{Number: 2, Kind: OutputPort},
		Name:      "init-out",
		Direction: OutputPort,
		FixedType: testType,
	}}); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

func TestCoverageNodeScopeDuringInitAndAfterFinish(t *testing.T) {
	runtime := &initScopeExerciseClass{}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{
		Name:    "example.com/Scoped",
		Runtime: runtime,
	}}}); err != nil {
		t.Fatal(err)
	}
	node, err := w.CreateNode("example.com/Scoped", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	snap, ok := w.Node(node)
	if !ok {
		t.Fatal("node should exist")
	}
	if snap.Dynamic.DisplayName != "from-init" || snap.Dynamic.Coordinate != "x:init" || len(snap.Dynamic.Metadata) != 1 || snap.Dynamic.Metadata["keep"] != "true" {
		t.Fatalf("init-mutated state = %#v", snap.Dynamic)
	}
	if len(snap.Inputs) != 1 || snap.Inputs[0].ID.Number != 2 || len(snap.Outputs) != 1 || snap.Outputs[0].ID.Number != 2 {
		t.Fatalf("init-mutated ports = %#v %#v", snap.Inputs, snap.Outputs)
	}
	if _, ok := runtime.scope.Snapshot(); !ok {
		t.Fatal("finished scope should still see committed node")
	}
	if err := w.DeleteNode(node); err != nil {
		t.Fatal(err)
	}
	if _, ok := runtime.scope.Snapshot(); ok {
		t.Fatal("deleted node scope snapshot should report false after init record is cleared")
	}
	if err := runtime.scope.SetState(NodeState{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted scope SetState error = %v, want not found", err)
	}
	if err := runtime.scope.SetCoordinate("late"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted scope SetCoordinate error = %v, want not found", err)
	}
	if err := runtime.scope.SetPorts(nil, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted scope SetPorts error = %v, want not found", err)
	}
}

func TestCoverageLinkTypeSelectionAndValidation(t *testing.T) {
	inputOnly := PortSpec{ID: PortID{Number: 1, Kind: InputPort}, Name: "in", Direction: InputPort, FixedType: testType, Multiple: true}
	outputAny := PortSpec{ID: PortID{Number: 1, Kind: OutputPort}, Name: "out", Direction: OutputPort, AcceptedTypes: []string{testType}}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{
		Name:    "example.com/Flexible",
		Inputs:  []PortSpec{inputOnly},
		Outputs: []PortSpec{outputAny},
	}}}); err != nil {
		t.Fatal(err)
	}
	a, _ := w.CreateNode("example.com/Flexible", NodeOptions{})
	b, _ := w.CreateNode("example.com/Flexible", NodeOptions{})
	link, err := w.CreateLink(
		FullPortID{Node: b, Port: inputOnly.ID},
		FullPortID{Node: a, Port: outputAny.ID},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := w.Link(link); got.Type != testType {
		t.Fatalf("chosen type = %q, want %q", got.Type, testType)
	}
	if _, err := w.CreateLink(
		FullPortID{Node: 999, Port: inputOnly.ID},
		FullPortID{Node: a, Port: outputAny.ID},
		LinkOptions{},
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing input node link error = %v, want not found", err)
	}
	if _, err := w.CreateLink(
		FullPortID{Node: b, Port: inputOnly.ID},
		FullPortID{Node: 999, Port: outputAny.ID},
		LinkOptions{},
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing output node link error = %v, want not found", err)
	}
	if _, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 99, Kind: InputPort}},
		FullPortID{Node: a, Port: outputAny.ID},
		LinkOptions{},
	); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("missing input port link error = %v, want invalid port", err)
	}
	if _, err := w.CreateLink(
		FullPortID{Node: b, Port: inputOnly.ID},
		FullPortID{Node: a, Port: PortID{Number: 99, Kind: OutputPort}},
		LinkOptions{},
	); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("missing output port link error = %v, want invalid port", err)
	}
	if _, err := w.CreateLink(
		FullPortID{Node: b, Port: inputOnly.ID},
		FullPortID{Node: a, Port: outputAny.ID},
		LinkOptions{Type: "example.com/float"},
	); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("requested mismatched type error = %v, want type mismatch", err)
	}

	noType := ClassSpec{
		Name: "example.com/NoType",
		Inputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: InputPort},
			Name:      "in",
			Direction: InputPort,
			Multiple:  true,
		}},
		Outputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: OutputPort},
			Name:      "out",
			Direction: OutputPort,
		}},
	}
	if err := w.DefineClass("example.com", noType); err != nil {
		t.Fatal(err)
	}
	c, _ := w.CreateNode("example.com/NoType", NodeOptions{})
	d, _ := w.CreateNode("example.com/NoType", NodeOptions{})
	if _, err := w.CreateLink(
		FullPortID{Node: d, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: c, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("untyped link error = %v, want type mismatch", err)
	}
}

func TestCoverageConfigRestoreCopyPasteAndRuntimeExports(t *testing.T) {
	w, nodes, _ := privateHookWorkspace(t, &privateHookClass{}, nil)
	a, err := w.CreateNode("example.com/Private", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.CreateNode("example.com/Private", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	nodes[a].exported = []string{"from-runtime"}
	cfg, err := w.SaveConfigWithRuntimeState()
	if err != nil {
		t.Fatal(err)
	}
	got, err := cfg.Get(configer.Path{"nodes", "0", "state", "Private", "0"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-runtime" {
		t.Fatalf("runtime config private = %#v", got)
	}
	nodes[a].failExport = true
	if _, err := w.SaveConfigWithRuntimeState(); err == nil {
		t.Fatal("expected SaveConfigWithRuntimeState export error")
	}
	nodes[a].failExport = false
	if err := w.RestoreConfig(nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nil RestoreConfig error = %v, want not found", err)
	}
	if _, err := w.Copy([]NodeID{a, a, b}); err != nil {
		t.Fatalf("copy duplicate selection: %v", err)
	}

	restored, _, _ := privateHookWorkspace(t, &privateHookClass{}, nil)
	data := SaveData{
		NextNode: -10,
		NextLink: -20,
		Nodes:    []SaveNode{{ID: "3N", Class: "example.com/Private"}},
	}
	if err := restored.Restore(data); err != nil {
		t.Fatal(err)
	}
	if saved := restored.Save(); saved.NextNode != 4 || saved.NextLink != 1 {
		t.Fatalf("next IDs after restore = %#v, want node 4 link 1", saved)
	}
	if err := restored.Restore(SaveData{Nodes: []SaveNode{{
		ID: "1N", Class: "example.com/Private",
		Outputs: []PortSpec{{ID: PortID{Number: 1, Kind: InputPort}, Direction: OutputPort}},
	}}}); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("invalid restore output error = %v, want invalid port", err)
	}
	if err := restored.Restore(SaveData{Links: []SaveLink{{Name: "bad"}}}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("invalid restore link name error = %v, want invalid id", err)
	}
	if _, _, err := restored.Paste(Clipboard{Nodes: []SaveNode{{ID: "bad", Class: "example.com/Private"}}}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("invalid paste node id error = %v, want invalid id", err)
	}
	if _, _, err := restored.Paste(Clipboard{Nodes: []SaveNode{{ID: "1N", Class: "missing.com/Missing"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid paste class error = %v, want not found", err)
	}
}

func TestCoverageLoggerAndLifecycleErrorBranches(t *testing.T) {
	logger := &testLogger{}
	w, nodes, _ := lifecycleWorkspace(t, &lifecycleClass{})
	w.logger = logger
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
	nodes[b].panicLinkInactive = true
	if err := w.RecallClass("example.com", "example.com/Source"); err != nil {
		t.Fatal(err)
	}
	if len(logger.errs) == 0 {
		t.Fatal("expected logged link inactive panic")
	}
	if got, ok := w.Link(link); !ok || got.State != StateInactive {
		t.Fatalf("link after recall = %#v, ok %v", got, ok)
	}

	optLogger := &testLogger{}
	withLogger := NewWorkspace(WithLogger(optLogger))
	if withLogger.logger != optLogger {
		t.Fatal("WithLogger did not install logger")
	}
}

func TestCoverageLibraryScopeReadOnlyAndSuccessfulWrappers(t *testing.T) {
	w := NewWorkspace()
	lib := &captureScopeLibrary{name: "example.com", classes: []ClassSpec{scopedTestClass("example.com", "Source")}}
	if err := w.RegisterLibrary(lib); err != nil {
		t.Fatal(err)
	}
	if lib.scope.ReadOnly() == nil {
		t.Fatal("library scope read-only is nil")
	}
	if err := lib.scope.CanCreateNode("example.com/Source"); err != nil {
		t.Fatal(err)
	}
	node, err := lib.scope.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.scope.SetNodePrivate(node, "private"); err != nil {
		t.Fatal(err)
	}
	if err := lib.scope.SetNodeMetadataValue(node, "k", "v"); err != nil {
		t.Fatal(err)
	}
	if err := lib.scope.DeleteNodeMetadataValue(node, "k"); err != nil {
		t.Fatal(err)
	}
	if err := lib.scope.CanDeleteNode(node); err != nil {
		t.Fatal(err)
	}
	if err := lib.scope.DeleteNode(node); err != nil {
		t.Fatal(err)
	}
	if err := lib.scope.CanDeleteLink(999); !errors.Is(err, ErrOwnership) {
		t.Fatalf("missing owned link check = %v, want ownership", err)
	}
}

type capturedWorkspaceClass struct {
	w           *Workspace
	closeDuring bool
	recallClass bool
	deleteID    NodeID
	wrongClass  NodeID
}

func (c capturedWorkspaceClass) InitNode(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
	if c.closeDuring {
		return struct{}{}, c.w.Close()
	}
	if c.recallClass {
		if err := c.w.RecallClass(ctx.Library, ctx.Class); err != nil {
			return nil, err
		}
	}
	if c.deleteID != 0 {
		c.w.mu.Lock()
		delete(c.w.nodes, c.deleteID)
		c.w.mu.Unlock()
	}
	if c.wrongClass != 0 {
		c.w.mu.Lock()
		if node := c.w.nodes[c.wrongClass]; node != nil {
			node.class = "example.com/Other"
		}
		c.w.mu.Unlock()
	}
	return struct{}{}, nil
}

type mutatingInactiveNode struct {
	w           *Workspace
	once        bool
	close       bool
	deleteLib   string
	deleteClass string
	changeOwner string
}

func (n *mutatingInactiveNode) BeforeInactive(InactiveReason) error {
	if n.once {
		return nil
	}
	n.once = true
	if n.close {
		n.w.mu.Lock()
		n.w.closed = true
		n.w.mu.Unlock()
	}
	if n.deleteLib != "" {
		n.w.mu.Lock()
		delete(n.w.libraries, n.deleteLib)
		n.w.mu.Unlock()
	}
	if n.deleteClass != "" {
		n.w.mu.Lock()
		delete(n.w.classes, n.deleteClass)
		n.w.mu.Unlock()
	}
	if n.changeOwner != "" {
		n.w.mu.Lock()
		if rec := n.w.classes[n.changeOwner]; rec != nil {
			rec.library = "other.com"
		}
		n.w.mu.Unlock()
	}
	return nil
}

func (n *mutatingInactiveNode) AfterInactive(InactiveReason) {}

type mutatingDeleteNode struct {
	w      *Workspace
	id     NodeID
	close  bool
	delete bool
}

func (n mutatingDeleteNode) BeforeDelete() error {
	if n.close {
		n.w.mu.Lock()
		n.w.closed = true
		n.w.mu.Unlock()
	}
	if n.delete {
		n.w.mu.Lock()
		delete(n.w.nodes, n.id)
		n.w.mu.Unlock()
	}
	return nil
}

func (n mutatingDeleteNode) AfterDelete() {}

type mutatingDetachNode struct {
	w      *Workspace
	link   LinkID
	close  bool
	delete bool
	add    *linkRecord
}

func (n mutatingDetachNode) BeforeLinkDetach(LinkEndpoint) error {
	if n.close {
		n.w.mu.Lock()
		n.w.closed = true
		n.w.mu.Unlock()
	}
	if n.delete {
		n.w.mu.Lock()
		delete(n.w.links, n.link)
		n.w.mu.Unlock()
	}
	if n.add != nil {
		n.w.mu.Lock()
		n.w.links[n.add.id] = n.add
		n.w.mu.Unlock()
	}
	return nil
}

func (n mutatingDetachNode) AfterLinkDetach(LinkEndpoint) {}

func TestCoverageMoreLifecycleValidationBranches(t *testing.T) {
	w, nodes, _ := lifecycleWorkspace(t, &lifecycleClass{})
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	nodes[b].failAttach = true
	if _, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	); err == nil {
		t.Fatal("expected input attach failure")
	}
	nodes[b].failAttach = false
	link, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	nodes[a].failDetach = true
	if err := w.DeleteLink(link); err == nil {
		t.Fatal("expected output detach failure")
	}
	nodes[a].failDetach = false
	nodes[a].panicOnClose = true
	if err := w.DeleteNode(a); err == nil {
		t.Fatal("expected close panic error from delete")
	}

	closeFail, closeNodes, _ := lifecycleWorkspace(t, &lifecycleClass{})
	closeNode, _ := closeFail.CreateNode("example.com/Source", NodeOptions{})
	closeNodes[closeNode].failInactive = true
	if err := closeFail.Close(); err == nil {
		t.Fatal("expected close before-inactive error")
	}

	unregisterFail, unregisterNodes, _ := lifecycleWorkspace(t, &lifecycleClass{})
	unregisterNode, _ := unregisterFail.CreateNode("example.com/Source", NodeOptions{})
	unregisterNodes[unregisterNode].failInactive = true
	if err := unregisterFail.UnregisterLibrary("example.com"); err == nil {
		t.Fatal("expected unregister before-inactive error")
	}

	closeDuring := NewWorkspace()
	closeRuntime := capturedWorkspaceClass{w: closeDuring, closeDuring: true}
	if err := closeDuring.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{
		Name:    "example.com/Closer",
		Runtime: closeRuntime,
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := closeDuring.CreateNode("example.com/Closer", NodeOptions{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("CreateNode closing during init error = %v, want closed", err)
	}

	inactivateDuring := NewWorkspace()
	inactivateRuntime := capturedWorkspaceClass{w: inactivateDuring, recallClass: true}
	if err := inactivateDuring.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{
		Name:    "example.com/Recalled",
		Runtime: inactivateRuntime,
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := inactivateDuring.CreateNode("example.com/Recalled", NodeOptions{}); !errors.Is(err, ErrInactive) {
		t.Fatalf("CreateNode inactivating during init error = %v, want inactive", err)
	}
}

func TestCoveragePrivateHelpersAndCorruptStateBranches(t *testing.T) {
	if got := (&Error{Op: "op", Phase: "phase", Err: ErrClosed}).Error(); got != "op phase: "+ErrClosed.Error() {
		t.Fatalf("phased error string = %q", got)
	}
	if ValidTypeName("example.com/bad_name") {
		t.Fatal("type with underscore should be invalid")
	}
	if err := (StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{Name: "example.com/bad"}}}).DefineClasses(&libraryScope{w: NewWorkspace(), library: "example.com"}); err == nil {
		t.Fatal("expected static library define error")
	}
	if _, err := saveDataConfig(SaveData{Nodes: []SaveNode{{ID: "1N", State: NodeState{Private: func() {}}}}}); err == nil {
		t.Fatal("expected config marshal error")
	}

	log := []string{}
	w := NewWorkspace()
	err := w.cleanupInitializedRuntimes(
		map[NodeID]NodeRuntime{
			2: &lifecycleNode{id: 2, log: &log, panicOnClose: true},
			1: &lifecycleNode{id: 1, log: &log},
		},
		map[NodeID]*nodeScope{
			2: {id: 2},
			1: {id: 1},
		},
	)
	if err == nil {
		t.Fatal("expected cleanup close error")
	}
	if !containsString(log, "close:1") {
		t.Fatalf("cleanup log = %#v, want sorted close of node 1", log)
	}

	class := scopedTestClass("example.com", "Source")
	w = NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{class}}); err != nil {
		t.Fatal(err)
	}
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	pending, err := w.prepareCreateLinkLocked(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	w.links[pending.link.id] = pending.link
	if _, err := w.commitPreparedLinkLocked(pending); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate commit error = %v, want duplicate", err)
	}
	delete(w.links, pending.link.id)
	pending.link.typ = "example.com/float"
	if _, err := w.commitPreparedLinkLocked(pending); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("type-changing commit error = %v, want type mismatch", err)
	}
	delete(w.links, pending.link.id)
	w.nextLink = pending.link.id
	pending.link.typ = testType
	if _, err := w.commitPreparedLinkLocked(pending); err != nil {
		t.Fatal(err)
	}
	if w.nextLink != pending.link.id+1 {
		t.Fatalf("next link = %d, want %d", w.nextLink, pending.link.id+1)
	}

	w.links[99] = nil
	w.links[100] = &linkRecord{id: 100, input: FullPortID{Node: 999, Port: PortID{Number: 1, Kind: InputPort}}, output: FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}}, typ: testType}
	w.links[101] = &linkRecord{id: 101, input: FullPortID{Node: b, Port: PortID{Number: 99, Kind: InputPort}}, output: FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}}, typ: testType}
	w.links[102] = &linkRecord{id: 102, input: FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}}, output: FullPortID{Node: a, Port: PortID{Number: 99, Kind: OutputPort}}, typ: testType}
	events := w.removeInvalidLinksLocked()
	if len(events) != 3 {
		t.Fatalf("remove invalid events = %#v, want 3", events)
	}
	delete(w.links, 99)

	w.links[200] = &linkRecord{id: 200, input: FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}}, output: FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}}, typ: testType}
	w.links[201] = &linkRecord{id: 201, input: FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}}, output: FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}}, typ: testType}
	if err := w.validateAttachedLinksLocked(b); !errors.Is(err, ErrMultiplicity) {
		t.Fatalf("corrupt multiplicity validation = %v, want multiplicity", err)
	}
	delete(w.nodes, a)
	if err := w.validateAttachedLinksLocked(b); !errors.Is(err, ErrNotFound) {
		t.Fatalf("corrupt missing node validation = %v, want not found", err)
	}

	classes := map[string]*classRecord{"nil": nil}
	if got := cloneClassRecords(classes); len(got) != 0 {
		t.Fatalf("clone nil classes = %#v, want empty", got)
	}
	nodes := map[NodeID]*nodeRecord{1: nil}
	if got := cloneNodeRecords(nodes); len(got) != 0 {
		t.Fatalf("clone nil nodes = %#v, want empty", got)
	}
	links := map[LinkID]*linkRecord{1: nil}
	if got := cloneLinkRecords(links); len(got) != 0 {
		t.Fatalf("clone nil links = %#v, want empty", got)
	}

	if _, err := chooseLinkType(
		PortSpec{AcceptedTypes: []string{"example.com/float"}},
		PortSpec{FixedType: testType},
		"",
	); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("output fixed mismatch = %v, want type mismatch", err)
	}
	if _, err := chooseLinkType(
		PortSpec{FixedType: testType},
		PortSpec{AcceptedTypes: []string{"example.com/float"}},
		"",
	); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("input fixed mismatch = %v, want type mismatch", err)
	}
	if portAccepts(PortSpec{AcceptedTypes: []string{"example.com/float"}}, testType) {
		t.Fatal("portAccepts accepted missing type")
	}
	if err := validatePorts([]PortSpec{
		{ID: PortID{Number: 1, Kind: InputPort}, Direction: InputPort},
		{ID: PortID{Number: 1, Kind: InputPort}, Direction: InputPort},
	}, InputPort); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate ports = %v, want duplicate", err)
	}
	if err := validatePorts([]PortSpec{{
		ID:            PortID{Number: 1, Kind: InputPort},
		Direction:     InputPort,
		AcceptedTypes: []string{"example.com/Bad"},
	}}, InputPort); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("invalid accepted type = %v, want invalid name", err)
	}

	cycleWorkspace := &Workspace{links: map[LinkID]*linkRecord{
		1: {id: 1, output: FullPortID{Node: 1}, input: FullPortID{Node: 2}},
		2: {id: 2, output: FullPortID{Node: 2}, input: FullPortID{Node: 1}},
	}}
	if !cycleWorkspace.pathExistsLocked(1, 2, 0) {
		t.Fatal("expected path through direct link")
	}
	if !cycleWorkspace.pathExistsLocked(1, 1, 0) {
		t.Fatal("expected path to self")
	}
	if cycleWorkspace.pathExistsLocked(3, 4, 0) {
		t.Fatal("unexpected path for disconnected nodes")
	}
	if cycleWorkspace.pathExistsLocked(1, 3, 0) {
		t.Fatal("unexpected path out of closed cycle")
	}
	sortNodes := []restoreInitNode{
		{record: &nodeRecord{id: 1}},
		{record: &nodeRecord{id: 2}},
	}
	cycleWorkspace.sortRestoreInitNodesLocked(sortNodes)
	if len(sortNodes) != 2 || sortNodes[0].record.id != 1 || sortNodes[1].record.id != 2 {
		t.Fatalf("cycle fallback order = %#v", sortNodes)
	}
}

func TestCoverageMoreSaveRestorePasteAndScopeBranches(t *testing.T) {
	w, nodes, _ := privateHookWorkspace(t, &privateHookClass{}, nil)
	a, _ := w.CreateNode("example.com/Private", NodeOptions{})
	b, _ := w.CreateNode("example.com/Private", NodeOptions{})
	nodes[a].exported = "selected"
	nodes[b].exported = "not-selected"
	clip, err := w.Copy([]NodeID{a})
	if err != nil {
		t.Fatal(err)
	}
	if len(clip.Nodes) != 1 || clip.Nodes[0].State.Private != "selected" {
		t.Fatalf("selected copy private = %#v", clip)
	}
	copyLinks := NewWorkspace()
	copyClass := ClassSpec{
		Name: "example.com/Multi",
		Inputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: InputPort},
			Direction: InputPort,
			FixedType: testType,
			Multiple:  true,
		}},
		Outputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: OutputPort},
			Direction: OutputPort,
			FixedType: testType,
		}},
	}
	if err := copyLinks.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{copyClass}}); err != nil {
		t.Fatal(err)
	}
	cl1, _ := copyLinks.CreateNode("example.com/Multi", NodeOptions{})
	cl2, _ := copyLinks.CreateNode("example.com/Multi", NodeOptions{})
	cl3, _ := copyLinks.CreateNode("example.com/Multi", NodeOptions{})
	if _, err := copyLinks.CreateLink(FullPortID{Node: cl3, Port: copyClass.Inputs[0].ID}, FullPortID{Node: cl2, Port: copyClass.Outputs[0].ID}, LinkOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := copyLinks.CreateLink(FullPortID{Node: cl3, Port: copyClass.Inputs[0].ID}, FullPortID{Node: cl1, Port: copyClass.Outputs[0].ID}, LinkOptions{}); err != nil {
		t.Fatal(err)
	}
	if clip, err := copyLinks.Copy([]NodeID{cl1, cl2, cl3}); err != nil || len(clip.Links) != 2 || clip.Links[0].Name > clip.Links[1].Name {
		t.Fatalf("copied sorted links = %#v err %v", clip.Links, err)
	}
	if _, _, err := w.Paste(Clipboard{Nodes: []SaveNode{{
		ID: "1N", Class: "example.com/Private",
		Outputs: []PortSpec{{ID: PortID{Number: 1, Kind: InputPort}, Direction: OutputPort}},
	}}}); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("invalid paste output error = %v, want invalid port", err)
	}
	if _, _, err := w.Paste(Clipboard{
		Nodes: []SaveNode{{ID: "1N", Class: "example.com/Private"}},
		Links: []SaveLink{{Name: "bad"}},
	}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("invalid paste link name error = %v, want invalid id", err)
	}
	if pastedNodes, pastedLinks, err := w.Paste(Clipboard{
		Nodes: []SaveNode{{ID: "1N", Class: "example.com/Private"}},
		Links: []SaveLink{{Name: "1L:2N1i:1N1o", Type: testType}},
	}); err != nil || len(pastedNodes) != 1 || len(pastedLinks) != 0 {
		t.Fatalf("external paste link = nodes %#v links %#v err %v", pastedNodes, pastedLinks, err)
	}
	failPaste := NewWorkspace()
	if err := failPaste.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{
		Name:    "example.com/Fail",
		Runtime: &lifecycleClass{log: &[]string{}, fail: true},
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := failPaste.Paste(Clipboard{Nodes: []SaveNode{{ID: "1N", Class: "example.com/Fail"}}}); err == nil {
		t.Fatal("expected paste init error")
	}

	emptyRestore := NewWorkspace()
	if err := emptyRestore.Restore(SaveData{NextNode: -1, NextLink: -1}); err != nil {
		t.Fatal(err)
	}
	if saved := emptyRestore.Save(); saved.NextNode != 1 || saved.NextLink != 1 {
		t.Fatalf("empty restore next IDs = %#v, want 1/1", saved)
	}
	badCfg := configer.NewMemory(nil)
	if err := badCfg.Set(configer.Path{"nodes"}, "not-a-list"); err != nil {
		t.Fatal(err)
	}
	if err := emptyRestore.RestoreConfig(badCfg); err == nil {
		t.Fatal("expected restore config unmarshal error")
	}
	closeRestore := NewWorkspace()
	closeRuntime := capturedWorkspaceClass{w: closeRestore, closeDuring: true}
	if err := closeRestore.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{Name: "example.com/Closer", Runtime: closeRuntime}}}); err != nil {
		t.Fatal(err)
	}
	if err := closeRestore.Restore(SaveData{Nodes: []SaveNode{{ID: "1N", Class: "example.com/Closer"}}}); !errors.Is(err, ErrClosed) {
		t.Fatalf("restore closed during init = %v, want closed", err)
	}

	regular, _ := testWorkspace(t)
	if err := regular.RecallClass("example.com", "example.com/Source"); err != nil {
		t.Fatal(err)
	}
	if err := regular.CanCreateNode("example.com/Source"); !errors.Is(err, ErrInactive) {
		t.Fatalf("inactive CanCreateNode = %v, want inactive", err)
	}
	if err := regular.CanDeleteLink(999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing CanDeleteLink = %v, want not found", err)
	}
	if err := regular.SetLinkWaypoints(999, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing SetLinkWaypoints = %v, want not found", err)
	}
	if err := regular.Close(); err != nil {
		t.Fatal(err)
	}
	if err := regular.DeleteLink(999); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed DeleteLink = %v, want closed", err)
	}
	if err := regular.SetLinkWaypoints(999, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetLinkWaypoints = %v, want closed", err)
	}

	scopeWorkspace := NewWorkspace()
	lib := &captureScopeLibrary{name: "example.com", classes: []ClassSpec{scopedTestClass("example.com", "Source")}}
	if err := scopeWorkspace.RegisterLibrary(lib); err != nil {
		t.Fatal(err)
	}
	n1, _ := lib.scope.CreateNode("example.com/Source", NodeOptions{})
	n2, _ := lib.scope.CreateNode("example.com/Source", NodeOptions{})
	link, err := lib.scope.CreateLink(
		FullPortID{Node: n2, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: n1, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.scope.CanCreateLink(
		FullPortID{Node: n2, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: n1, Port: PortID{Number: 1, Kind: OutputPort}},
		testType,
	); !errors.Is(err, ErrMultiplicity) {
		t.Fatalf("scope CanCreateLink duplicate = %v, want multiplicity", err)
	}
	if err := lib.scope.CanSetLinkWaypoints(link); err != nil {
		t.Fatal(err)
	}
	if err := lib.scope.CanDeleteLink(link); err != nil {
		t.Fatal(err)
	}
	directScope := &libraryScope{w: scopeWorkspace, library: "example.com"}
	if _, err := directScope.CreateNode("example.com/Source", NodeOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageConcurrentRevalidationBranches(t *testing.T) {
	for _, tt := range []struct {
		name string
		mut  func(*Workspace, *mutatingInactiveNode)
		call func(*Workspace) error
		err  error
	}{
		{
			name: "close observes concurrently closed workspace",
			mut:  func(w *Workspace, n *mutatingInactiveNode) { n.close = true },
			call: func(w *Workspace) error { return w.Close() },
			err:  nil,
		},
		{
			name: "unregister observes closed workspace",
			mut:  func(w *Workspace, n *mutatingInactiveNode) { n.close = true },
			call: func(w *Workspace) error { return w.UnregisterLibrary("example.com") },
			err:  ErrClosed,
		},
		{
			name: "unregister observes missing library",
			mut:  func(w *Workspace, n *mutatingInactiveNode) { n.deleteLib = "example.com" },
			call: func(w *Workspace) error { return w.UnregisterLibrary("example.com") },
			err:  ErrNotFound,
		},
		{
			name: "recall observes closed workspace",
			mut:  func(w *Workspace, n *mutatingInactiveNode) { n.close = true },
			call: func(w *Workspace) error { return w.RecallClass("example.com", "example.com/Source") },
			err:  ErrClosed,
		},
		{
			name: "recall observes missing class",
			mut:  func(w *Workspace, n *mutatingInactiveNode) { n.deleteClass = "example.com/Source" },
			call: func(w *Workspace) error { return w.RecallClass("example.com", "example.com/Source") },
			err:  ErrNotFound,
		},
		{
			name: "recall observes changed owner",
			mut:  func(w *Workspace, n *mutatingInactiveNode) { n.changeOwner = "example.com/Source" },
			call: func(w *Workspace) error { return w.RecallClass("example.com", "example.com/Source") },
			err:  ErrOwnership,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWorkspace()
			runtime := &mutatingInactiveNode{w: w}
			tt.mut(w, runtime)
			if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{
				Name: "example.com/Source",
				Runtime: nodeClassFunc(func(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
					return runtime, nil
				}),
			}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := w.CreateNode("example.com/Source", NodeOptions{}); err != nil {
				t.Fatal(err)
			}
			err := tt.call(w)
			if tt.err == nil {
				if err != nil {
					t.Fatalf("call error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.err) {
				t.Fatalf("call error = %v, want %v", err, tt.err)
			}
		})
	}

	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Restore(SaveData{Nodes: []SaveNode{{ID: "1N", Class: "example.com/Source"}}}); err != nil {
		t.Fatal(err)
	}
	runtime := capturedWorkspaceClass{w: w, deleteID: 1}
	if err := w.DefineClass("example.com", ClassSpec{Name: "example.com/Source", Runtime: runtime}); !errors.Is(err, ErrInactive) {
		t.Fatalf("define class deleted reactivated node error = %v, want inactive", err)
	}
	wrongClass := NewWorkspace()
	if err := wrongClass.RegisterLibrary(StaticLibrary{LibraryName: "example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := wrongClass.Restore(SaveData{Nodes: []SaveNode{{ID: "1N", Class: "example.com/Source"}}}); err != nil {
		t.Fatal(err)
	}
	if err := wrongClass.DefineClass("example.com", ClassSpec{Name: "example.com/Source", Runtime: capturedWorkspaceClass{w: wrongClass, wrongClass: 1}}); !errors.Is(err, ErrInactive) {
		t.Fatalf("define class changed reactivated node error = %v, want inactive", err)
	}
	directReactivation := NewWorkspace()
	directReactivation.classes["example.com/Source"] = &classRecord{spec: ClassSpec{Name: "example.com/Source", Runtime: nodeClassFunc(func(NodeContext, NodeState, InitMode) (NodeRuntime, error) {
		return struct{}{}, nil
	})}}
	directReactivation.nodes[2] = &nodeRecord{id: 2, class: "example.com/Other", state: StateActive}
	directReactivation.nodes[3] = &nodeRecord{id: 3, class: "example.com/Source", state: StateInactive}
	_ = directReactivation.reactivatedInitNodesLocked("example.com/Source", nil)

	deleteClose := NewWorkspace()
	deleteRuntime := mutatingDeleteNode{w: deleteClose, close: true}
	if err := deleteClose.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{Name: "example.com/Source", Runtime: nodeClassFunc(func(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
		deleteRuntime.id = ctx.ID
		return deleteRuntime, nil
	})}}}); err != nil {
		t.Fatal(err)
	}
	deleteNode, _ := deleteClose.CreateNode("example.com/Source", NodeOptions{})
	if err := deleteClose.DeleteNode(deleteNode); !errors.Is(err, ErrClosed) {
		t.Fatalf("delete after close hook = %v, want closed", err)
	}

	deleteMissing := NewWorkspace()
	missingRuntime := mutatingDeleteNode{w: deleteMissing, delete: true}
	if err := deleteMissing.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{Name: "example.com/Source", Runtime: nodeClassFunc(func(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
		missingRuntime.id = ctx.ID
		return missingRuntime, nil
	})}}}); err != nil {
		t.Fatal(err)
	}
	missingNode, _ := deleteMissing.CreateNode("example.com/Source", NodeOptions{})
	if err := deleteMissing.DeleteNode(missingNode); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete after node removed hook = %v, want not found", err)
	}

	detachWorkspace := NewWorkspace()
	detacher := mutatingDetachNode{w: detachWorkspace}
	if err := detachWorkspace.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{
		Name:    "example.com/Source",
		Runtime: nodeClassFunc(func(NodeContext, NodeState, InitMode) (NodeRuntime, error) { return detacher, nil }),
		Inputs:  []PortSpec{{ID: PortID{Number: 1, Kind: InputPort}, Direction: InputPort, FixedType: testType}},
		Outputs: []PortSpec{{ID: PortID{Number: 1, Kind: OutputPort}, Direction: OutputPort, FixedType: testType}},
	}}}); err != nil {
		t.Fatal(err)
	}
	n1, _ := detachWorkspace.CreateNode("example.com/Source", NodeOptions{})
	n2, _ := detachWorkspace.CreateNode("example.com/Source", NodeOptions{})
	detachLink, _ := detachWorkspace.CreateLink(
		FullPortID{Node: n2, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: n1, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	detacher.link = detachLink
	detacher.delete = true
	detachWorkspace.nodes[n1].runtime = detacher
	detachWorkspace.nodes[n2].runtime = detacher
	if err := detachWorkspace.DeleteLink(detachLink); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete link after hook removed link = %v, want not found", err)
	}

	finalCleanup := NewWorkspace()
	finalDetacher := mutatingDetachNode{w: finalCleanup}
	if err := finalCleanup.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{
		Name:    "example.com/Source",
		Runtime: nodeClassFunc(func(NodeContext, NodeState, InitMode) (NodeRuntime, error) { return finalDetacher, nil }),
		Inputs:  []PortSpec{{ID: PortID{Number: 1, Kind: InputPort}, Direction: InputPort, FixedType: testType}},
		Outputs: []PortSpec{{ID: PortID{Number: 1, Kind: OutputPort}, Direction: OutputPort, FixedType: testType}},
	}}}); err != nil {
		t.Fatal(err)
	}
	f1, _ := finalCleanup.CreateNode("example.com/Source", NodeOptions{})
	f2, _ := finalCleanup.CreateNode("example.com/Source", NodeOptions{})
	f3, _ := finalCleanup.CreateNode("example.com/Source", NodeOptions{})
	fLink, _ := finalCleanup.CreateLink(FullPortID{Node: f2, Port: PortID{Number: 1, Kind: InputPort}}, FullPortID{Node: f1, Port: PortID{Number: 1, Kind: OutputPort}}, LinkOptions{})
	finalDetacher.add = &linkRecord{id: 999, input: FullPortID{Node: f3, Port: PortID{Number: 1, Kind: InputPort}}, output: FullPortID{Node: f1, Port: PortID{Number: 1, Kind: OutputPort}}, typ: testType}
	finalCleanup.nodes[f1].runtime = finalDetacher
	finalCleanup.nodes[f2].runtime = finalDetacher
	if err := finalCleanup.DeleteNode(f1); err != nil {
		t.Fatal(err)
	}
	if _, ok := finalCleanup.links[fLink]; ok {
		t.Fatal("original link should be deleted")
	}
	if _, ok := finalCleanup.links[999]; ok {
		t.Fatal("concurrently added attached link should be deleted by final cleanup")
	}
}

type deletingExportNode struct {
	w  *Workspace
	id NodeID
}

func (n deletingExportNode) ExportPrivateState() (any, error) {
	n.w.mu.Lock()
	delete(n.w.nodes, n.id)
	n.w.mu.Unlock()
	return "deleted", nil
}

func TestCoverageFinalReachableBranches(t *testing.T) {
	w, nodes, _ := lifecycleWorkspace(t, &lifecycleClass{})
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	c, _ := w.CreateNode("example.com/Source", NodeOptions{})
	link1, _ := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	nodes[b].failDetach = true
	if err := w.DeleteNode(a); err == nil {
		t.Fatal("expected delete node to surface attached link detach error")
	}
	nodes[b].failDetach = false
	w.links[link1].state = StateInactive
	w.links[100] = &linkRecord{id: 100, state: StateActive, input: FullPortID{Node: c, Port: PortID{Number: 1, Kind: InputPort}}, output: FullPortID{Node: b, Port: PortID{Number: 1, Kind: OutputPort}}, typ: testType}
	_, linkEvents := w.inactiveEventsForNodesLocked(map[NodeID]bool{a: true})
	if len(linkEvents) != 0 {
		t.Fatalf("inactive/unrelated link events = %#v, want none", linkEvents)
	}
	w.links[link1].state = StateActive
	w.links[101] = &linkRecord{id: 101, state: StateActive, input: FullPortID{Node: c, Port: PortID{Number: 1, Kind: InputPort}}, output: FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}}, typ: testType}
	_, linkEvents = w.inactiveEventsForNodesLocked(map[NodeID]bool{a: true})
	if len(linkEvents) != 2 || linkEvents[0].id != link1 || linkEvents[1].id != 101 {
		t.Fatalf("sorted link events = %#v", linkEvents)
	}

	closeRecall, closeNodes, _ := lifecycleWorkspace(t, &lifecycleClass{})
	closeNode, _ := closeRecall.CreateNode("example.com/Source", NodeOptions{})
	closeNodes[closeNode].panicOnClose = true
	if err := closeRecall.RecallClass("example.com", "example.com/Source"); err == nil {
		t.Fatal("expected recall close error")
	}
	closeUnregister, unregisterNodes, _ := lifecycleWorkspace(t, &lifecycleClass{})
	unregisterNode, _ := closeUnregister.CreateNode("example.com/Source", NodeOptions{})
	unregisterNodes[unregisterNode].panicOnClose = true
	if err := closeUnregister.UnregisterLibrary("example.com"); err == nil {
		t.Fatal("expected unregister close error")
	}

	copyRace := NewWorkspace()
	var deleting deletingExportNode
	if err := copyRace.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{
		Name: "example.com/DeleteOnExport",
		Runtime: nodeClassFunc(func(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
			deleting.w = copyRace
			deleting.id = ctx.ID
			return deleting, nil
		}),
	}}}); err != nil {
		t.Fatal(err)
	}
	copyNode, _ := copyRace.CreateNode("example.com/DeleteOnExport", NodeOptions{})
	if _, err := copyRace.Copy([]NodeID{copyNode}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("copy after export deleted node = %v, want not found", err)
	}

	pasteClose := NewWorkspace()
	closeRuntime := capturedWorkspaceClass{w: pasteClose, closeDuring: true}
	if err := pasteClose.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{Name: "example.com/ClosePaste", Runtime: closeRuntime}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := pasteClose.Paste(Clipboard{Nodes: []SaveNode{{ID: "1N", Class: "example.com/ClosePaste"}}}); !errors.Is(err, ErrClosed) {
		t.Fatalf("paste closed during init = %v, want closed", err)
	}
	pasteInactive := NewWorkspace()
	inactiveRuntime := capturedWorkspaceClass{w: pasteInactive, recallClass: true}
	if err := pasteInactive.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{Name: "example.com/InactivePaste", Runtime: inactiveRuntime}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := pasteInactive.Paste(Clipboard{Nodes: []SaveNode{{ID: "1N", Class: "example.com/InactivePaste"}}}); !errors.Is(err, ErrInactive) {
		t.Fatalf("paste inactive during init = %v, want inactive", err)
	}

	class := scopedTestClass("example.com", "Source")
	multi := NewWorkspace()
	if err := multi.RegisterLibrary(StaticLibrary{LibraryName: "b.com"}); err != nil {
		t.Fatal(err)
	}
	if err := multi.RegisterLibrary(StaticLibrary{LibraryName: "a.com"}); err != nil {
		t.Fatal(err)
	}
	if libs := multi.Snapshot().Libraries; len(libs) != 2 || libs[0].Name != "a.com" || libs[1].Name != "b.com" {
		t.Fatalf("sorted libraries = %#v", libs)
	}
	_ = class

	commitWorkspace, _ := testWorkspace(t)
	x, _ := commitWorkspace.CreateNode("example.com/Source", NodeOptions{})
	y, _ := commitWorkspace.CreateNode("example.com/Source", NodeOptions{})
	pending, err := commitWorkspace.prepareCreateLinkLocked(
		FullPortID{Node: y, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: x, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	pending.link.typ = ""
	if _, err := commitWorkspace.commitPreparedLinkLocked(pending); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("commit changed implicit type = %v, want type mismatch", err)
	}
	commitWorkspace.closed = true
	if _, err := commitWorkspace.prepareCreateLinkLocked(
		FullPortID{Node: y, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: x, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	); !errors.Is(err, ErrClosed) {
		t.Fatalf("prepare link closed = %v, want closed", err)
	}
	if _, err := commitWorkspace.commitPreparedLinkLocked(pending); !errors.Is(err, ErrClosed) {
		t.Fatalf("commit link closed = %v, want closed", err)
	}

	validateWorkspace, _ := testWorkspace(t)
	p, _ := validateWorkspace.CreateNode("example.com/Source", NodeOptions{})
	q, _ := validateWorkspace.CreateNode("example.com/Source", NodeOptions{})
	validateWorkspace.links[1] = &linkRecord{id: 1, input: FullPortID{Node: q, Port: PortID{Number: 99, Kind: InputPort}}, output: FullPortID{Node: p, Port: PortID{Number: 1, Kind: OutputPort}}, typ: testType}
	if err := validateWorkspace.validateAttachedLinksLocked(q); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("missing input attached validation = %v, want invalid port", err)
	}
	validateWorkspace.links[1] = &linkRecord{id: 1, input: FullPortID{Node: q, Port: PortID{Number: 1, Kind: InputPort}}, output: FullPortID{Node: p, Port: PortID{Number: 99, Kind: OutputPort}}, typ: testType}
	if err := validateWorkspace.validateAttachedLinksLocked(q); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("missing output attached validation = %v, want invalid port", err)
	}
	validateWorkspace.links[1] = &linkRecord{id: 1, input: FullPortID{Node: q, Port: PortID{Number: 99, Kind: InputPort}}, output: FullPortID{Node: p, Port: PortID{Number: 1, Kind: OutputPort}}, typ: testType}
	if err := validateWorkspace.validateAttachedLinksLocked(999); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("second-pass missing input validation = %v, want invalid port", err)
	}
	validateWorkspace.links[1] = &linkRecord{id: 1, input: FullPortID{Node: 999, Port: PortID{Number: 1, Kind: InputPort}}, output: FullPortID{Node: p, Port: PortID{Number: 1, Kind: OutputPort}}, typ: testType}
	if err := validateWorkspace.validateAttachedLinksLocked(123); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second-pass missing node validation = %v, want not found", err)
	}

	scopeRuntime := &nodeScopeClass{scopes: map[NodeID]NodeScope{}}
	scopeWorkspace := NewWorkspace()
	if err := scopeWorkspace.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{Name: "example.com/Scoped", Runtime: scopeRuntime}}}); err != nil {
		t.Fatal(err)
	}
	scopeNode, _ := scopeWorkspace.CreateNode("example.com/Scoped", NodeOptions{})
	if err := scopeRuntime.scopes[scopeNode].SetState(NodeState{DisplayName: "committed"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := scopeWorkspace.Node(scopeNode); got.Dynamic.DisplayName != "committed" {
		t.Fatalf("committed SetState = %#v", got.Dynamic)
	}

	failCreate := NewWorkspace()
	if err := failCreate.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{Name: "example.com/Fail", Runtime: &lifecycleClass{log: &[]string{}, fail: true}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := failCreate.CreateNode("example.com/Fail", NodeOptions{}); err == nil {
		t.Fatal("expected create node init error")
	}
	if err := failCreate.RecallClass("example.com", "example.com/Fail"); err != nil {
		t.Fatal(err)
	}
	if _, err := failCreate.CreateNode("example.com/Fail", NodeOptions{}); !errors.Is(err, ErrInactive) {
		t.Fatalf("create inactive class = %v, want inactive", err)
	}

	defineClose := NewWorkspace()
	if err := defineClose.RegisterLibrary(StaticLibrary{LibraryName: "example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := defineClose.Restore(SaveData{Nodes: []SaveNode{{ID: "1N", Class: "example.com/CloseDefine"}}}); err != nil {
		t.Fatal(err)
	}
	if err := defineClose.DefineClass("example.com", ClassSpec{Name: "example.com/CloseDefine", Runtime: capturedWorkspaceClass{w: defineClose, closeDuring: true}}); !errors.Is(err, ErrClosed) {
		t.Fatalf("define closed during init = %v, want closed", err)
	}

	pasteLinkErr, _ := testWorkspace(t)
	if _, _, err := pasteLinkErr.Paste(Clipboard{
		Nodes: []SaveNode{{ID: "1N", Class: "example.com/Source"}, {ID: "2N", Class: "example.com/Source"}},
		Links: []SaveLink{{Name: "1L:2N99i:1N1o", Type: testType}},
	}); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("paste link create error = %v, want invalid port", err)
	}

	initMetadata := &nodeScopeClass{scopes: map[NodeID]NodeScope{}, metadataKeyInInit: "only", metadataValueInInit: "value"}
	initWorkspace := NewWorkspace()
	if err := initWorkspace.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{Name: "example.com/Meta", Runtime: initMetadata}}}); err != nil {
		t.Fatal(err)
	}
	initNode, _ := initWorkspace.CreateNode("example.com/Meta", NodeOptions{})
	if err := initMetadata.scopes[initNode].DeleteMetadataValue("only"); err != nil {
		t.Fatal(err)
	}
	if got, _ := initWorkspace.Node(initNode); got.Dynamic.Metadata != nil {
		t.Fatalf("metadata after deleting last key = %#v, want nil", got.Dynamic.Metadata)
	}
	initPorts := &initScopeExerciseClass{}
	initPortsWorkspace := NewWorkspace()
	if err := initPortsWorkspace.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{Name: "example.com/Ports", Runtime: nodeClassFunc(func(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
		if err := ctx.Node.SetMetadataValue("only", "value"); err != nil {
			return nil, err
		}
		if err := ctx.Node.DeleteMetadataValue("only"); err != nil {
			return nil, err
		}
		if snap, ok := ctx.Node.Snapshot(); !ok || snap.Dynamic.Metadata != nil {
			return nil, fmt.Errorf("metadata after init delete = %#v", snap.Dynamic.Metadata)
		}
		if err := ctx.Node.SetPorts([]PortSpec{{ID: PortID{Number: 0, Kind: InputPort}, Direction: InputPort}}, nil); !errors.Is(err, ErrInvalidPort) {
			return nil, fmt.Errorf("invalid input SetPorts = %v", err)
		}
		if err := ctx.Node.SetPorts(nil, []PortSpec{{ID: PortID{Number: 1, Kind: InputPort}, Direction: OutputPort}}); !errors.Is(err, ErrInvalidPort) {
			return nil, fmt.Errorf("invalid output SetPorts = %v", err)
		}
		if err := ctx.Node.SetPorts([]PortSpec{{ID: PortID{Number: 1, Kind: InputPort}, Direction: InputPort}}, []PortSpec{{ID: PortID{Number: 1, Kind: OutputPort}, Direction: OutputPort}}); err != nil {
			return nil, err
		}
		return struct{}{}, nil
	})}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := initPortsWorkspace.CreateNode("example.com/Ports", NodeOptions{}); err != nil {
		t.Fatal(err)
	}
	_ = initPorts

	directScope := &nodeScope{w: NewWorkspace(), id: 1, initRec: &nodeRecord{id: 1}}
	if err := directScope.SetPorts([]PortSpec{{ID: PortID{Number: 0, Kind: InputPort}, Direction: InputPort}}, nil); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("direct init SetPorts invalid input = %v, want invalid port", err)
	}
	if err := directScope.SetPorts(nil, []PortSpec{{ID: PortID{Number: 1, Kind: InputPort}, Direction: OutputPort}}); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("direct init SetPorts invalid output = %v, want invalid port", err)
	}
}

type nodeClassFunc func(NodeContext, NodeState, InitMode) (NodeRuntime, error)

func (f nodeClassFunc) InitNode(ctx NodeContext, state NodeState, mode InitMode) (NodeRuntime, error) {
	return f(ctx, state, mode)
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
		"close:1",
		"init:new",
		"inactive-before:2:workspace-close",
		"inactive-after:2:workspace-close",
		"close:2",
	}
	if fmt.Sprint(*log) != fmt.Sprint(want) {
		t.Fatalf("log after delete = %#v, want %#v", *log, want)
	}
}

func TestLifecycleDeleteNodeHookOrderWithAttachedLink(t *testing.T) {
	w, _, log := lifecycleWorkspace(t, &lifecycleClass{})
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	if _, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	); err != nil {
		t.Fatal(err)
	}
	*log = nil
	if err := w.DeleteNode(b); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"delete-before:2",
		"detach-before:input:1",
		"detach-before:output:1",
		"detach-after:input:1",
		"detach-after:output:1",
		"delete-after:2",
		"close:2",
	}
	if fmt.Sprint(*log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", *log, want)
	}
}

func TestLifecycleCloseInactiveHookOrderWithAttachedLink(t *testing.T) {
	w, _, log := lifecycleWorkspace(t, &lifecycleClass{})
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	if _, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	); err != nil {
		t.Fatal(err)
	}
	*log = nil
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{
		"inactive-before:1:workspace-close",
		"inactive-before:2:workspace-close",
		"inactive-after:1:workspace-close",
		"inactive-after:2:workspace-close",
		"link-inactive:input:1:workspace-close",
		"link-inactive:output:1:workspace-close",
	}
	if len(*log) < len(wantPrefix) {
		t.Fatalf("log = %#v, want prefix %#v", *log, wantPrefix)
	}
	if fmt.Sprint((*log)[:len(wantPrefix)]) != fmt.Sprint(wantPrefix) {
		t.Fatalf("log prefix = %#v, want %#v", (*log)[:len(wantPrefix)], wantPrefix)
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
		"close:1",
		"close:2",
	}
	if fmt.Sprint(*log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", *log, want)
	}
	got, ok := w.Link(link)
	if !ok || got.State != StateInactive {
		t.Fatalf("link = %#v, ok %v; want inactive preserved link", got, ok)
	}
}

func TestLifecycleDefineClassPrunedLinkSendsDetachNotification(t *testing.T) {
	runtime := &lifecycleClass{}
	w, _, log := lifecycleWorkspace(t, runtime)
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
	if err := w.DefineClass("example.com", ClassSpec{
		Name:    "example.com/Source",
		Runtime: runtime,
		Inputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: InputPort},
			Name:      "in",
			Direction: InputPort,
			FixedType: "example.com/float",
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
	if _, ok := w.Link(link); ok {
		t.Fatal("incompatible link should be pruned")
	}
	want := []string{
		"detach-after:input:1",
		"detach-after:output:1",
	}
	if fmt.Sprint(*log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", *log, want)
	}
}

func TestLifecycleRegisterLibraryPrunedRecoveredLinkSendsDetachNotification(t *testing.T) {
	data := SaveData{
		NextNode: 3,
		NextLink: 2,
		Nodes: []SaveNode{
			{
				ID:    "1N",
				Class: "example.com/Source",
				Outputs: []PortSpec{{
					ID:        PortID{Number: 1, Kind: OutputPort},
					Name:      "out",
					Direction: OutputPort,
					FixedType: testType,
				}},
			},
			{
				ID:    "2N",
				Class: "example.com/Source",
				Inputs: []PortSpec{{
					ID:        PortID{Number: 1, Kind: InputPort},
					Name:      "in",
					Direction: InputPort,
					FixedType: testType,
				}},
			},
		},
		Links: []SaveLink{{
			Name: "1L:2N1i:1N1o",
			Type: testType,
		}},
	}
	w := NewWorkspace()
	if err := w.Restore(data); err != nil {
		t.Fatal(err)
	}
	log := []string{}
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{
		Name:    "example.com/Source",
		Runtime: &lifecycleClass{log: &log, nodes: map[NodeID]*lifecycleNode{}},
		Inputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: InputPort},
			Name:      "in",
			Direction: InputPort,
			FixedType: "example.com/float",
		}},
		Outputs: []PortSpec{{
			ID:        PortID{Number: 1, Kind: OutputPort},
			Name:      "out",
			Direction: OutputPort,
			FixedType: testType,
		}},
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.Link(1); ok {
		t.Fatal("incompatible recovered link should be pruned")
	}
	want := []string{
		"init:restore",
		"init:restore",
		"detach-after:input:1",
		"detach-after:output:1",
	}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("log = %#v, want %#v", log, want)
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
