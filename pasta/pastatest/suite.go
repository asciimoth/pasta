package pastatest

import (
	"errors"
	"reflect"
	"testing"

	"github.com/asciimoth/pasta/pasta"
)

// Suite describes a Pasta library conformance run.
type Suite struct {
	// LibraryName is the expected library name. It may be left empty when
	// NewLibrary returns a non-nil library.
	LibraryName string

	// NewLibrary returns a fresh library instance for each subtest. Fresh
	// instances avoid coupling tests through library-owned mutable state.
	NewLibrary func(*testing.T) pasta.Library

	// Classes lists expected class specs. When NewLibrary is nil, Classes are
	// registered through pasta.StaticLibrary. When NewLibrary is set, Classes
	// are used as stricter expectations for names and recovery definitions.
	Classes []pasta.ClassSpec

	// ClassCases customizes individual class checks. Classes not listed here
	// are still checked with default options.
	ClassCases []ClassCase

	// Links lists representative valid graph edges to exercise link validation,
	// lifecycle hooks, save/restore, copy/paste, and inactive recovery.
	Links []LinkCase
}

// ClassCase customizes conformance checks for one class.
type ClassCase struct {
	Name string

	// StrictDefaults requires a newly created node to expose the class default
	// state and ports exactly. Leave false for runtimes that intentionally
	// customize nodes during InitNode through NodeScope.
	StrictDefaults bool

	// SkipCreate skips node creation checks for classes that need external
	// process state or configuration before they can initialize.
	SkipCreate bool
}

// Endpoint identifies a class port used in a link scenario.
type Endpoint struct {
	Class string
	Port  pasta.PortID
}

// LinkCase describes one valid link scenario.
type LinkCase struct {
	Name   string
	Input  Endpoint
	Output Endpoint

	// Type is optional when one endpoint has a fixed type. Supplying it also
	// verifies the explicit type path used by controller UIs.
	Type string
}

// StaticSuite builds a Suite for a statically defined library.
func StaticSuite(library string, classes []pasta.ClassSpec, links []LinkCase) Suite {
	return Suite{
		LibraryName: library,
		Classes:     classes,
		Links:       links,
	}
}

// Input returns the canonical input PortID for n.
func Input(n int64) pasta.PortID {
	return pasta.PortID{Number: n, Kind: pasta.InputPort}
}

// Output returns the canonical output PortID for n.
func Output(n int64) pasta.PortID {
	return pasta.PortID{Number: n, Kind: pasta.OutputPort}
}

// Full returns a FullPortID for a node and port.
func Full(node pasta.NodeID, port pasta.PortID) pasta.FullPortID {
	return pasta.FullPortID{Node: node, Port: port}
}

// RunSuite runs the standard library, class, node, link, persistence, scoped
// ownership, and recovery checks for a Pasta implementation.
func RunSuite(t *testing.T, suite Suite) {
	t.Helper()
	suite = normalizeSuite(t, suite)

	t.Run("registers classes", func(t *testing.T) {
		w, _, classes := newSuiteWorkspace(t, suite, false)
		snap := w.Snapshot()
		if len(snap.Libraries) != 1 || snap.Libraries[0].Name != suite.LibraryName || !snap.Libraries[0].Active {
			t.Fatalf("libraries = %#v, want active %q", snap.Libraries, suite.LibraryName)
		}
		for name := range classes {
			if !pasta.ValidClassName(suite.LibraryName, name) {
				t.Fatalf("class %q is not valid under library %q", name, suite.LibraryName)
			}
			got, ok := w.Class(name)
			if !ok || !got.Active || got.Library != suite.LibraryName {
				t.Fatalf("class %q snapshot = %#v, ok %v", name, got, ok)
			}
		}
	})

	classCases := suite.classCases()
	for _, class := range suite.classNames(t) {
		class := class
		cc := classCases[class]
		if cc.Name == "" {
			cc.Name = class
		}
		t.Run("class "+class, func(t *testing.T) {
			runClassCase(t, suite, cc)
		})
	}

	for _, link := range suite.Links {
		link := link
		name := link.Name
		if name == "" {
			name = link.Output.Class + " to " + link.Input.Class
		}
		t.Run("link "+name, func(t *testing.T) {
			runLinkCase(t, suite, link)
		})
	}

	t.Run("library scope ownership", func(t *testing.T) {
		runLibraryScopeCase(t, suite)
	})
}

func runClassCase(t *testing.T, suite Suite, cc ClassCase) {
	t.Helper()
	w, _, classes := newSuiteWorkspace(t, suite, false)
	spec, ok := classes[cc.Name]
	if !ok {
		t.Fatalf("suite class %q was not defined", cc.Name)
	}

	assertClassSnapshotDefensive(t, w, cc.Name)

	before := w.Save()
	if err := w.CanCreateNode(cc.Name); err != nil {
		t.Fatalf("CanCreateNode(%q): %v", cc.Name, err)
	}
	if !reflect.DeepEqual(before, w.Save()) {
		t.Fatalf("CanCreateNode(%q) mutated workspace", cc.Name)
	}
	if cc.SkipCreate {
		return
	}

	node, err := w.CreateNode(cc.Name, pasta.NodeOptions{})
	if err != nil {
		t.Fatalf("CreateNode(%q): %v", cc.Name, err)
	}
	snap, ok := w.Node(node)
	if !ok || snap.State != pasta.StateActive || snap.Class != cc.Name || snap.Library != suite.LibraryName {
		t.Fatalf("node snapshot = %#v, ok %v", snap, ok)
	}
	if cc.StrictDefaults {
		if !reflect.DeepEqual(snap.Dynamic, spec.Default) {
			t.Fatalf("node dynamic state = %#v, want class default %#v", snap.Dynamic, spec.Default)
		}
		if !reflect.DeepEqual(snap.Inputs, spec.Inputs) || !reflect.DeepEqual(snap.Outputs, spec.Outputs) {
			t.Fatalf("node ports = %#v/%#v, want %#v/%#v", snap.Inputs, snap.Outputs, spec.Inputs, spec.Outputs)
		}
	}
	assertNodeSnapshotDefensive(t, w, node)

	saved := w.Save()
	restored, _, _ := newSuiteWorkspace(t, suite, false)
	if err := restored.Restore(saved); err != nil {
		t.Fatalf("Restore single %q node: %v", cc.Name, err)
	}
	if _, ok := restored.Node(node); !ok {
		t.Fatalf("restored node %s not found", node)
	}

	clip, err := w.Copy([]pasta.NodeID{node})
	if err != nil {
		t.Fatalf("Copy single %q node: %v", cc.Name, err)
	}
	if spec.SingleNode {
		if err := w.CanCreateNode(cc.Name); !errors.Is(err, pasta.ErrMultiplicity) {
			t.Fatalf("CanCreateNode(%q) with existing single node = %v, want multiplicity", cc.Name, err)
		}
		beforePaste := w.Save()
		nodes, links, err := w.Paste(clip)
		if err != nil {
			t.Fatalf("Paste single-node %q over existing node = %v, want skipped duplicate", cc.Name, err)
		}
		if len(nodes) != 0 || len(links) != 0 {
			t.Fatalf("Paste single-node %q over existing node = nodes %#v links %#v, want skipped duplicate", cc.Name, nodes, links)
		}
		if !reflect.DeepEqual(beforePaste, w.Save()) {
			t.Fatalf("skipped Paste single-node %q mutated workspace", cc.Name)
		}
		return
	}
	nodes, links, err := w.Paste(clip)
	if err != nil {
		t.Fatalf("Paste single %q node: %v", cc.Name, err)
	}
	if len(nodes) != 1 || len(links) != 0 || nodes[0] == node {
		t.Fatalf("paste returned nodes=%#v links=%#v, want one remapped node and no links", nodes, links)
	}
}

func runLinkCase(t *testing.T, suite Suite, link LinkCase) {
	t.Helper()
	w, _, classes := newSuiteWorkspace(t, suite, false)
	if _, ok := classes[link.Input.Class]; !ok {
		t.Fatalf("input class %q was not defined", link.Input.Class)
	}
	if _, ok := classes[link.Output.Class]; !ok {
		t.Fatalf("output class %q was not defined", link.Output.Class)
	}

	outNode, err := w.CreateNode(link.Output.Class, pasta.NodeOptions{State: pasta.NodeState{Coordinate: "x:1"}})
	if err != nil {
		t.Fatalf("CreateNode output %q: %v", link.Output.Class, err)
	}
	inNode, err := w.CreateNode(link.Input.Class, pasta.NodeOptions{State: pasta.NodeState{Coordinate: "x:2"}})
	if err != nil {
		t.Fatalf("CreateNode input %q: %v", link.Input.Class, err)
	}
	input := Full(inNode, link.Input.Port)
	output := Full(outNode, link.Output.Port)

	before := w.Save()
	if err := w.CanCreateLink(input, output, link.Type); err != nil {
		t.Fatalf("CanCreateLink(%s, %s, %q): %v", input, output, link.Type, err)
	}
	if !reflect.DeepEqual(before, w.Save()) {
		t.Fatal("CanCreateLink mutated workspace")
	}

	id, err := w.CreateLink(input, output, pasta.LinkOptions{Type: link.Type, Waypoints: []string{"p1"}})
	if err != nil {
		t.Fatalf("CreateLink(%s, %s, %q): %v", input, output, link.Type, err)
	}
	got, ok := w.Link(id)
	if !ok || got.State != pasta.StateActive || got.Input != input || got.Output != output || len(got.Waypoints) != 1 || got.Waypoints[0] != "p1" {
		t.Fatalf("link snapshot = %#v, ok %v", got, ok)
	}
	if link.Type != "" && got.Type != link.Type {
		t.Fatalf("link type = %q, want %q", got.Type, link.Type)
	}
	got.Waypoints[0] = "mutated"
	again, _ := w.Link(id)
	if again.Waypoints[0] != "p1" {
		t.Fatalf("link waypoint snapshot leaked mutation: %#v", again.Waypoints)
	}
	if err := w.SetLinkWaypoints(id, []string{"p2", "p3"}); err != nil {
		t.Fatalf("SetLinkWaypoints: %v", err)
	}

	inSnap, _ := w.Node(inNode)
	if p, ok := findPort(inSnap.Inputs, link.Input.Port); ok && !p.Multiple {
		otherOut, err := w.CreateNode(link.Output.Class, pasta.NodeOptions{})
		if err != nil {
			t.Fatalf("CreateNode duplicate output: %v", err)
		}
		_, err = w.CreateLink(input, Full(otherOut, link.Output.Port), pasta.LinkOptions{Type: link.Type})
		if !errors.Is(err, pasta.ErrMultiplicity) {
			t.Fatalf("duplicate input link error = %v, want ErrMultiplicity", err)
		}
	}

	saved := w.Save()
	restored, _, _ := newSuiteWorkspace(t, suite, false)
	if err := restored.Restore(saved); err != nil {
		t.Fatalf("Restore linked workspace: %v", err)
	}
	if restoredLink, ok := restored.Link(id); !ok || restoredLink.State != pasta.StateActive || len(restoredLink.Waypoints) != 2 {
		t.Fatalf("restored link = %#v, ok %v", restoredLink, ok)
	}

	clip, err := w.Copy([]pasta.NodeID{outNode, inNode})
	if err != nil {
		t.Fatalf("Copy linked workspace: %v", err)
	}
	nodes, links, err := w.Paste(clip)
	if err != nil {
		t.Fatalf("Paste linked workspace: %v", err)
	}
	if len(nodes) != 2 || len(links) != 1 {
		t.Fatalf("paste returned nodes=%#v links=%#v, want two nodes and one link", nodes, links)
	}

	if err := w.RecallClass(suite.LibraryName, link.Input.Class); err != nil {
		t.Fatalf("RecallClass(%q): %v", link.Input.Class, err)
	}
	inactive, ok := w.Link(id)
	if !ok || inactive.State != pasta.StateInactive {
		t.Fatalf("recalled class should preserve inactive link, got %#v ok %v", inactive, ok)
	}
	if spec, ok := classes[link.Input.Class]; ok {
		if err := w.DefineClass(suite.LibraryName, spec); err != nil {
			t.Fatalf("DefineClass(%q) recovery: %v", link.Input.Class, err)
		}
		recovered, ok := w.Link(id)
		if !ok || recovered.State != pasta.StateActive {
			t.Fatalf("recovered link = %#v, ok %v", recovered, ok)
		}
	}
}

func runLibraryScopeCase(t *testing.T, suite Suite) {
	t.Helper()
	w, scope, classes := newSuiteWorkspace(t, suite, true)
	if scope == nil {
		t.Fatal("suite did not capture library scope")
	}
	className := firstClassName(classes)
	if className == "" {
		t.Fatal("suite library defined no classes")
	}
	if err := scope.CanCreateNode(className); err != nil {
		t.Fatalf("scoped CanCreateNode(%q): %v", className, err)
	}
	node, err := scope.CreateNode(className, pasta.NodeOptions{})
	if err != nil {
		t.Fatalf("scoped CreateNode(%q): %v", className, err)
	}
	if err := scope.SetNodeMetadataValue(node, "pastatest", "owned"); err != nil {
		t.Fatalf("scoped SetNodeMetadataValue: %v", err)
	}
	snap, ok := w.Node(node)
	if !ok || snap.Dynamic.Metadata["pastatest"] != "owned" {
		t.Fatalf("scoped metadata snapshot = %#v, ok %v", snap, ok)
	}

	otherClass := pasta.ClassSpec{Name: "pastatest-other.example/Other"}
	if err := w.RegisterLibrary(pasta.StaticLibrary{LibraryName: "pastatest-other.example", Classes: []pasta.ClassSpec{otherClass}}); err != nil {
		t.Fatalf("register foreign library: %v", err)
	}
	otherNode, err := w.CreateNode(otherClass.Name, pasta.NodeOptions{})
	if err != nil {
		t.Fatalf("create foreign node: %v", err)
	}
	if _, err := scope.CreateNode(otherClass.Name, pasta.NodeOptions{}); !errors.Is(err, pasta.ErrOwnership) {
		t.Fatalf("scoped CreateNode foreign error = %v, want ErrOwnership", err)
	}
	if err := scope.SetNodeMetadataValue(otherNode, "pastatest", "foreign"); !errors.Is(err, pasta.ErrOwnership) {
		t.Fatalf("scoped SetNodeMetadataValue foreign error = %v, want ErrOwnership", err)
	}
}

func normalizeSuite(t *testing.T, suite Suite) Suite {
	t.Helper()
	if suite.NewLibrary == nil {
		if suite.LibraryName == "" {
			t.Fatal("pastatest Suite needs LibraryName when NewLibrary is nil")
		}
		classes := append([]pasta.ClassSpec(nil), suite.Classes...)
		suite.NewLibrary = func(*testing.T) pasta.Library {
			return pasta.StaticLibrary{LibraryName: suite.LibraryName, Classes: classes}
		}
	}
	if suite.LibraryName == "" {
		lib := suite.NewLibrary(t)
		if lib == nil {
			t.Fatal("pastatest NewLibrary returned nil")
		}
		suite.LibraryName = lib.Name()
	}
	return suite
}

func newSuiteWorkspace(t *testing.T, suite Suite, capture bool) (*pasta.Workspace, pasta.LibraryScope, map[string]pasta.ClassSpec) {
	t.Helper()
	lib := suite.NewLibrary(t)
	if lib == nil {
		t.Fatal("pastatest NewLibrary returned nil")
	}
	if lib.Name() != suite.LibraryName {
		t.Fatalf("library name = %q, want %q", lib.Name(), suite.LibraryName)
	}
	var rec *scopeRecorder
	if capture {
		rec = &scopeRecorder{inner: lib}
		lib = rec
	}
	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(lib); err != nil {
		t.Fatalf("RegisterLibrary(%q): %v", suite.LibraryName, err)
	}
	classes := make(map[string]pasta.ClassSpec)
	for _, class := range w.ClassesByLibrary(suite.LibraryName) {
		if class.Active {
			classes[class.Spec.Name] = class.Spec
		}
	}
	for _, expected := range suite.Classes {
		if expected.Name == "" {
			continue
		}
		if _, ok := classes[expected.Name]; !ok {
			t.Fatalf("expected class %q was not defined by %q", expected.Name, suite.LibraryName)
		}
	}
	var scope pasta.LibraryScope
	if rec != nil {
		scope = rec.scope
	}
	return w, scope, classes
}

type scopeRecorder struct {
	inner pasta.Library
	scope pasta.LibraryScope
}

func (r *scopeRecorder) Name() string { return r.inner.Name() }

func (r *scopeRecorder) DefineClasses(scope pasta.LibraryScope) error {
	r.scope = scope
	return r.inner.DefineClasses(scope)
}

func (s Suite) classCases() map[string]ClassCase {
	out := make(map[string]ClassCase)
	for _, cc := range s.ClassCases {
		out[cc.Name] = cc
	}
	return out
}

func (s Suite) classNames(t *testing.T) []string {
	t.Helper()
	w, _, _ := newSuiteWorkspace(t, s, false)
	classes := w.ClassesByLibrary(s.LibraryName)
	names := make([]string, 0, len(classes))
	for _, class := range classes {
		if class.Active {
			names = append(names, class.Spec.Name)
		}
	}
	return names
}

func assertClassSnapshotDefensive(t *testing.T, w *pasta.Workspace, name string) {
	t.Helper()
	snap, ok := w.Class(name)
	if !ok {
		t.Fatalf("Class(%q) not found", name)
	}
	mutated := false
	if snap.Spec.Metadata != nil {
		snap.Spec.Metadata["pastatest"] = "mutated"
		mutated = true
	}
	if snap.Spec.Default.Metadata != nil {
		snap.Spec.Default.Metadata["pastatest"] = "mutated"
		mutated = true
	}
	if len(snap.Spec.Inputs) > 0 {
		snap.Spec.Inputs[0].Name = "pastatest-mutated"
		mutated = true
	}
	if len(snap.Spec.Outputs) > 0 {
		snap.Spec.Outputs[0].Name = "pastatest-mutated"
		mutated = true
	}
	if !mutated {
		return
	}
	next, _ := w.Class(name)
	if next.Spec.Metadata["pastatest"] == "mutated" ||
		next.Spec.Default.Metadata["pastatest"] == "mutated" ||
		(len(next.Spec.Inputs) > 0 && next.Spec.Inputs[0].Name == "pastatest-mutated") ||
		(len(next.Spec.Outputs) > 0 && next.Spec.Outputs[0].Name == "pastatest-mutated") {
		t.Fatalf("Class(%q) returned mutable internal state: %#v", name, next.Spec)
	}
}

func assertNodeSnapshotDefensive(t *testing.T, w *pasta.Workspace, id pasta.NodeID) {
	t.Helper()
	snap, ok := w.Node(id)
	if !ok {
		t.Fatalf("Node(%s) not found", id)
	}
	mutated := false
	if snap.Dynamic.Metadata != nil {
		snap.Dynamic.Metadata["pastatest"] = "mutated"
		mutated = true
	}
	if len(snap.Inputs) > 0 {
		snap.Inputs[0].Name = "pastatest-mutated"
		mutated = true
	}
	if len(snap.Outputs) > 0 {
		snap.Outputs[0].Name = "pastatest-mutated"
		mutated = true
	}
	if !mutated {
		return
	}
	next, _ := w.Node(id)
	if next.Dynamic.Metadata["pastatest"] == "mutated" ||
		(len(next.Inputs) > 0 && next.Inputs[0].Name == "pastatest-mutated") ||
		(len(next.Outputs) > 0 && next.Outputs[0].Name == "pastatest-mutated") {
		t.Fatalf("Node(%s) returned mutable internal state: %#v", id, next)
	}
}

func findPort(ports []pasta.PortSpec, id pasta.PortID) (pasta.PortSpec, bool) {
	for _, port := range ports {
		if port.ID == id {
			return port, true
		}
	}
	return pasta.PortSpec{}, false
}

func firstClassName(classes map[string]pasta.ClassSpec) string {
	var out string
	for name := range classes {
		if out == "" || name < out {
			out = name
		}
	}
	return out
}

// RequireNoError fails the current test when err is non-nil.
func RequireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// RequireErrorIs fails the current test unless err wraps target.
func RequireErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want %v", err, target)
	}
}

// Require reports a formatted test failure when condition is false.
func Require(t *testing.T, condition bool, format string, args ...any) {
	t.Helper()
	if !condition {
		t.Fatalf(format, args...)
	}
}
