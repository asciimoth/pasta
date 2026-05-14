# UI Tutorial

This tutorial is for building a frontend around a Pasta workspace. The frontend can be a browser GUI, native GUI, HTTP API, CLI, MCP server for an LLM, or another controller.

The tested reference code for this tutorial is:

- `demo/main.go`
- `demo/app.js`
- `demo/README.md`
- `pasta/examples/usage_test.go`
- `pasta/workspace_menu_internal_test.go`

## 1. Use Pasta As The Source Of Truth

The demo rule is:

```text
Pasta owns workspace state. The UI is the editable view.
```

All model changes should go through workspace methods. After a successful write, redraw or patch UI state from a fresh snapshot.

The demo exposes a small request/response API:

```go
func (a *appState) call(method, raw string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var data any
	var err error
	switch method {
	case "snapshot":
		data = a.snapshot()
	case "createNode":
		data, err = a.createNode(raw)
	case "createLink":
		data, err = a.createLink(raw)
	case "updateMenuField":
		err = a.updateMenuField(raw)
		data = a.snapshot()
	}
	if err != nil {
		return a.encode(response{OK: false, Error: err.Error(), Logs: a.logs})
	}
	return a.encode(response{OK: true, Data: data, Logs: a.logs})
}
```

For an HTTP or MCP frontend, keep the same shape: parse external IDs, call workspace methods, return either data from `Snapshot` or a structured error.

## 2. Build UI State From Snapshots

Convert `Snapshot` into a frontend DTO. Do not let the frontend invent Pasta IDs.

```go
s := a.workspace.Snapshot()
for _, link := range s.Links {
	out.Links = append(out.Links, linkDTO{
		ID:     link.ID.String(),
		Input:  endpointDTO{Node: link.Input.Node.String(), Port: link.Input.Port.String()},
		Output: endpointDTO{Node: link.Output.Node.String(), Port: link.Output.Port.String()},
		Type:   link.Type,
		State:  string(link.State),
	})
}
```

Snapshots include inactive nodes and inactive links when their endpoints still exist. Render them as recoverable but disabled. Broken links are removed by the workspace and should disappear from the next snapshot.

Node snapshots also include `HasKeyNodeAccess`.
A node without key-node access is still an editable graph object, but its runtime may be idle because it is not connected to any active key node.
UI controls can show that state as "not currently used" without deleting or disabling the node.

## 3. Parse External IDs At The Boundary

When a UI sends IDs back, parse the canonical strings with the helpers in `ids.go`.

```go
func fullPort(nodeText, portText string) (pasta.FullPortID, error) {
	node, err := pasta.ParseNodeID(nodeText)
	if err != nil {
		return pasta.FullPortID{}, err
	}
	port, err := pasta.ParsePortID(portText)
	if err != nil {
		return pasta.FullPortID{}, err
	}
	return full(node, port), nil
}
```

Return validation errors to the UI instead of trusting stale client-side state.

## 4. Translate UI Edits To Workspace Mutations

Create nodes:

```go
id, err := a.workspace.CreateNode(class, opts)
if err != nil {
	return 0, err
}
if err := a.workspace.SetNodeCoordinate(id, encodeCoord(x, y)); err != nil {
	return 0, err
}
```

Move nodes:

```go
id, err := pasta.ParseNodeID(in.ID)
if err != nil {
	return err
}
return a.workspace.SetNodeCoordinate(id, encodeCoord(in.X, in.Y))
```

Rename nodes by reading the current state, changing the field, then setting the full state:

```go
snap, ok := a.workspace.Node(id)
if !ok {
	return fmt.Errorf("node %s not found", id)
}
state := snap.Dynamic
state.DisplayName = name
return a.workspace.SetNodeState(id, state)
```

Create links by resolving endpoints and choosing a type accepted by the ports:

```go
input, err := fullPort(in.InputNode, in.InputPort)
if err != nil {
	return snapshotDTO{}, err
}
output, err := fullPort(in.OutputNode, in.OutputPort)
if err != nil {
	return snapshotDTO{}, err
}
linkType, err := a.linkType(input, output)
if err != nil {
	return snapshotDTO{}, err
}
_, err = a.workspace.CreateLink(input, output, pasta.LinkOptions{Type: linkType})
```

## 5. Handle Gone Objects

UI code must assume any object can be gone or inactive by the time a set/get operation runs. Causes include user actions in another pane, runtime panics recovered by Pasta, lifecycle cleanup, restore, class recall, or workspace close.

Practical rules:

- If `Node`, `Link`, or `NodeMenu` returns `ok == false`, refresh from `Snapshot`.
- If a mutation returns `ErrNotFound`, refresh and remove or disable the stale UI element.
- If it returns `ErrInactive`, keep showing the object but disable operational edits.
- If it returns `ErrClosed`, stop accepting workspace edits.
- If it returns validation errors such as `ErrTypeMismatch`, `ErrMultiplicity`, or `ErrCycle`, keep the UI state and show the failed edit near the attempted action.

Use `errors.Is` for classification.

## 6. Render Node Menus Generically

A menu is a JSON-serializable schema plus state. Render blocks, scalar fields, options, checkboxes, read-only fields, repeat groups, and buttons from `NodeMenu`.

The renderer should understand this schema:

- `NodeMenu.Version`: send it back with updates. A stale version returns `ErrStaleMenu`.
- `NodeMenu.Blocks`: render each `MenuBlock` as a section or inspector group.
- `MenuBlock.Fields`: scalar controls.
- `MenuBlock.Buttons`: actions that call `TriggerNodeMenuButton`.
- `MenuBlock.Repeats`: lists/arrays of structured items.
- `Metadata`: opaque hints for your frontend. Pasta stores and copies it but does not interpret it.

Field kinds map directly to UI controls:

- `read-only`: display JSON-compatible data, never submit edits for it.
- `string`: text input, textarea, or select when `Options` is present.
- `int64`: integer input or select.
- `float64`: number input, slider, or select.
- `bool`: checkbox or toggle; if `Render` is `checkbox`, prefer a checkbox.

`Options` means the value must be one of the listed option values. Render enabled options as choices and disabled options as visible but unavailable choices. Preserve the raw JSON value when sending updates; do not send the label.

```go
pasta.MenuField{
	ID:      "choice",
	Kind:    pasta.MenuFieldString,
	Value:   "a",
	Options: []pasta.MenuOption{{Value: "a"}, {Value: "b", Disabled: true}},
}
```

Repeat groups are the menu shape for field lists and arrays. Render `MenuRepeat.Template` as the column/control schema, then render each `MenuRepeatItem` as one row, card, or nested form item. The item `Fields` slice is already normalized by Pasta, so each item field has its effective kind, label, options, render hint, read-only flag, and value.

```go
pasta.MenuRepeat{
	ID: "rows",
	Template: []pasta.MenuField{
		{ID: "name", Kind: pasta.MenuFieldString},
		{ID: "count", Kind: pasta.MenuFieldInt64},
	},
	Items: []pasta.MenuRepeatItem{{
		ID:     "one",
		Fields: []pasta.MenuField{{ID: "name", Value: "alpha"}, {ID: "count", Value: int64(1)}},
	}},
}
```

When the user edits a repeat list, send the whole replacement item state for that repeat. Keep item IDs stable across reorder/edit operations:

```go
_, err = a.workspace.UpdateNodeMenuState(id, pasta.MenuStateUpdate{
	Version: in.Version,
	Repeats: []pasta.MenuRepeatUpdate{{
		Block:  "main",
		Repeat: "rows",
		Items: []pasta.MenuRepeatItemState{{
			ID:     "one",
			Fields: map[string]any{"name": "alpha", "count": int64(1)},
		}},
	}},
})
```

For a GUI, track repeat item drafts by `(nodeID, menuVersion or local menu instance, blockID, repeatID, itemID, fieldID)`. That lets a periodic refresh update other rows without resetting the field the user is editing.

Send field edits through `UpdateNodeMenuState`:

```go
_, err = a.workspace.UpdateNodeMenuState(id, pasta.MenuStateUpdate{
	Version: in.Version,
	Fields:  []pasta.MenuFieldUpdate{{Block: in.Block, Field: in.Field, Value: in.Value}},
})
```

Send buttons through `TriggerNodeMenuButton`:

```go
err = a.workspace.TriggerNodeMenuButton(id, pasta.MenuButtonRef{
	Block:  in.Block,
	Button: in.Button,
})
```

Use the menu `Version`. If the workspace returns `ErrStaleMenu`, refresh the menu but do not blindly reset a text box while the user is typing. Keep local draft input until the user commits or the field truly disappears.

## 7. Show Messages

Messages are transient node notifications. They appear in snapshots:

```go
out.Nodes = append(out.Nodes, nodeDTO{
	ID:       node.ID.String(),
	Messages: messagesDTO(node.Messages),
	Menu:     node.Menu,
})
```

They can also be watched with `WatchMessages`. Watchers are useful for popups or toast notifications; snapshots remain the recovery path if events are dropped or a view reconnects.

Messages are not persisted. Do not include them in undo state or saved files unless your application intentionally stores its own separate notification log.

## 8. Poll Or Subscribe Carefully

Periodic UI updates may be needed because nodes can initiate state changes from internal workers. Demo stream nodes update menus and private state while worker goroutines run, and they only run those workers while connected to active key nodes.

Polling guidance:

- Poll only while a workspace view / app window is visible or active.
- Use a moderate interval; avoid high-frequency polling for inactive tabs.
- Prefer event subscriptions for menus/messages when they fit your transport.
- Reconcile from `Snapshot` periodically even when using events.
- Never overwrite a focused input with refreshed menu state unless the committed field changed under the user in a way you must surface.

For GUI frameworks, keep an explicit "editing" or "dirty local value" state for menu fields.

## 9. Save, Restore, Copy, Paste

For JSON save/restore, the demo uses `SaveWithRuntimeState` and `Restore`.

```go
data, err := a.workspace.SaveWithRuntimeState()
if err != nil {
	return "", err
}
out, err := json.MarshalIndent(data, "", "  ")
```

Restore into a fresh workspace, register libraries, restore data, and rehydrate runtime-only link objects if your libraries need them:

```go
a.workspace = pasta.NewWorkspace(pasta.WithLogger((*demoLogger)(a)))
if err := a.workspace.RegisterLibrary(examples.CalculatorLibrary{}); err != nil {
	return err
}
if err := a.workspace.Restore(data); err != nil {
	return err
}
return a.rehydrateLinks()
```

Copy/paste can be exposed as a JSON clipboard:

```go
clip, err := a.workspace.Copy(ids)
if err != nil {
	return "", err
}
data, err := json.MarshalIndent(clip, "", "  ")
```

If the clipboard contains nodes for single-node classes that already exist in the
workspace, `Paste` skips those duplicates and still returns the non-single-node
nodes it created. UI code should use the returned IDs instead of assuming every
clipboard node was pasted.

After paste, the demo offsets coordinates and tolerates a pasted node disappearing before the coordinate update:

```go
for _, id := range nodes {
	snap, ok := a.workspace.Node(id)
	if !ok {
		continue
	}
	x, y := decodeCoord(snap.Dynamic.Coordinate)
	_ = a.workspace.SetNodeCoordinate(id, encodeCoord(x+in.DX, y+in.DY))
}
```

## 10. Implement Undo/Redo In The UI Layer

Pasta deliberately does not implement undo/redo. If your product needs it, wrap workspace mutations in UI commands.

A practical command should store:

- the user intent and affected IDs;
- enough previous model state to reverse the operation, usually from `Save`, `Copy`, or targeted snapshots;
- the fresh IDs returned by `Paste` or `CreateNode`;
- any UI-only state such as selection, viewport, focused menu field, and local drafts.

Do not bypass workspace methods to implement undo. Replay or reverse through public mutations so validation, locking, lifecycle hooks, and broken-link cleanup still run.

## 11. API And MCP Notes

For non-GUI frontends, expose operations that match workspace concepts:

- `snapshot`
- `classes`
- `createNode`
- `deleteNode`
- `setNodeState` or narrower operations such as `renameNode`
- `createLink`
- `deleteLink`
- `updateNodeMenuState`
- `triggerNodeMenuButton`
- `copy`
- `paste`
- `save`
- `restore`

Return canonical IDs and structured errors. For LLM/MCP tools, prefer small operations with fresh snapshots before and after mutations, because stale IDs and stale menu versions are common in conversational workflows.
