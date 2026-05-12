package main

import (
	"testing"

	"github.com/asciimoth/pasta/pasta"
)

func TestStringLibraryPushFlow(t *testing.T) {
	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(StringLibrary{}); err != nil {
		t.Fatalf("RegisterLibrary() error = %v", err)
	}
	source, err := w.CreateNode(TextClass, pasta.NodeOptions{UseState: true, State: pasta.NodeState{
		DisplayName: "Text",
		PrimaryType: StringType,
		Private:     map[string]any{"value": " hello pasta "},
	}})
	if err != nil {
		t.Fatalf("CreateNode(Text) error = %v", err)
	}
	trim, err := w.CreateNode(TrimClass, pasta.NodeOptions{})
	if err != nil {
		t.Fatalf("CreateNode(Trim) error = %v", err)
	}
	upper, err := w.CreateNode(UppercaseClass, pasta.NodeOptions{})
	if err != nil {
		t.Fatalf("CreateNode(Uppercase) error = %v", err)
	}
	replace, err := w.CreateNode(ReplaceClass, pasta.NodeOptions{})
	if err != nil {
		t.Fatalf("CreateNode(Replace) error = %v", err)
	}
	result, err := w.CreateNode(StringResultClass, pasta.NodeOptions{})
	if err != nil {
		t.Fatalf("CreateNode(Result) error = %v", err)
	}

	link := func(inputNode, outputNode pasta.NodeID) {
		t.Helper()
		if _, err := w.CreateLink(
			pasta.FullPortID{Node: inputNode, Port: StringInput},
			pasta.FullPortID{Node: outputNode, Port: StringOutput},
			pasta.LinkOptions{Type: StringType},
		); err != nil {
			t.Fatalf("CreateLink(%s <- %s) error = %v", inputNode, outputNode, err)
		}
	}
	link(trim, source)
	link(upper, trim)
	link(replace, upper)
	link(result, replace)

	assertText(t, w, result, "HELLO Pasta")

	menu, ok := w.NodeMenu(source)
	if !ok {
		t.Fatal("source menu missing")
	}
	if _, err := w.UpdateNodeMenuState(source, pasta.MenuStateUpdate{
		Version: menu.Version,
		Fields:  []pasta.MenuFieldUpdate{{Block: "main", Field: "value", Value: " pasta demo "}},
	}); err != nil {
		t.Fatalf("UpdateNodeMenuState(source) error = %v", err)
	}
	assertText(t, w, result, "Pasta DEMO")

	menu, ok = w.NodeMenu(replace)
	if !ok {
		t.Fatal("replace menu missing")
	}
	if _, err := w.UpdateNodeMenuState(replace, pasta.MenuStateUpdate{
		Version: menu.Version,
		Fields:  []pasta.MenuFieldUpdate{{Block: "main", Field: "replacement", Value: "PASTA"}},
	}); err != nil {
		t.Fatalf("UpdateNodeMenuState(replace) error = %v", err)
	}
	assertText(t, w, result, "PASTA DEMO")
}

func assertText(t *testing.T, w *pasta.Workspace, node pasta.NodeID, want string) {
	t.Helper()
	snap, ok := w.Node(node)
	if !ok {
		t.Fatalf("Node(%s) missing", node)
	}
	if got := stringStateFromAny(snap.Dynamic.Private).Value; got != want {
		t.Fatalf("node %s text = %q, want %q", node, got, want)
	}
}
