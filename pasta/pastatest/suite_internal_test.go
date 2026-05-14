package pastatest

import (
	"errors"
	"testing"

	"github.com/asciimoth/pasta/pasta"
)

type namedStaticLibrary struct {
	name    string
	classes []pasta.ClassSpec
}

func (l namedStaticLibrary) Name() string { return l.name }

func (l namedStaticLibrary) DefineClasses(scope pasta.LibraryScope) error {
	for _, class := range l.classes {
		if err := scope.DefineClass(class); err != nil {
			return err
		}
	}
	return nil
}

func TestSuiteAlternateSuccessBranches(t *testing.T) {
	source := pasta.ClassSpec{
		Name: "example.com/Source",
		Default: pasta.NodeState{
			DisplayName: "Source",
			Metadata:    map[string]string{"role": "source"},
		},
		Outputs: []pasta.PortSpec{{
			ID:        Output(1),
			Name:      "out",
			Direction: pasta.OutputPort,
			FixedType: "example.com/value",
			Metadata:  map[string]string{"side": "out"},
		}},
		Metadata: map[string]string{"kind": "source"},
	}
	sink := pasta.ClassSpec{
		Name: "example.com/Sink",
		Inputs: []pasta.PortSpec{{
			ID:        Input(1),
			Name:      "in",
			Direction: pasta.InputPort,
			FixedType: "example.com/value",
			Multiple:  true,
			Metadata:  map[string]string{"side": "in"},
		}},
		Metadata: map[string]string{"kind": "sink"},
	}
	passive := pasta.ClassSpec{Name: "example.com/Passive"}
	singleton := pasta.ClassSpec{Name: "example.com/Singleton", SingleNode: true}

	RunSuite(t, Suite{
		NewLibrary: func(*testing.T) pasta.Library {
			return namedStaticLibrary{name: "example.com", classes: []pasta.ClassSpec{source, sink, passive, singleton}}
		},
		Classes: []pasta.ClassSpec{{}, source, sink, passive, singleton},
		ClassCases: []ClassCase{
			{Name: source.Name, StrictDefaults: true},
			{Name: passive.Name, SkipCreate: true},
		},
		Links: []LinkCase{{
			Input:  Endpoint{Class: sink.Name, Port: Input(1)},
			Output: Endpoint{Class: source.Name, Port: Output(1)},
		}},
	})
}

func TestSuiteInternalHelpersSuccessBranches(t *testing.T) {
	RequireNoError(t, nil)
	RequireErrorIs(t, pasta.ErrNotFound, pasta.ErrNotFound)
	Require(t, true, "should not fail")

	classes := map[string]pasta.ClassSpec{
		"example.com/B": {Name: "example.com/B"},
		"example.com/A": {Name: "example.com/A"},
	}
	if got := firstClassName(classes); got != "example.com/A" {
		t.Fatalf("firstClassName = %q, want example.com/A", got)
	}
	if got := firstClassName(nil); got != "" {
		t.Fatalf("firstClassName nil = %q, want empty", got)
	}
	if _, ok := findPort(nil, Input(1)); ok {
		t.Fatal("findPort found a port in nil slice")
	}
	if _, ok := findPort([]pasta.PortSpec{{ID: Output(1)}}, Input(1)); ok {
		t.Fatal("findPort found the wrong port")
	}

	suite := StaticSuite("example.com", []pasta.ClassSpec{{Name: "example.com/A"}}, nil)
	suite.ClassCases = []ClassCase{{Name: "example.com/A", SkipCreate: true}}
	if cases := suite.classCases(); !cases["example.com/A"].SkipCreate {
		t.Fatalf("classCases = %#v, want skip case", cases)
	}

	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(pasta.StaticLibrary{LibraryName: "example.com", Classes: []pasta.ClassSpec{{Name: "example.com/Empty"}}}); err != nil {
		t.Fatal(err)
	}
	assertClassSnapshotDefensive(t, w, "example.com/Empty")
	node, err := w.CreateNode("example.com/Empty", pasta.NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertNodeSnapshotDefensive(t, w, node)

	if !errors.Is(pasta.ErrNotFound, pasta.ErrNotFound) {
		t.Fatal("errors.Is sanity check failed")
	}
}
