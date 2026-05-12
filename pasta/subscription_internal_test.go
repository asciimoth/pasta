package pasta

import (
	"errors"
	"testing"
)

func TestSubscriptionNilAndCloseBranches(t *testing.T) {
	var messageSub *MessageSubscription
	if messageSub.Events() != nil {
		t.Fatal("nil message subscription returned non-nil events")
	}
	messageSub.Close()

	var menuSub *MenuSubscription
	if menuSub.Events() != nil {
		t.Fatal("nil menu subscription returned non-nil events")
	}
	menuSub.Close()

	w, _ := testWorkspace(t)
	w.mu.Lock()
	w.watchers = nil
	w.menuWatchers = nil
	w.mu.Unlock()
	msg := w.WatchMessages(-1)
	msg.Close()
	msg.Close()
	msg.send(MessageEvent{})

	menu := w.WatchMenus(-1)
	menu.Close()
	menu.Close()
	menu.send(MenuEvent{})
}

func TestMessageAndMenuWorkspaceErrorBranches(t *testing.T) {
	w, _ := testWorkspace(t)
	node, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	other, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.AddNodeMessage(999, MessageNote, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AddNodeMessage missing node error = %v, want not found", err)
	}
	if _, err := w.AddNodeMessage(node, MessageType("bad"), "bad"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("AddNodeMessage invalid type error = %v, want invalid name", err)
	}
	message, err := w.AddNodeMessage(node, MessageNote, "note")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.RemoveNodeMessage(999, message); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RemoveNodeMessage missing node error = %v, want not found", err)
	}
	if err := w.RemoveNodeMessage(other, message); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RemoveNodeMessage wrong node error = %v, want not found", err)
	}

	if err := w.SetNodeMenu(999, NodeMenu{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetNodeMenu missing node error = %v, want not found", err)
	}
	if err := w.SetNodeMenu(node, NodeMenu{Blocks: []MenuBlock{{ID: ""}}}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("SetNodeMenu invalid menu error = %v, want invalid name", err)
	}
	if err := w.ClearNodeMenu(999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ClearNodeMenu missing node error = %v, want not found", err)
	}
	if err := w.ClearNodeMenu(node); err != nil {
		t.Fatalf("ClearNodeMenu without menu error = %v", err)
	}
	if _, err := w.UpdateNodeMenuState(999, MenuStateUpdate{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateNodeMenuState missing node error = %v, want not found", err)
	}
	if _, err := w.UpdateNodeMenuState(node, MenuStateUpdate{Version: -1}); !errors.Is(err, ErrInvalidMenu) {
		t.Fatalf("UpdateNodeMenuState invalid update error = %v, want invalid menu", err)
	}
	if _, err := w.UpdateNodeMenuState(node, MenuStateUpdate{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateNodeMenuState without menu error = %v, want not found", err)
	}
	if err := w.TriggerNodeMenuButton(node, MenuButtonRef{Block: "bad id", Button: "run"}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("TriggerNodeMenuButton invalid ref error = %v, want invalid name", err)
	}
	if err := w.TriggerNodeMenuButton(999, MenuButtonRef{Block: "main", Button: "run"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("TriggerNodeMenuButton missing node error = %v, want not found", err)
	}
	if err := w.TriggerNodeMenuButton(node, MenuButtonRef{Block: "main", Button: "run"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("TriggerNodeMenuButton without menu error = %v, want not found", err)
	}

	if err := w.SetNodeMenu(node, NodeMenu{Blocks: []MenuBlock{{ID: "main", Buttons: []MenuButton{{ID: "disabled", Disabled: true}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := w.TriggerNodeMenuButton(node, MenuButtonRef{Block: "main", Button: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("TriggerNodeMenuButton missing button error = %v, want not found", err)
	}
	if err := w.TriggerNodeMenuButton(node, MenuButtonRef{Block: "main", Button: "disabled"}); !errors.Is(err, ErrInactive) {
		t.Fatalf("TriggerNodeMenuButton disabled button error = %v, want inactive", err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.AddNodeMessage(node, MessageNote, "closed"); !errors.Is(err, ErrClosed) {
		t.Fatalf("AddNodeMessage closed error = %v, want closed", err)
	}
	if err := w.RemoveNodeMessage(node, message); !errors.Is(err, ErrClosed) {
		t.Fatalf("RemoveNodeMessage closed error = %v, want closed", err)
	}
	if err := w.SetNodeMenu(node, NodeMenu{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetNodeMenu closed error = %v, want closed", err)
	}
	if err := w.ClearNodeMenu(node); !errors.Is(err, ErrClosed) {
		t.Fatalf("ClearNodeMenu closed error = %v, want closed", err)
	}
	if _, err := w.UpdateNodeMenuState(node, MenuStateUpdate{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("UpdateNodeMenuState closed error = %v, want closed", err)
	}
	if err := w.TriggerNodeMenuButton(node, MenuButtonRef{Block: "main", Button: "run"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("TriggerNodeMenuButton closed error = %v, want closed", err)
	}
}
