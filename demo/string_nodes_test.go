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

func TestStringSplitRoutesEachOutput(t *testing.T) {
	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(StringLibrary{}); err != nil {
		t.Fatalf("RegisterLibrary() error = %v", err)
	}
	source, err := w.CreateNode(TextClass, pasta.NodeOptions{UseState: true, State: pasta.NodeState{
		DisplayName: "Text",
		PrimaryType: StringType,
		Private:     map[string]any{"value": "alpha beta gamma delta"},
	}})
	if err != nil {
		t.Fatalf("CreateNode(Text) error = %v", err)
	}
	split, err := w.CreateNode(SplitClass, pasta.NodeOptions{})
	if err != nil {
		t.Fatalf("CreateNode(Split) error = %v", err)
	}
	first, err := w.CreateNode(StringResultClass, pasta.NodeOptions{})
	if err != nil {
		t.Fatalf("CreateNode(first Result) error = %v", err)
	}
	second, err := w.CreateNode(StringResultClass, pasta.NodeOptions{})
	if err != nil {
		t.Fatalf("CreateNode(second Result) error = %v", err)
	}
	rest, err := w.CreateNode(StringResultClass, pasta.NodeOptions{})
	if err != nil {
		t.Fatalf("CreateNode(rest Result) error = %v", err)
	}

	link := func(inputNode, outputNode pasta.NodeID, outputPort pasta.PortID) {
		t.Helper()
		if _, err := w.CreateLink(
			pasta.FullPortID{Node: inputNode, Port: StringInput},
			pasta.FullPortID{Node: outputNode, Port: outputPort},
			pasta.LinkOptions{Type: StringType},
		); err != nil {
			t.Fatalf("CreateLink(%s <- %s:%s) error = %v", inputNode, outputNode, outputPort, err)
		}
	}
	link(split, source, StringOutput)
	link(first, split, StringOutput)
	link(second, split, StringPartOutput)
	link(rest, split, StringRestOutput)

	assertText(t, w, first, "alpha")
	assertText(t, w, second, "beta")
	assertText(t, w, rest, "gamma delta")

	menu, ok := w.NodeMenu(split)
	if !ok {
		t.Fatal("split menu missing")
	}
	if _, err := w.UpdateNodeMenuState(split, pasta.MenuStateUpdate{
		Version: menu.Version,
		Fields:  []pasta.MenuFieldUpdate{{Block: "main", Field: "separator", Value: "a "}},
	}); err != nil {
		t.Fatalf("UpdateNodeMenuState(split) error = %v", err)
	}
	assertText(t, w, first, "alph")
	assertText(t, w, second, "bet")
	assertText(t, w, rest, "gamma delta")
}

func TestStringNodeMessageButtons(t *testing.T) {
	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(StringLibrary{}); err != nil {
		t.Fatalf("RegisterLibrary() error = %v", err)
	}
	node, err := w.CreateNode(TextClass, pasta.NodeOptions{})
	if err != nil {
		t.Fatalf("CreateNode(Text) error = %v", err)
	}

	if err := w.TriggerNodeMenuButton(node, pasta.MenuButtonRef{Block: "main", Button: "message-note"}); err != nil {
		t.Fatalf("TriggerNodeMenuButton(note) error = %v", err)
	}
	if err := w.TriggerNodeMenuButton(node, pasta.MenuButtonRef{Block: "main", Button: "message-warn"}); err != nil {
		t.Fatalf("TriggerNodeMenuButton(warn) error = %v", err)
	}
	if err := w.TriggerNodeMenuButton(node, pasta.MenuButtonRef{Block: "main", Button: "message-err"}); err != nil {
		t.Fatalf("TriggerNodeMenuButton(err) error = %v", err)
	}
	messages := w.NodeMessages(node)
	if len(messages) != 3 {
		t.Fatalf("NodeMessages() len = %d, want 3: %#v", len(messages), messages)
	}
	if messages[0].Type != pasta.MessageNote || messages[1].Type != pasta.MessageWarn || messages[2].Type != pasta.MessageErr {
		t.Fatalf("NodeMessages() types = %#v", messages)
	}

	if err := w.TriggerNodeMenuButton(node, pasta.MenuButtonRef{Block: "main", Button: "messages-clear"}); err != nil {
		t.Fatalf("TriggerNodeMenuButton(clear) error = %v", err)
	}
	if got := w.NodeMessages(node); len(got) != 0 {
		t.Fatalf("NodeMessages() after clear = %#v, want none", got)
	}
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
