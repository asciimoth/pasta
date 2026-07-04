<p align="center">
  <img height="180" src="./pasta.png">
  <img height="180" src="./hello-pasta-demo.png">
</p>
<p align="center">
  <a href='https://coveralls.io/github/asciimoth/pasta?branch=master'><img src='https://coveralls.io/repos/github/asciimoth/pasta/badge.svg?branch=master' alt='CoverageStatus' /></a>
  <a href="https://pkg.go.dev/github.com/asciimoth/pasta/pasta"><img src="https://pkg.go.dev/badge/github.com/asciimoth/pasta/pasta.svg" alt="Go Reference"></a>
</p>


# pasta
Pasta is a headless Go framework for building node-based graph editors and
runtimes. It provides the core model and lifecycle machinery for systems similar
in shape to Unreal Engine Blueprints: flow-based programming languages,
sound-processing graphs, network-processing engines, visual scripting tools,
data pipelines, and other applications where users connect typed nodes with
directed links.

Pasta does not ship an out-of-the-box GUI and is not bound to a specific UI
library or application framework. Instead, it owns the graph data structures,
validation rules, state management, persistence, runtime lifecycle hooks,
notifications, resources, undo/redo, and test helpers that a GUI, API server,
runtime host, or other frontend can build on top of.

The core package, `pasta/`, contains the framework itself. The `pasta/std/`
package contains reusable node classes and type implementations.

## Features
- Typed node graph model with workspace-scoped node, port, and link IDs.
- Directed links between left and right ports, with type compatibility checks.
- Acyclic graph validation.
- Node classes and factories for live construction and restore.
- Placeholder nodes for unknown or unavailable classes, so saved topology can
  survive missing plugins or delayed class registration.
- Runtime lifecycle callbacks for initialization, readiness, root-path changes,
  port changes, link validation, link changes, events, inbox messages, Formular
  messages, and save hooks.
- JSON-serializable snapshots for frontends and observers.
- Name-based save configuration with stable link specs.
- Incremental workspace notifications.
- Optional interactive node menus backed by [formular](https://pkg.go.dev/github.com/asciimoth/formular).
- Workspace-owned closeable resources tied to nodes and links.
- Bounded best-effort undo/redo for topology changes.

## Graph model
A `Workspace` is the aggregate root. It owns node records, ports, links,
registered node classes, resources, notification subscriptions, node-menu
subscriptions, and undo/redo entries.

Nodes store workspace-owned metadata such as class, name, label, position,
primary type, popups, root state, derived root-path status, ordered left/right
ports, and optionally a live `Node` implementation. If the implementation is
missing, the node remains in the graph as a placeholder.

Ports belong to exactly one node. Each port has a side (`left` or `right`), a
name unique on that node side, a non-empty list of supported types, and attached
links. Links connect one left port to one right port on different nodes. Link
direction is determined by endpoint side, not by the order of arguments passed by
a caller.

## Validation and invariants
Pasta keeps the workspace internally consistent after topology mutations:

- node, port, and link IDs are positive and unique inside a workspace;
- node names are unique;
- port names are unique per node side;
- links cannot connect a node to itself;
- duplicate links between the same ports are rejected;
- linked ports must share a concrete type, unless one side supports `any/any`;
- link creation fails if it would introduce a cycle;
- removing a node removes its ports and links;
- removing a port removes its attached links;
- closed workspaces reject or ignore later mutations.

## Node classes and lifecycle
A `NodeClass` describes a node class for UI, construction, and restore purposes.
Classes that also implement `NodeClassFactory` can be used by `AddNodeByClass`
and restored as live implementations. `NodeClassParams` supplies default root
status, uniqueness, primary type, and initial ports.

Live nodes receive callbacks through the `Node` interface. Callback errors and
panics are contained by stopping or replacing the affected implementation with a
placeholder where possible, preserving graph structure instead of corrupting the
workspace.

## Workspace locking and callbacks
`Workspace` uses a regular non-recursive mutex. Public methods such as
`AddNode`, `Snapshot`, `SendEvent`, and `SetNodeLabel` acquire and release that
mutex themselves, so they are the right API for external callers such as UI
handlers, HTTP or RPC handlers, background goroutines, and tests.

Node callbacks run while the workspace mutex is already held. Code running from a
`Node` callback must use the matching `Locked` method, such as
`AddNodeLocked`, `SnapshotLocked`, `SendEventLocked`, or `SetNodeLabelLocked`.
This also applies to shared helper functions when they are called by a callback.

```go
// External code.
if err := workspace.SetNodeLabel(nodeID, "ready"); err != nil {
	return err
}

// Code already running under the workspace lock, such as a Node callback.
if err := workspace.SetNodeLabelLocked(nodeID, "ready"); err != nil {
	return err
}
```

The same rule applies after manually calling `workspace.Lock()`: use `Locked`
methods until the matching `workspace.Unlock()`. `Unlock` drains pending
operations and notifications after each outer call, so scheduled events,
inbox messages, and notification callbacks are still delivered after the locked
mutation completes.

### Migrating from recursive-lock versions
Older Pasta versions used a recursive workspace lock, so callback code could call
the normal public methods without blocking. New versions reject that pattern:
calling a public workspace method from a node callback attempts to lock the
already-held mutex and can deadlock.

When migrating existing node implementations:

- Keep public workspace methods in external code paths.
- Replace workspace calls inside `Node` callbacks with their `Locked`
  counterparts, for example `NextID` -> `NextIDLocked`, `Snapshot` ->
  `SnapshotLocked`, `NodeSnapshot` -> `NodeSnapshotLocked`, `AddPort` ->
  `AddPortLocked`, `RemovePort` -> `RemovePortLocked`, `AddLink` ->
  `AddLinkLocked`, `RemoveLink` -> `RemoveLinkLocked`, `SetPortTypes` ->
  `SetPortTypesLocked`, and `SetNodePortOrder` -> `SetNodePortOrderLocked`.
- Replace callback-side runtime messaging with locked methods:
  `SendEvent` -> `SendEventLocked`, `EmitEvent` -> `EmitEventLocked`,
  `SendInbox` -> `SendInboxLocked`, `SendNodeMenuMsg` ->
  `SendNodeMenuMsgLocked`, and `SendNodeFormularMsg` ->
  `SendNodeFormularMsgLocked`.
- Replace callback-side resource and subscription calls with locked methods:
  `AddNodeResource` -> `AddNodeResourceLocked`, `AddLinkResource` ->
  `AddLinkResourceLocked`, `SubscribeNotifications` ->
  `SubscribeNotificationsLocked`, and `SubscribeNodeMenu` ->
  `SubscribeNodeMenuLocked`.
- In `pasta/std`, use `std.RequestLocked` from callback code and keep
  `std.Request` for external code.
- Split helper functions that are used from both callback and external paths, or
  make the helper accept a function parameter so each caller can choose the
  correct public or locked workspace operation.

## Persistence and snapshots
`Snapshot()` returns JSON-serializable class, node, port, and link maps keyed by
IDs. It is intended for frontends, observers, and synchronization layers.

`SaveConfig()` persists by node name. Workspace-owned node keys are CamelCase:
`Class`, `Primary`, `Pos`, and `Links`. Node implementations save their own
lower-case keys through `OnSave`.

Links are saved as outgoing specs:

```text
output port name -> [Target Node] input port name
```

`WorkspaceFromConfig()` restores the workspace.

## Notifications and node menus
Notification subscribers receive a full snapshot on subscription, then
incremental class, node, port, link, workspace-stop, and node-menu
notifications.

Nodes may also expose interactive per-node menus using Formular JSON protocol
types. In this model the node is the Formular backend, the workspace is a cache
plus demultiplexer, and external GUI, TUI, web, or RPC code is the frontend.

Menus are not part of workspace snapshots, are not supplied by node-add APIs,
and are cleared when a node implementation is replaced. A node can build or
update its menu from `OnInit` or later by calling
`Workspace.SendNodeMenuMsg(nodeID, formularMessage)`. The workspace applies
cacheable messages to that node's `formular.MenuSnapshotState` and forwards
copies only to subscribers of that node menu.

Workspace notification subscribers do not receive node menu traffic by default.
A frontend that wants to show a clicked node menu should call
`SubscribeNotifications`, then `SubscribeNodeMenu(nodeID, subscriptionID)`.
If cached menu state exists, the subscriber immediately receives a
`NotificationNodeMenu` carrying a forced Formular `menu.snapshot`.

Frontend-to-backend Formular messages should be sent with
`Workspace.SendNodeFormularMsg(nodeID, message)`. Live nodes receive the message
through `Node.OnFormularMsg`; missing nodes, placeholders, closed workspaces, and
nil messages are dropped silently.

## Resources and undo/redo
Resources registered with nodes or links are `io.Closer` values. They are closed
when the owner is removed, replaced, failed into a placeholder, or when the
workspace closes. The same resource bound to multiple owners is deduplicated and
closed once.

Undo/redo is best-effort and bounded to 64 entries. It covers topology-focused
node and link add/remove operations and groups where appropriate. Failed rollback
entries are dropped, so undo should be treated as a user convenience rather than
durable history.

## License
Files in this repository are distributed under the CC0 license.  

<p xmlns:dct="http://purl.org/dc/terms/">
  <a rel="license"
     href="http://creativecommons.org/publicdomain/zero/1.0/">
    <img src="http://i.creativecommons.org/p/zero/1.0/88x31.png" style="border-style: none;" alt="CC0" />
  </a>
  <br />
  To the extent possible under law,
  <a rel="dct:publisher"
     href="https://github.com/asciimoth">
    <span property="dct:title">ASCIIMoth</span></a>
  has waived all copyright and related or neighboring rights to
  <span property="dct:title">pasta</span>.
</p>
