package pasta

import (
	"errors"
	"fmt"
	"testing"
)

type configurableMenuRuntime struct {
	update      MenuStateUpdate
	updateErr   error
	buttonErr   error
	buttonCalls int
	onUpdate    func()
	onButton    func()
}

func (r *configurableMenuRuntime) ApplyMenuUpdate(MenuStateUpdate) (MenuStateUpdate, error) {
	if r.onUpdate != nil {
		r.onUpdate()
	}
	return r.update, r.updateErr
}

func (r *configurableMenuRuntime) TriggerMenuButton(MenuButtonRef) error {
	r.buttonCalls++
	if r.onButton != nil {
		r.onButton()
	}
	return r.buttonErr
}

type configurableMenuClass struct {
	runtime *configurableMenuRuntime
	init    func(NodeContext) error
}

func (c configurableMenuClass) InitNode(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
	if c.init != nil {
		if err := c.init(ctx); err != nil {
			return nil, err
		}
	}
	return c.runtime, nil
}

func simpleEditableMenu() NodeMenu {
	return NodeMenu{Blocks: []MenuBlock{{ID: "main", Fields: []MenuField{{ID: "name", Kind: MenuFieldString, Value: "old"}}, Buttons: []MenuButton{{ID: "run"}}}}}
}

func workspaceWithMenuRuntime(t *testing.T, runtime *configurableMenuRuntime, init func(NodeContext) error) (*Workspace, NodeID) {
	t.Helper()
	w := NewWorkspace()
	class := ClassSpec{Name: "example.com/Menu", Runtime: configurableMenuClass{runtime: runtime, init: init}}
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{class}}); err != nil {
		t.Fatal(err)
	}
	node, err := w.CreateNode(class.Name, NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return w, node
}

func TestWorkspaceMenuHookBranches(t *testing.T) {
	t.Run("hook error", func(t *testing.T) {
		runtime := &configurableMenuRuntime{updateErr: errors.New("hook failed")}
		w, node := workspaceWithMenuRuntime(t, runtime, nil)
		if err := w.SetNodeMenu(node, simpleEditableMenu()); err != nil {
			t.Fatal(err)
		}
		if _, err := w.UpdateNodeMenuState(node, MenuStateUpdate{Version: 1, Fields: []MenuFieldUpdate{{Block: "main", Field: "name", Value: "new"}}}); err == nil {
			t.Fatal("UpdateNodeMenuState hook error succeeded")
		}
	})

	t.Run("hook invalid update", func(t *testing.T) {
		runtime := &configurableMenuRuntime{update: MenuStateUpdate{Version: -1}}
		w, node := workspaceWithMenuRuntime(t, runtime, nil)
		if err := w.SetNodeMenu(node, simpleEditableMenu()); err != nil {
			t.Fatal(err)
		}
		if _, err := w.UpdateNodeMenuState(node, MenuStateUpdate{Version: 1, Fields: []MenuFieldUpdate{{Block: "main", Field: "name", Value: "new"}}}); !errors.Is(err, ErrInvalidMenu) {
			t.Fatalf("UpdateNodeMenuState invalid hook update error = %v, want invalid menu", err)
		}
	})

	t.Run("hook fills version", func(t *testing.T) {
		runtime := &configurableMenuRuntime{update: MenuStateUpdate{Fields: []MenuFieldUpdate{{Block: "main", Field: "name", Value: "hook"}}}}
		w, node := workspaceWithMenuRuntime(t, runtime, nil)
		if err := w.SetNodeMenu(node, simpleEditableMenu()); err != nil {
			t.Fatal(err)
		}
		menu, err := w.UpdateNodeMenuState(node, MenuStateUpdate{Version: 1, Fields: []MenuFieldUpdate{{Block: "main", Field: "name", Value: "new"}}})
		if err != nil {
			t.Fatal(err)
		}
		if menu.Blocks[0].Fields[0].Value != "hook" {
			t.Fatalf("hook-normalized value = %#v, want hook", menu.Blocks[0].Fields[0].Value)
		}
	})

	t.Run("button hook error", func(t *testing.T) {
		runtime := &configurableMenuRuntime{buttonErr: errors.New("button failed")}
		w, node := workspaceWithMenuRuntime(t, runtime, nil)
		if err := w.SetNodeMenu(node, simpleEditableMenu()); err != nil {
			t.Fatal(err)
		}
		if err := w.TriggerNodeMenuButton(node, MenuButtonRef{Block: "main", Button: "run"}); err == nil {
			t.Fatal("TriggerNodeMenuButton hook error succeeded")
		}
		if runtime.buttonCalls != 1 {
			t.Fatalf("button hook calls = %d, want 1", runtime.buttonCalls)
		}
	})

	t.Run("closed before update commit", func(t *testing.T) {
		var w *Workspace
		var node NodeID
		runtime := &configurableMenuRuntime{onUpdate: func() { _ = w.Close() }}
		w, node = workspaceWithMenuRuntime(t, runtime, nil)
		if err := w.SetNodeMenu(node, simpleEditableMenu()); err != nil {
			t.Fatal(err)
		}
		if _, err := w.UpdateNodeMenuState(node, MenuStateUpdate{Version: 1, Fields: []MenuFieldUpdate{{Block: "main", Field: "name", Value: "new"}}}); !errors.Is(err, ErrClosed) {
			t.Fatalf("UpdateNodeMenuState closed-before-commit error = %v, want closed", err)
		}
	})

	t.Run("node deleted before update commit", func(t *testing.T) {
		var w *Workspace
		var node NodeID
		runtime := &configurableMenuRuntime{onUpdate: func() { _ = w.DeleteNode(node) }}
		w, node = workspaceWithMenuRuntime(t, runtime, nil)
		if err := w.SetNodeMenu(node, simpleEditableMenu()); err != nil {
			t.Fatal(err)
		}
		if _, err := w.UpdateNodeMenuState(node, MenuStateUpdate{Version: 1, Fields: []MenuFieldUpdate{{Block: "main", Field: "name", Value: "new"}}}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("UpdateNodeMenuState deleted-before-commit error = %v, want not found", err)
		}
	})

	t.Run("menu cleared before update commit", func(t *testing.T) {
		var w *Workspace
		var node NodeID
		runtime := &configurableMenuRuntime{onUpdate: func() { _ = w.ClearNodeMenu(node) }}
		w, node = workspaceWithMenuRuntime(t, runtime, nil)
		if err := w.SetNodeMenu(node, simpleEditableMenu()); err != nil {
			t.Fatal(err)
		}
		if _, err := w.UpdateNodeMenuState(node, MenuStateUpdate{Version: 1, Fields: []MenuFieldUpdate{{Block: "main", Field: "name", Value: "new"}}}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("UpdateNodeMenuState cleared-before-commit error = %v, want not found", err)
		}
	})

	t.Run("menu changed before update commit", func(t *testing.T) {
		var w *Workspace
		var node NodeID
		runtime := &configurableMenuRuntime{onUpdate: func() { _ = w.SetNodeMenu(node, NodeMenu{}) }}
		w, node = workspaceWithMenuRuntime(t, runtime, nil)
		if err := w.SetNodeMenu(node, simpleEditableMenu()); err != nil {
			t.Fatal(err)
		}
		if _, err := w.UpdateNodeMenuState(node, MenuStateUpdate{Version: 1, Fields: []MenuFieldUpdate{{Block: "main", Field: "name", Value: "new"}}}); !errors.Is(err, ErrStaleMenu) {
			t.Fatalf("UpdateNodeMenuState changed-before-commit error = %v, want stale menu", err)
		}
	})

	t.Run("closed before button commit", func(t *testing.T) {
		var w *Workspace
		var node NodeID
		runtime := &configurableMenuRuntime{onButton: func() { _ = w.Close() }}
		w, node = workspaceWithMenuRuntime(t, runtime, nil)
		if err := w.SetNodeMenu(node, simpleEditableMenu()); err != nil {
			t.Fatal(err)
		}
		if err := w.TriggerNodeMenuButton(node, MenuButtonRef{Block: "main", Button: "run"}); !errors.Is(err, ErrClosed) {
			t.Fatalf("TriggerNodeMenuButton closed-before-commit error = %v, want closed", err)
		}
	})

	t.Run("menu changed before button commit", func(t *testing.T) {
		var w *Workspace
		var node NodeID
		runtime := &configurableMenuRuntime{onButton: func() { _ = w.SetNodeMenu(node, NodeMenu{}) }}
		w, node = workspaceWithMenuRuntime(t, runtime, nil)
		if err := w.SetNodeMenu(node, simpleEditableMenu()); err != nil {
			t.Fatal(err)
		}
		if err := w.TriggerNodeMenuButton(node, MenuButtonRef{Block: "main", Button: "run"}); !errors.Is(err, ErrStaleMenu) {
			t.Fatalf("TriggerNodeMenuButton changed-before-commit error = %v, want stale menu", err)
		}
	})
}

func TestUpdateNodeMenuStateDirectBranches(t *testing.T) {
	w, _ := testWorkspace(t)
	node, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.updateNodeMenuStateDirect(node, MenuStateUpdate{Version: -1}); !errors.Is(err, ErrInvalidMenu) {
		t.Fatalf("direct invalid update error = %v, want invalid menu", err)
	}
	if _, err := w.updateNodeMenuStateDirect(999, MenuStateUpdate{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("direct missing node error = %v, want not found", err)
	}
	if _, err := w.updateNodeMenuStateDirect(node, MenuStateUpdate{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("direct missing menu error = %v, want not found", err)
	}
	if err := w.SetNodeMenu(node, simpleEditableMenu()); err != nil {
		t.Fatal(err)
	}
	if err := w.SetNodeMenu(node, simpleEditableMenu()); err != nil {
		t.Fatal(err)
	}
	if _, err := w.updateNodeMenuStateDirect(node, MenuStateUpdate{Version: 2, Fields: []MenuFieldUpdate{{Block: "main", Field: "missing", Value: "x"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("direct apply error = %v, want not found", err)
	}
	menu, err := w.updateNodeMenuStateDirect(node, MenuStateUpdate{Version: 2, Fields: []MenuFieldUpdate{{Block: "main", Field: "name", Value: "new"}}})
	if err != nil {
		t.Fatal(err)
	}
	if menu.Version != 3 || menu.Blocks[0].Fields[0].Value != "new" {
		t.Fatalf("direct updated menu = %#v", menu)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.updateNodeMenuStateDirect(node, MenuStateUpdate{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("direct closed error = %v, want closed", err)
	}
}

func TestNodeScopeMenuDuringInitializationBranches(t *testing.T) {
	w, node := workspaceWithMenuRuntime(t, &configurableMenuRuntime{}, func(ctx NodeContext) error {
		if err := ctx.Node.NotifyChanged(); !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("NotifyChanged during init error = %v, want not found", err)
		}
		if _, err := ctx.Node.AddMessage(MessageNote, "init"); !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("AddMessage during init error = %v, want not found", err)
		}
		if err := ctx.Node.RemoveMessage(1); !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("RemoveMessage during init error = %v, want not found", err)
		}
		if err := ctx.Node.SetMenu(simpleEditableMenu()); err != nil {
			return err
		}
		if err := ctx.Node.SetMenu(simpleEditableMenu()); err != nil {
			return err
		}
		if _, err := ctx.Node.UpdateMenuState(MenuStateUpdate{Version: 2, Fields: []MenuFieldUpdate{{Block: "main", Field: "name", Value: "init"}}}); err != nil {
			return err
		}
		if _, err := ctx.Node.UpdateMenuState(MenuStateUpdate{Version: 3, Fields: []MenuFieldUpdate{{Block: "main", Field: "missing", Value: "init"}}}); !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("UpdateMenuState invalid field error = %v, want not found", err)
		}
		if err := ctx.Node.ClearMenu(); err != nil {
			return err
		}
		if _, err := ctx.Node.UpdateMenuState(MenuStateUpdate{}); !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("UpdateMenuState after ClearMenu error = %v, want not found", err)
		}
		if err := ctx.Node.SetMenu(NodeMenu{Blocks: []MenuBlock{{ID: ""}}}); !errors.Is(err, ErrInvalidName) {
			return fmt.Errorf("SetMenu invalid error = %v, want invalid name", err)
		}
		return nil
	})
	if _, ok := w.NodeMenu(node); ok {
		t.Fatal("init ClearMenu should leave no menu")
	}
}

func TestNodeScopeMenuAfterInitializationBranches(t *testing.T) {
	scopes := map[NodeID]NodeScope{}
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{
		Name: "example.com/Menu",
		Runtime: menuRuntimeClass{
			scopes: scopes,
		},
	}}}); err != nil {
		t.Fatal(err)
	}
	node, err := w.CreateNode("example.com/Menu", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := scopes[node].ClearMenu(); err != nil {
		t.Fatal(err)
	}
}

type registeringMessageLibrary struct {
	errs []error
}

func (l *registeringMessageLibrary) Name() string { return "example.com" }

func (l *registeringMessageLibrary) DefineClasses(scope LibraryScope) error {
	_, addErr := scope.AddNodeMessage(1, MessageNote, "register")
	l.errs = append(l.errs, addErr)
	l.errs = append(l.errs, scope.RemoveNodeMessage(1, 1))
	return scope.DefineClass(ClassSpec{Name: "example.com/Node"})
}

func TestLibraryScopeMessageAndMenuBranches(t *testing.T) {
	lib := &registeringMessageLibrary{}
	w := NewWorkspace()
	if err := w.RegisterLibrary(lib); err != nil {
		t.Fatal(err)
	}
	for _, err := range lib.errs {
		if !errors.Is(err, ErrInactive) {
			t.Fatalf("registering message scope error = %v, want inactive", err)
		}
	}
	scope := &libraryScope{w: w, library: "example.com"}
	node, err := scope.CreateNode("example.com/Node", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	message, err := scope.AddNodeMessage(node, MessageWarn, "scoped")
	if err != nil {
		t.Fatal(err)
	}
	if err := scope.RemoveNodeMessage(node, message); err != nil {
		t.Fatal(err)
	}
	if err := scope.SetNodeMenu(node, simpleEditableMenu()); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.UpdateNodeMenuState(node, MenuStateUpdate{Version: 1, Fields: []MenuFieldUpdate{{Block: "main", Field: "name", Value: "scoped"}}}); err != nil {
		t.Fatal(err)
	}
	if err := scope.TriggerNodeMenuButton(node, MenuButtonRef{Block: "main", Button: "run"}); err != nil {
		t.Fatal(err)
	}
	if err := scope.ClearNodeMenu(node); err != nil {
		t.Fatal(err)
	}

	foreign := &libraryScope{w: w, library: "other.com"}
	if _, err := foreign.AddNodeMessage(node, MessageWarn, "foreign"); !errors.Is(err, ErrOwnership) {
		t.Fatalf("foreign AddNodeMessage error = %v, want ownership", err)
	}
	if err := foreign.RemoveNodeMessage(node, 1); !errors.Is(err, ErrOwnership) {
		t.Fatalf("foreign RemoveNodeMessage error = %v, want ownership", err)
	}
	if err := foreign.ClearNodeMenu(node); !errors.Is(err, ErrOwnership) {
		t.Fatalf("foreign ClearNodeMenu error = %v, want ownership", err)
	}
	if _, err := foreign.UpdateNodeMenuState(node, MenuStateUpdate{}); !errors.Is(err, ErrOwnership) {
		t.Fatalf("foreign UpdateNodeMenuState error = %v, want ownership", err)
	}
	if err := foreign.TriggerNodeMenuButton(node, MenuButtonRef{Block: "main", Button: "run"}); !errors.Is(err, ErrOwnership) {
		t.Fatalf("foreign TriggerNodeMenuButton error = %v, want ownership", err)
	}
}

func TestWorkspaceRemovalSortAndCloneBranches(t *testing.T) {
	w, _ := testWorkspace(t)
	first, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.AddNodeMessage(second, MessageNote, "second"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.AddNodeMessage(first, MessageNote, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.AddNodeMessage(first, MessageWarn, "first again"); err != nil {
		t.Fatal(err)
	}
	sub := w.WatchMessages(4)
	defer sub.Close()
	if err := w.DeleteNode(first); err != nil {
		t.Fatal(err)
	}
	firstEvent := <-sub.Events()
	secondEvent := <-sub.Events()
	if firstEvent.Message.Node != first || secondEvent.Message.Node != first || firstEvent.Message.ID > secondEvent.Message.ID {
		t.Fatalf("removed message events = %#v %#v, want sorted first-node events", firstEvent, secondEvent)
	}

	menuSub := w.WatchMenus(4)
	defer menuSub.Close()
	if err := w.SetNodeMenu(second, simpleEditableMenu()); err != nil {
		t.Fatal(err)
	}
	<-menuSub.Events()
	third, err := w.CreateNode("example.com/Source", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetNodeMenu(third, simpleEditableMenu()); err != nil {
		t.Fatal(err)
	}
	<-menuSub.Events()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	firstClear := <-menuSub.Events()
	secondClear := <-menuSub.Events()
	if firstClear.Kind != MenuCleared || secondClear.Kind != MenuCleared || firstClear.Node > secondClear.Node {
		t.Fatalf("close menu events = %#v %#v, want sorted clears", firstClear, secondClear)
	}

	records := cloneNodeRecords(map[NodeID]*nodeRecord{
		1: {
			id:      1,
			state:   StateActive,
			runtime: struct{}{},
			menu:    &NodeMenu{Version: 1},
		},
	})
	if records[1].runtime == nil || records[1].menu == nil {
		t.Fatalf("cloneNodeRecords lost runtime/menu: %#v", records[1])
	}
	messages := cloneMessageRecords(map[MessageID]*messageRecord{1: nil, 2: {id: 2, node: 1}})
	if messages[1] != nil || messages[2] == nil {
		t.Fatalf("cloneMessageRecords = %#v", messages)
	}
}
