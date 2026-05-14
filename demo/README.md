# Pasta WASM LiteGraph Demo

This demo is a static web app that uses Pasta from Go compiled to WebAssembly as
the graph backend and `github.com/jagenjo/litegraph.js` as the browser graph
editor. It exposes the calculator library from `pasta/examples` and a
demo-local string processing library.

The important rule is:

```text
Pasta owns workspace state. LiteGraph is the editable view.
```

All model changes go through the Go backend, then the frontend redraws from a
fresh backend snapshot. Background node changes reach the browser through
workspace notifications, so the UI does not poll. This keeps validation,
locking, menus, copy/paste, and save/restore on the same API paths a real app
would use.

## Run

```sh
just demo-serve
```

Open:

```text
http://127.0.0.1:8000/
```

Build without serving:

```sh
just demo-build
```

Clean generated browser assets:

```sh
just demo-clean
```

`app.wasm` and `wasm_exec.js` are generated build outputs and are ignored by
Git.

## Files

- `main.go`: WASM backend. It owns a `pasta.Workspace`, registers the demo
  libraries, exposes `window.pastaDemoCall(method, json)` and
  `window.pastaDemoSubscribe(callback)`, and returns JSON responses.
- `string_nodes.go`: demo-local string node library using push-style data flow.
- `main_stub.go`: host build stub so `go test ./...` works outside `GOOS=js
  GOARCH=wasm`.
- `app.js`: browser controller. It creates the LiteGraph canvas, translates UI
  edits to backend calls, and redraws from backend snapshots.
- `index.html`: static page shell and CDN imports for LiteGraph.
- `style.css`: demo layout and panel styling.
- root `Justfile`: demo build, serve, and clean commands.

## Backend API

The frontend calls:

```js
window.pastaDemoCall(method, JSON.stringify(payload))
```

It also subscribes once:

```js
window.pastaDemoSubscribe(() => refreshFromSnapshot())
```

The callback is a broad invalidation signal from `WatchWorkspace`; the browser
fetches a fresh `snapshot` instead of treating events as a complete mutation
log.

The backend returns:

```json
{
  "ok": true,
  "data": {},
  "logs": []
}
```

or:

```json
{
  "ok": false,
  "error": "message",
  "logs": []
}
```

Current methods:

- `snapshot`: return classes, nodes, links, current menus, values, and
  coordinates.
- `seed`: reset to a sample `10 + 6 -> result` graph.
- `clear`: reset to an empty workspace.
- `createNode`: create a node at `{class, x, y, value}`.
- `deleteNode`: delete `{id}`.
- `moveNode`: store `{id, x, y}` in `NodeState.Coordinate`.
- `renameNode`: store `{id, name}` in `NodeState.DisplayName`.
- `createLink`: create a Pasta link from `{outputNode, outputPort}` to
  `{inputNode, inputPort}`.
- `deleteLink`: delete `{id}`.
- `updateMenuField`: apply a Pasta node menu state update.
- `triggerMenuButton`: trigger a Pasta node menu button.
- `copy`: copy selected node IDs and return a clipboard JSON dump.
- `paste`: paste a clipboard JSON dump with `{dx, dy}` offset.
- `save`: return a workspace `SaveData` JSON dump.
- `restore`: restore from a workspace `SaveData` JSON dump.

## Snapshot Shape

`snapshot` returns a frontend-oriented DTO:

- `classes`: active calculator classes with class names and port specs.
- `nodes`: node ID, class, display name, value, coordinate, ports, state, menu.
- `links`: link ID, endpoints, type, and state.

IDs stay in Pasta canonical text form, for example `1N`, `1i`, `1o`, and `1L`.
The frontend does not invent backend IDs.

## Coordinate Sync

Coordinates live in `NodeState.Coordinate` as a small JSON array string:

```json
[80, 170]
```

The frontend flushes LiteGraph positions to Go:

- when the user releases the pointer after dragging;
- before backend operations that would redraw from a snapshot.

This avoids losing unsaved positions when a menu edit, link edit, save, or copy
causes a redraw.

## Menus

Pasta node menus are exposed in each node snapshot. The frontend renders:

- the demo-level editable node name field, stored in `NodeState.DisplayName`;
- Pasta menu fields from `NodeMenu.Blocks`;
- Pasta menu buttons, such as the calculator `Result` node's `Pull` button.

Calculator constants use the example runtime menu field named `main/value`.
Changing it calls `Workspace.UpdateNodeMenuState`, which lets the runtime update
private state and push values through connected calculator wires.

String `Text` nodes also use `main/value`. Editing the text pushes the value
through string wires to nodes such as `Split`, `Trim`, `Uppercase`, `Lowercase`,
`Replace`, and `String Result`. `Split` exposes `main/separator` and routes the
first, second, and remaining text segments through separate output ports.
`Replace` exposes `main/find` and `main/replacement` fields and recomputes from
its latest pushed input.

String node menus also include demo buttons that attach note, warning, and error
popup messages to the selected node. These messages are ephemeral Pasta node
messages: they show in snapshots and on the graph, but are not saved or restored.

## Save / Restore

`save` calls `Workspace.SaveWithRuntimeState()` so runtime-owned calculator and
string values are included in the text dump.

`restore` rebuilds a fresh workspace, registers the demo libraries, restores the
`SaveData`, then rehydrates runtime link objects by deleting and recreating each
restored link. This mirrors the pattern in `pasta/examples/usage_test.go`. The
persisted `SaveData` stores graph state, not Go link objects.

## Copy / Paste

`copy` calls `Workspace.Copy(selectedIDs)` and writes the resulting Pasta
clipboard JSON to the dump textarea.

`paste` calls `Workspace.Paste(clipboard)` and offsets pasted node coordinates.
Only links whose endpoints are both inside the selected copied set are included,
which is Pasta's clipboard contract.
When the clipboard contains a node from a single-node class that is already
present, Pasta skips that duplicate and still pastes the other nodes; links
touching the skipped node are omitted.

## LiteGraph Integration Notes

LiteGraph node classes are registered dynamically from backend class snapshots.
Each LiteGraph node stores:

- `backendId`: Pasta node ID, such as `3N`;
- `pastaPort` on each LiteGraph input/output slot, such as `1i` or `1o`.

Connections are translated into Pasta `CreateLink` calls from output endpoint to
input endpoint. Disconnect handling only deletes from the input-side callback,
because LiteGraph can notify both link endpoints for one user disconnect.

## Adding Node Types Later

For new real node types, add the library/runtime code on the Go side first:

1. Define new Pasta classes in a library.
2. Register that library in `appState.reset` and `restoreLocked`.
3. Include the classes in `snapshot()` if they should appear in the palette.
4. Teach the frontend any custom rendering or editor affordances that are not
   expressible through Pasta ports and menus.

Prefer using Pasta menus for node-specific controls. That keeps validation and
state updates in Go and lets the frontend remain a generic menu renderer.

## Development Checks

From the repository root:

```sh
go test ./pasta/... -race
golangci-lint run ./pasta/...
```

From `demo/`:

```sh
go test ./...
GOOS=js GOARCH=wasm go build -o /tmp/pasta-demo-test.wasm .
```

From the repository root:

```sh
just demo-build
```
