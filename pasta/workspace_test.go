package pasta

import (
	"errors"
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
