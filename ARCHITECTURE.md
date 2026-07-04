# Architecture
Pasta is a headless Go framework for node-based graph editors and runtimes.
The core package, `pasta/`, owns the in-memory graph model, validation rules, 
lifecycle callbacks, persistence, notifications, resources, and bounded undo/redo.
The `pasta/std/` package provides reusable node classes and type implementations.

## High-Level Model
`Workspace` is the aggregate root. It owns ordered maps of:

- node records, keyed by workspace-scoped `uint64` IDs
- ports, keyed by ID
- links, keyed by ID
- registered `NodeClass` definitions, keyed by class name
- resources, notification subscriptions, node-menu subscriptions, and undo/redo entries

IDs start at `1`, are never `0`, and are unique across nodes, ports, and links 
in a workspace.

A node record stores workspace-owned metadata: class, name, label, position,
primary type, popups, explicit root flag, derived root-path status, 
ordered left/right port IDs, and an optional live `Node` implementation.
If the implementation is missing, the record is a placeholder.

Ports belong to exactly one node. A port has a direction (`left` or `right`),
a name unique on that node side, a non-empty list of supported types,
and attached link IDs.

Links connect one left port to one right port on different nodes.
Link direction is modeled by endpoint side, not by caller argument order.

## Node Classes and Construction
`NodeClass` describes a class for UI and restore purposes.
Classes may also implement `NodeClassFactory`; only factory classes can be used
with `AddNodeByClass` or restored as live implementations.
`NodeClassParams` supplies default root status, uniqueness, primary type, and
initial ports.

Unknown or unavailable classes are restored as placeholders so saved graph
topology can survive until a matching class is registered.

## Lifecycle
Nodes receive callbacks through the `Node` interface: initialization, readiness,
root-path status, port changes, link validation, link changes, events,
inbox messages, Formular messages, and save.
Callback errors and panics are contained by stopping or replacing the affected
implementation with a placeholder where needed, preserving graph structure when
possible.
Node implementations can also spawn node-attached background workers through
the workspace. Worker panics are contained like callback panics and replace the
attached node with a placeholder; workspace close waits for tracked workers to
stop after node stop callbacks run.

`Workspace` uses a regular non-recursive mutex plus a pending-operation queue.
Public workspace methods acquire the mutex themselves and are intended for
external callers. Node callbacks run while the workspace mutex is already held,
so callback code and helper functions called from callbacks must use the
matching exported `Locked` methods. Manual callers that invoke `Workspace.Lock`
follow the same rule until they invoke `Workspace.Unlock`.

`Unlock` drains pending operations after every outer unlock. Delivery callbacks
and notification callbacks therefore run after the mutation that scheduled them,
while a guard prevents nested post-unlock processing from reordering queued
work. Event and inbox delivery are scheduled and revalidated immediately before
delivery, so stale graph references are dropped instead of delivered.

## Core Invariants
- Workspace IDs are positive and unique across nodes, ports, and links.
- Node names are unique in a workspace and cannot contain `[` or `]`.
- Class names use `example.com/ClassName`; type names use `example.com/typeName`; `any/any` is the wildcard type.
- Port directions are only `left` or `right`.
- Ports have at least one type and valid names.
- Port names are unique among ports on the same node side.
- A link connects exactly one left port and one right port.
- A link cannot connect a node to itself or a port to itself.
- Duplicate links between the same two ports are rejected.
- Linked ports must share a concrete type, unless one side supports `any/any`.
- The node graph is kept acyclic; link creation that introduces a cycle fails.
- Removing a node removes its ports and links; removing a port removes attached links.
- Link and port snapshots are kept consistent with each other after every topology mutation.
- `Root` is explicit node state; `HasRootPath` is derived from roots and the acyclic link graph.
- Closed workspaces reject or ignore future mutations.

## Persistence and Snapshots
`Snapshot()` returns JSON-serializable class, node, port, and link maps keyed by IDs.
It is intended for frontends and observers.

`SaveConfig()` persists by node name.
Workspace-owned node keys are CamelCase: `Class`, `Primary`, `Pos`, and `Links`.
Node implementations save their own lower-case keys through `OnSave`.

Links are saved as outgoing specs such as:
```text
output port name -> [Target Node] input port name
```

`WorkspaceFromConfig()` parses nodes first, restores or creates placeholders,
then restores links and marks the workspace ready.

## Notifications, Menus, and Resources
Notification subscribers receive a full snapshot on subscription, then incremental 
class, node, port, link, workspace-stop, and node-menu notifications.
Formular node-menu messages are delivered only to subscribers that explicitly
subscribe to that node menu.

Resources registered with nodes or links are `io.Closer` values.
They are closed when the owner is removed, replaced, failed into a placeholder,
or when the workspace closes.
The same resource bound to multiple owners is deduplicated and closed once.

## Undo and Redo
Undo/redo is best-effort and bounded to 64 entries.
It covers topology-focused node and link add/remove operations and groups where appropriate.
Failed rollback entries are dropped silently, so undo should be treated as user 
convenience rather than durable history.
