# Concepts And Workspace Usage

This tutorial is for applications that want to use Pasta as a graph workspace and use existing node libraries.

The tested reference code for this tutorial is:

- `pasta/examples/calculator.go`
- `pasta/examples/usage_test.go`
- `demo/main.go`
- `pasta/workspace_test.go`

## 1. Know The Model

A Pasta `Workspace` owns the model:

- libraries and node classes;
- node instances with public state, private state, coordinates, metadata, ports, messages, and menus;
- directed links from output ports to input ports;
- ID generation for nodes, links, ports, messages, and persisted link names.

Applications own behavior. Pasta stores values such as private state and link objects, but it does not interpret calculator numbers, strings, streams, RPC handles, or editor coordinates.

The graph is a DAG. Link creation validates endpoint existence, port direction, type compatibility, input multiplicity, scoped ownership, and cycle safety before committing.

## 2. Register Existing Libraries

Create a workspace and register the libraries your application wants to expose. Registration asks each library to define its classes.

```go
w := pasta.NewWorkspace()
defer func() { _ = w.Close() }()

_ = w.RegisterLibrary(examples.CalculatorLibrary{})
```

The demo registers multiple libraries into one workspace:

```go
a.workspace = pasta.NewWorkspace(pasta.WithLogger((*demoLogger)(a)))
a.must(a.workspace.RegisterLibrary(examples.CalculatorLibrary{}), "register calculator library")
a.must(a.workspace.RegisterLibrary(StringLibrary{}), "register string library")
a.must(a.workspace.RegisterLibrary(StreamLibrary{}), "register stream library")
```

Use `Snapshot`, `Classes`, or `ClassesByLibrary` to populate a palette.

## 3. Create Nodes

Create nodes by class name. `NodeOptions{}` uses the class default state. Set `UseState` when the caller needs to seed public or private state.

```go
left, _ := w.CreateNode(examples.ConstantClass, pasta.NodeOptions{
	State: pasta.NodeState{
		DisplayName: "Example",
		PrimaryType: examples.NumberType,
		Private:     float64(2),
		Metadata:    map[string]string{"createdBy": "examples"},
	},
	UseState: true,
})
```

Coordinates are opaque strings owned by your application or UI. The demo stores them as a JSON array string:

```go
id, err := a.workspace.CreateNode(class, opts)
if err != nil {
	return 0, err
}
if err := a.workspace.SetNodeCoordinate(id, encodeCoord(x, y)); err != nil {
	return 0, err
}
```

## 4. Connect Nodes With Links

Links are directed from an output port to an input port. `CreateLink` takes the input endpoint first because the input runtime may provide the application-owned link object.

```go
_, _ = w.CreateLink(
    pasta.FullPortID{Node: sum, Port: examples.InputA},
    pasta.FullPortID{Node: left, Port: examples.Output},
	pasta.LinkOptions{Type: examples.NumberType},
)
```

Controllers can preflight common edits with `CanCreateNode`, `CanDeleteNode`, `CanCreateLink`, `CanSetNodePorts`, `CanSetLinkWaypoints`, and `CanDeleteLink`. These methods validate without mutating the workspace.

## 5. Query State

Use read-only snapshots for rendering and inspectors. Returned values are defensive copies.

```go
s := a.workspace.Snapshot()
for _, node := range s.Nodes {
	out.Nodes = append(out.Nodes, nodeDTO{
		ID:          node.ID.String(),
		Class:       node.Class,
		DisplayName: node.Dynamic.DisplayName,
		State:       string(node.State),
		Inputs:      portsDTO(node.Inputs),
		Outputs:     portsDTO(node.Outputs),
		Messages:    messagesDTO(node.Messages),
		Menu:        node.Menu,
	})
}
```

For one node, use `Node`:

```go
snap, ok := w.Node(node)
if !ok {
	return 0
}
return floatFromAny(snap.Dynamic.Private)
```

Always handle `ok == false`; a node or link can disappear between UI actions and backend calls.

## 6. Use Node Menus

Node menus are ephemeral JSON-serializable control documents. They are exposed in snapshots but are not saved, copied, pasted, or restored as model state.

Read the current menu, then submit a versioned update:

```go
menu, ok := w.NodeMenu(node)
if !ok {
	return
}
_, _ = w.UpdateNodeMenuState(node, pasta.MenuStateUpdate{
	Version: menu.Version,
	Fields: []pasta.MenuFieldUpdate{{
		Block: "main",
		Field: "value",
		Value: 7.5,
	}},
})
```

Buttons use a separate call:

```go
_ = w.TriggerNodeMenuButton(result, pasta.MenuButtonRef{
	Block:  "main",
	Button: "pull",
})
```

`ErrStaleMenu` means the UI submitted an older menu version. Refresh the menu and decide whether to preserve the user's in-progress input.

## 7. Save And Restore

Use `Save` when private state is already committed to `NodeState.Private`. Use `SaveWithRuntimeState` when active runtimes may hold newer volatile private state.

```go
saved, _ := original.SaveWithRuntimeState()

restored := pasta.NewWorkspace()
defer func() { _ = restored.Close() }()
_ = restored.RegisterLibrary(examples.CalculatorLibrary{})
_ = restored.Restore(saved)
```

`SaveData` stores model state, not Go link objects. If a library uses runtime-only link objects, recreate the links after restore to let lifecycle hooks rebuild those objects:

```go
for _, link := range w.Snapshot().Links {
	if err := w.DeleteLink(link.ID); err != nil {
		return err
	}
	if _, err := w.CreateLink(link.Input, link.Output, pasta.LinkOptions{
		Type:      link.Type,
		Waypoints: link.Waypoints,
	}); err != nil {
		return err
	}
}
```

For config-backed persistence, use the `configer.Config` helpers:

```go
cfg, err := w.SaveConfig()
if err != nil {
	return err
}
return restored.RestoreConfig(cfg)
```

Use `SaveConfigWithRuntimeState` for the config equivalent of `SaveWithRuntimeState`.

## 8. Copy And Paste

Copy serializes selected nodes and internal links whose endpoints are both
selected. Paste allocates fresh node and link IDs. If a copied selection
includes a single-node class that already exists, Pasta skips that duplicate and
still pastes the non-single-node nodes.

```go
clip, _ := w.Copy([]pasta.NodeID{left, right, sum})
pastedNodes, pastedLinks, _ := w.Paste(clip)
```

The demo stores the clipboard as JSON and offsets pasted coordinates after `Paste`:

```go
nodes, links, err := a.workspace.Paste(clip)
if err != nil {
	return err
}
for _, id := range nodes {
	snap, ok := a.workspace.Node(id)
	if !ok {
		continue
	}
	x, y := decodeCoord(snap.Dynamic.Coordinate)
	_ = a.workspace.SetNodeCoordinate(id, encodeCoord(x+in.DX, y+in.DY))
}
```

## 9. Handle Errors

Workspace errors wrap sentinel errors such as `ErrNotFound`, `ErrClosed`, `ErrInvalidName`, `ErrInvalidID`, `ErrInvalidPort`, `ErrTypeMismatch`, `ErrMultiplicity`, `ErrCycle`, `ErrInactive`, `ErrInvalidMenu`, and `ErrStaleMenu`.

Use `errors.Is` instead of string matching:

```go
if err := n.ctx.Node.RemoveMessage(id); err != nil && !errors.Is(err, pasta.ErrNotFound) {
	return err
}
```

Common handling:

- `ErrNotFound`: refresh the snapshot; the object may have been deleted.
- `ErrClosed`: stop mutating the workspace.
- `ErrInactive`: show the preserved inactive object but disable operational controls.
- `ErrStaleMenu`: refresh the menu version and preserve local user input where possible.
- `ErrTypeMismatch`, `ErrMultiplicity`, `ErrCycle`: report validation feedback near the attempted link or port edit.
