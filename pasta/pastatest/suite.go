package pastatest

import (
	"testing"

	check "github.com/asciimoth/pasta/internal/pastatestcheck"
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
		check.Require(t, len(snap.Libraries) == 1 && snap.Libraries[0].Name == suite.LibraryName && snap.Libraries[0].Active,
			"libraries = %#v, want active %q", snap.Libraries, suite.LibraryName)
		for name := range classes {
			check.Require(t, pasta.ValidClassName(suite.LibraryName, name),
				"class %q is not valid under library %q", name, suite.LibraryName)
			got, ok := w.Class(name)
			check.Require(t, ok && got.Active && got.Library == suite.LibraryName,
				"class %q snapshot = %#v, ok %v", name, got, ok)
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
	check.Require(t, ok, "suite class %q was not defined", cc.Name)

	assertClassSnapshotDefensive(t, w, cc.Name)

	before := w.Save()
	err := w.CanCreateNode(cc.Name)
	check.NoError(t, err, "CanCreateNode(%q): %v", cc.Name)
	check.DeepEqual(t, before, w.Save(), "CanCreateNode(%q) mutated workspace", cc.Name)
	if cc.SkipCreate {
		return
	}

	node, err := w.CreateNode(cc.Name, pasta.NodeOptions{})
	check.NoError(t, err, "CreateNode(%q): %v", cc.Name)
	snap, ok := w.Node(node)
	check.Require(t, ok && snap.State == pasta.StateActive && snap.Class == cc.Name && snap.Library == suite.LibraryName,
		"node snapshot = %#v, ok %v", snap, ok)
	if cc.StrictDefaults {
		check.DeepEqual(t, snap.Dynamic, spec.Default,
			"node dynamic state = %#v, want class default %#v", snap.Dynamic, spec.Default)
		check.Require(t, len(snap.Inputs) == len(spec.Inputs) && len(snap.Outputs) == len(spec.Outputs),
			"node ports = %#v/%#v, want %#v/%#v", snap.Inputs, snap.Outputs, spec.Inputs, spec.Outputs)
		check.DeepEqual(t, snap.Inputs, spec.Inputs,
			"node ports = %#v/%#v, want %#v/%#v", snap.Inputs, snap.Outputs, spec.Inputs, spec.Outputs)
		check.DeepEqual(t, snap.Outputs, spec.Outputs,
			"node ports = %#v/%#v, want %#v/%#v", snap.Inputs, snap.Outputs, spec.Inputs, spec.Outputs)
	}
	assertNodeSnapshotDefensive(t, w, node)

	saved := w.Save()
	restored, _, _ := newSuiteWorkspace(t, suite, false)
	err = restored.Restore(saved)
	check.NoError(t, err, "Restore single %q node: %v", cc.Name)
	_, ok = restored.Node(node)
	check.Require(t, ok, "restored node %s not found", node)

	clip, err := w.Copy([]pasta.NodeID{node})
	check.NoError(t, err, "Copy single %q node: %v", cc.Name)
	if spec.SingleNode {
		err = w.CanCreateNode(cc.Name)
		check.ErrorIs(t, err, pasta.ErrMultiplicity, "CanCreateNode(%q) with existing single node = %v, want multiplicity", cc.Name)
		beforePaste := w.Save()
		nodes, links, err := w.Paste(clip)
		check.NoError(t, err, "Paste single-node %q over existing node = %v, want skipped duplicate", cc.Name)
		check.Require(t, len(nodes) == 0 && len(links) == 0,
			"Paste single-node %q over existing node = nodes %#v links %#v, want skipped duplicate", cc.Name, nodes, links)
		check.DeepEqual(t, beforePaste, w.Save(), "skipped Paste single-node %q mutated workspace", cc.Name)
		return
	}
	nodes, links, err := w.Paste(clip)
	check.NoError(t, err, "Paste single %q node: %v", cc.Name)
	check.Require(t, len(nodes) == 1 && len(links) == 0 && nodes[0] != node,
		"paste returned nodes=%#v links=%#v, want one remapped node and no links", nodes, links)
}

func runLinkCase(t *testing.T, suite Suite, link LinkCase) {
	t.Helper()
	w, _, classes := newSuiteWorkspace(t, suite, false)
	_, ok := classes[link.Input.Class]
	check.Require(t, ok, "input class %q was not defined", link.Input.Class)
	_, ok = classes[link.Output.Class]
	check.Require(t, ok, "output class %q was not defined", link.Output.Class)

	outNode, err := w.CreateNode(link.Output.Class, pasta.NodeOptions{State: pasta.NodeState{Coordinate: "x:1"}})
	check.NoError(t, err, "CreateNode output %q: %v", link.Output.Class)
	inNode, err := w.CreateNode(link.Input.Class, pasta.NodeOptions{State: pasta.NodeState{Coordinate: "x:2"}})
	check.NoError(t, err, "CreateNode input %q: %v", link.Input.Class)
	input := Full(inNode, link.Input.Port)
	output := Full(outNode, link.Output.Port)

	before := w.Save()
	err = w.CanCreateLink(input, output, link.Type)
	check.NoError(t, err, "CanCreateLink(%s, %s, %q): %v", input, output, link.Type)
	check.DeepEqual(t, before, w.Save(), "CanCreateLink mutated workspace")

	id, err := w.CreateLink(input, output, pasta.LinkOptions{Type: link.Type, Waypoints: []string{"p1"}})
	check.NoError(t, err, "CreateLink(%s, %s, %q): %v", input, output, link.Type)
	got, ok := w.Link(id)
	check.Require(t, ok && got.State == pasta.StateActive && got.Input == input && got.Output == output && len(got.Waypoints) == 1 && got.Waypoints[0] == "p1",
		"link snapshot = %#v, ok %v", got, ok)
	check.Require(t, link.Type == "" || got.Type == link.Type, "link type = %q, want %q", got.Type, link.Type)
	got.Waypoints[0] = "mutated"
	again, _ := w.Link(id)
	check.Require(t, again.Waypoints[0] == "p1",
		"link waypoint snapshot leaked mutation: %#v", again.Waypoints)
	err = w.SetLinkWaypoints(id, []string{"p2", "p3"})
	check.NoError(t, err, "SetLinkWaypoints: %v")

	inSnap, _ := w.Node(inNode)
	if p, ok := findPort(inSnap.Inputs, link.Input.Port); ok && !p.Multiple {
		otherOut, err := w.CreateNode(link.Output.Class, pasta.NodeOptions{})
		check.NoError(t, err, "CreateNode duplicate output: %v")
		_, err = w.CreateLink(input, Full(otherOut, link.Output.Port), pasta.LinkOptions{Type: link.Type})
		check.ErrorIs(t, err, pasta.ErrMultiplicity, "duplicate input link error = %v, want ErrMultiplicity")
	}

	saved := w.Save()
	restored, _, _ := newSuiteWorkspace(t, suite, false)
	err = restored.Restore(saved)
	check.NoError(t, err, "Restore linked workspace: %v")
	restoredLink, ok := restored.Link(id)
	check.Require(t, ok && restoredLink.State == pasta.StateActive && len(restoredLink.Waypoints) == 2,
		"restored link = %#v, ok %v", restoredLink, ok)

	clip, err := w.Copy([]pasta.NodeID{outNode, inNode})
	check.NoError(t, err, "Copy linked workspace: %v")
	nodes, links, err := w.Paste(clip)
	check.NoError(t, err, "Paste linked workspace: %v")
	check.Require(t, len(nodes) == 2 && len(links) == 1,
		"paste returned nodes=%#v links=%#v, want two nodes and one link", nodes, links)

	err = w.RecallClass(suite.LibraryName, link.Input.Class)
	check.NoError(t, err, "RecallClass(%q): %v", link.Input.Class)
	inactive, ok := w.Link(id)
	check.Require(t, ok && inactive.State == pasta.StateInactive,
		"recalled class should preserve inactive link, got %#v ok %v", inactive, ok)
	spec := classes[link.Input.Class]
	err = w.DefineClass(suite.LibraryName, spec)
	check.NoError(t, err, "DefineClass(%q) recovery: %v", link.Input.Class)
	recovered, ok := w.Link(id)
	check.Require(t, ok && recovered.State == pasta.StateActive,
		"recovered link = %#v, ok %v", recovered, ok)
}

func runLibraryScopeCase(t *testing.T, suite Suite) {
	t.Helper()
	w, scope, classes := newSuiteWorkspace(t, suite, true)
	check.Require(t, scope != nil, "suite did not capture library scope")
	className := firstClassName(classes)
	check.Require(t, className != "", "suite library defined no classes")
	err := scope.CanCreateNode(className)
	check.NoError(t, err, "scoped CanCreateNode(%q): %v", className)
	node, err := scope.CreateNode(className, pasta.NodeOptions{})
	check.NoError(t, err, "scoped CreateNode(%q): %v", className)
	err = scope.SetNodeMetadataValue(node, "pastatest", "owned")
	check.NoError(t, err, "scoped SetNodeMetadataValue: %v")
	snap, ok := w.Node(node)
	check.Require(t, ok && snap.Dynamic.Metadata["pastatest"] == "owned",
		"scoped metadata snapshot = %#v, ok %v", snap, ok)

	otherClass := pasta.ClassSpec{Name: "pastatest-other.example/Other"}
	err = w.RegisterLibrary(pasta.StaticLibrary{LibraryName: "pastatest-other.example", Classes: []pasta.ClassSpec{otherClass}})
	check.NoError(t, err, "register foreign library: %v")
	otherNode, err := w.CreateNode(otherClass.Name, pasta.NodeOptions{})
	check.NoError(t, err, "create foreign node: %v")
	_, err = scope.CreateNode(otherClass.Name, pasta.NodeOptions{})
	check.ErrorIs(t, err, pasta.ErrOwnership, "scoped CreateNode foreign error = %v, want ErrOwnership")
	err = scope.SetNodeMetadataValue(otherNode, "pastatest", "foreign")
	check.ErrorIs(t, err, pasta.ErrOwnership, "scoped SetNodeMetadataValue foreign error = %v, want ErrOwnership")
}

func normalizeSuite(t *testing.T, suite Suite) Suite {
	t.Helper()
	if suite.NewLibrary == nil {
		check.Require(t, suite.LibraryName != "", "pastatest Suite needs LibraryName when NewLibrary is nil")
		classes := append([]pasta.ClassSpec(nil), suite.Classes...)
		suite.NewLibrary = func(*testing.T) pasta.Library {
			return pasta.StaticLibrary{LibraryName: suite.LibraryName, Classes: classes}
		}
	}
	if suite.LibraryName == "" {
		lib := suite.NewLibrary(t)
		check.Require(t, lib != nil, "pastatest NewLibrary returned nil")
		suite.LibraryName = lib.Name()
	}
	return suite
}

func newSuiteWorkspace(t *testing.T, suite Suite, capture bool) (*pasta.Workspace, pasta.LibraryScope, map[string]pasta.ClassSpec) {
	t.Helper()
	lib := suite.NewLibrary(t)
	check.Require(t, lib != nil, "pastatest NewLibrary returned nil")
	check.Require(t, lib.Name() == suite.LibraryName, "library name = %q, want %q", lib.Name(), suite.LibraryName)
	var rec *scopeRecorder
	if capture {
		rec = &scopeRecorder{inner: lib}
		lib = rec
	}
	w := pasta.NewWorkspace()
	err := w.RegisterLibrary(lib)
	check.NoError(t, err, "RegisterLibrary(%q): %v", suite.LibraryName)
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
		_, ok := classes[expected.Name]
		check.Require(t, ok, "expected class %q was not defined by %q", expected.Name, suite.LibraryName)
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
	check.Require(t, ok, "Class(%q) not found", name)
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
	leaked := next.Spec.Metadata["pastatest"] == "mutated" ||
		next.Spec.Default.Metadata["pastatest"] == "mutated" ||
		(len(next.Spec.Inputs) > 0 && next.Spec.Inputs[0].Name == "pastatest-mutated") ||
		(len(next.Spec.Outputs) > 0 && next.Spec.Outputs[0].Name == "pastatest-mutated")
	check.Require(t, !leaked,
		"Class(%q) returned mutable internal state: %#v", name, next.Spec)
}

func assertNodeSnapshotDefensive(t *testing.T, w *pasta.Workspace, id pasta.NodeID) {
	t.Helper()
	snap, ok := w.Node(id)
	check.Require(t, ok, "Node(%s) not found", id)
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
	leaked := next.Dynamic.Metadata["pastatest"] == "mutated" ||
		(len(next.Inputs) > 0 && next.Inputs[0].Name == "pastatest-mutated") ||
		(len(next.Outputs) > 0 && next.Outputs[0].Name == "pastatest-mutated")
	check.Require(t, !leaked,
		"Node(%s) returned mutable internal state: %#v", id, next)
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
	check.NoError(t, err, "%v")
}

// RequireErrorIs fails the current test unless err wraps target.
func RequireErrorIs(t *testing.T, err, target error) {
	t.Helper()
	check.ErrorIs(t, err, target, "error = %v, want "+target.Error())
}

// Require reports a formatted test failure when condition is false.
func Require(t *testing.T, condition bool, format string, args ...any) {
	t.Helper()
	check.Require(t, condition, format, args...)
}
