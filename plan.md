# Pasta Implementation Plan

## Goal

Build a Go framework for node-based editors and runtimes. Target use cases include
FBP-style dataflow graphs, sound editors, visual scripting, and network/process
managers.

The `./pasta` package should provide the graph model, validation, lifecycle management,
serialization hooks, and controlled access boundaries. Applications and libraries
provide the actual node behavior and define type-specific contracts.

## Core Domain

### Graph Model

- A `Workspace` owns registered libraries, node classes, nodes, links, and ID
  generation.
- A `Node` is an instance of one node class.
- A node has input and output `Port`s.
- A `Link` connects one output port to one input port on different nodes.
- Links are directed from output port to input port.
- The workspace graph must always be a DAG.
- Some ports accept at most one link. Other ports may allow multiple links.
- Nodes and ports may change dynamically, but every change must preserve link
  validity and DAG validity.

Terminology for a link:

- The **output node** owns the output port.
- The **input node** owns the input port.
- UIs usually render output nodes to the left of input nodes, input ports on the
  left side of a node, and output ports on the right side. This is only a UI
  convention; the model should not depend on coordinates.

### Types

- Each port has either:
  - one fixed type, or
  - a set of accepted types.
- A link always has one fixed type.
- At least one endpoint involved in creating a link must provide a fixed type.
- The link type is the fixed type chosen at creation time, normally from the
  port where the user started the connection in the GUI.
- A link may attach only to a port with the same fixed type or to a port whose
  accepted-type set contains the link type.
- An unlinked port may freely change its fixed type or accepted-type set.
- A linked port may change only in ways that keep all attached links valid.

Type compatibility should be implemented as a pure validation path so it can be
used by both mutation methods and "can I do this?" UI queries.

### Dynamic Node State

Nodes may change the following public state at runtime:

- ports and port metadata
- primary type hint
- display name
- description
- opaque editor coordinates
- other public metadata needed by editors

Nodes may also have ephemeral text messages attached for UI/runtime feedback.
Messages have one of three severities, `note`, `warn`, or `err`, and are
intended for transient popups, diagnostics, or acknowledgable warnings. They can
be added through the full workspace API, by the owning library scope, or by the
node itself through its node-scoped API, and they can be removed later.
External watchers must be able to subscribe to message add/remove events.

Each node also has optional private state owned by the node implementation. The
workspace stores and restores it, but does not interpret it. Private state is a
`configer.Config` subtree.

Node coordinates are stored as an arbitrary string. Different applications may
use different coordinate systems, layouts, or serialized geometry formats, so
the core framework should only provide get/set storage and persistence for this
field.

Ephemeral node messages are not persisted, copied, pasted, restored, or included
in save data. They may appear in current snapshots for renderers, but they must
not become durable graph state.

Nodes must be able to update their private state at any time, including from
background goroutines owned by the node. These updates still need to go through
a workspace-provided or node-scoped API so they are synchronized, panic-safe,
and visible to save/copy operations.

When a node is created normally, it receives the class default state. When a
workspace is restored, the node is initialized again with the saved
public and private state.

### Link Editor State

Links may store optional path waypoint coordinates. This lets editors route
links through complex graphs in a more readable way.

Waypoint coordinates are stored as an array of arbitrary strings for the same
reason node coordinates are strings: each application may have its own
coordinate system and path format. The core framework should provide get/set
storage, validation that the field is well-formed as an array, and persistence.
It should not parse or interpret waypoint values.

### Node Classes and Libraries

Each node has exactly one node class.

A node class provides:

- static metadata that cannot change for existing class identity, including the
  class name
- default dynamic state used when creating nodes
- lifecycle callbacks used by the workspace
- validation or construction behavior needed for links and private state

Each node class is defined by one `Library`.

Most applications will use one library for the whole app. More complex
applications may use multiple libraries, for example one built-in library and
one user-defined library, or one library per plugin.

Libraries may be registered and unregistered at runtime.

- On registration, the workspace asks the library to define all currently known
  classes.
- A registered library may define additional classes later.
- A registered library may recall classes later.
- Nodes whose class is recalled become inactive.
- When a library is unregistered, all nodes whose classes came from that library
  become inactive.
- Links between inactive nodes are inactive.
- Links that become invalid because an endpoint was deleted or no longer exists
  are broken and must be removed immediately.
- Links that still have valid endpoints but cannot currently operate because a
  node, class, or library is inactive are preserved as inactive links.

Library access must be scoped. A library should be able to:

- define and recall its own classes
- create, delete, and modify nodes of its own classes
- create, delete, and modify links only when both endpoint nodes are owned by
  that same library

A library must not be able to mutate unrelated classes, nodes, or links owned by
other libraries.

## Naming and IDs

### Qualified Names

Node class names and type names are URL-like names containing only:

- ASCII letters and digits
- dots
- dashes
- slashes

They must start with a letter.

Class names start with an uppercase letter after the library prefix:

- `example.com/NodeSum`

Type names start with a lowercase letter after the library prefix:

- `example.com/int`
- `example.com/float`
- `e-x-a-m-p-l-e.com/bool`

Library names are domain-like names containing only ASCII letters, digits, dots,
and dashes:

- `example.com`

A library may define only node classes under its own prefix:

- library `example.com` may define `example.com/Sub`
- library `example.com` may not define `other.example/Sub`

Types are also namespaced by library, but any library may use any valid type
name. Type ownership is only namespacing, not an access-control rule.

All name validation must be centralized and covered by table tests.

### Object IDs

The workspace generates numeric IDs. IDs must be unique within their object
kind and stable across save/restore.

- Node ID: numeric ID plus `N` suffix, for example `1234N`.
- Port ID: numeric ID plus `i` or `o` suffix, for example `5678i` or `5678o`.
  Port IDs are unique within one node.
- Full port ID: `{node ID}{port ID}`, for example `1234N5678o`.
- Link ID: numeric ID plus `L` suffix, for example `43235L`.
- Full link name:
  `{link ID}:{input full port ID}:{output full port ID}`
  for example `234234L:1234N5678i:4532N9879o`.

Use one canonical parser and formatter per ID/name type. Avoid ad hoc string
construction outside those helpers.

## Runtime Behavior

### Link Objects

When a link is created, the input node must provide a value of type `any` to the
workspace. The workspace passes that value to the output node.

The value is part of the contract for the link type. It may be:

- a one-time configuration struct
- a callback function
- an interface implementation
- an object with several methods

The core framework must not interpret the object beyond passing it through and
calling lifecycle hooks.

Nodes must type-check or type-assert `any` values accepted during link attach.
If a link object does not match the behavioral contract for the link type, the
node must report the problem during attach and reject link creation early
instead of accepting the link and panicking later.

Link creation must be transactional:

- validate endpoints, direction, type compatibility, multiplicity, ownership,
  and DAG constraints first
- call node hooks in a defined order
- if any hook fails or panics, leave the workspace in its previous valid state
- return a structured error describing the failed phase

### Pull and Push Flow

The type system does not define whether a link is push-based or pull-based.
That behavior belongs to the link type's behavioral contract.

Required supported patterns:

- Pull: the input node asks the output node for data.
- Push: the output node calls into the input node on demand.
- Mixed: for example, the output node calls a `NewConn`-style method on the
  object supplied by the input node, then the input side pulls from the received
  connection object.

The public API and locking strategy must allow all of these patterns without
forcing calls through the workspace lock.

### In-Flight Calls

Nodes and links may be deleted or inactivated while a long-lived call between
nodes is still in flight. For example, a node may be blocked in a pending
`Read`-style operation through a link contract.

The workspace is responsible for notifying affected nodes when:

- the node is deleted
- the node becomes inactive
- one of its links is deleted
- one of its links becomes inactive or broken
- the workspace is closing

The callee that owns the long-lived operation is responsible for terminating or
unblocking that operation according to the link contract, usually by returning
an error or closing a channel/context. The workspace should not try to kill
goroutines directly.

Lifecycle hooks and link objects should make this practical, but the framework
must not require a built-in cancellation primitive. If a link type needs a
context, close callback, channel, or other cancellation signal, that is part of
the contract between the connected nodes.

### Node Lifecycle and Workers

Most nodes do not need background goroutines. They behave like actors that
receive calls and make calls through link contracts.

Some nodes may start background goroutines. The framework must support graceful
shutdown when:

- a node is deleted
- a node becomes inactive
- a class is recalled
- a library is unregistered
- a workspace is closed

Define explicit lifecycle hooks for node implementations. At minimum, plan for:

- initialize from defaults
- initialize from restored state
- before/after link attach
- before/after link detach
- link deleted/inactivated notification
- before becoming inactive
- deleted notification
- close/shutdown
- export private state
- import private state

All lifecycle hooks must be documented with their lock behavior and panic
handling rules.

### Active, Inactive, and Broken State

The implementation should distinguish these cases:

- **Active node**: class and library are available, node is initialized, and
  normal operations are allowed.
- **Inactive node**: class or library is unavailable, initialization failed, a
  lifecycle hook panicked, or the workspace explicitly disabled the node.
- **Active link**: both endpoints are active and the link object is installed.
- **Inactive link**: both endpoints still exist, but one or both endpoints cannot
  currently operate because a node, class, or library is inactive.
- **Broken link**: one or both endpoints no longer exist or the persisted link
  cannot be restored as a valid model object.

Preserve inactive nodes and inactive links in public state when possible,
because editors can display them and users may recover them after loading a
missing library. Remove broken links immediately.

## Workspace API Requirements

### Access Views

Provide separate access surfaces instead of exposing all workspace internals:

- `Workspace`: full owner/controller API.
- `WorkspaceRO`: read-only view for inspectors, renderers, and external code.
- Library-scoped view: limited read/write API for one library.
- Node-scoped view: limited API passed to node implementations.

Read-only views must be safe for concurrent use and must not expose mutable
internal slices, maps, or pointers without defensive copying or immutable
wrappers.

### External Controller API

The workspace should expose methods for an external controller or GUI to perform
all model-valid operations:

- register and unregister libraries
- create and delete nodes
- create and delete links
- move or edit public node metadata
- get and set node coordinate strings
- get and set link waypoint coordinate arrays
- change ports and port types where allowed
- query possible node classes
- query whether a proposed operation is valid
- save and restore workspace state
- copy and paste nodes
- close the workspace

Mutating methods should return typed errors that can be shown in a UI or tested
with `errors.Is` / `errors.As`.

### Copy and Paste

Copying nodes means serializing:

- the node class name
- non-unique public state
- private state
- node coordinate strings
- selected internal links between copied nodes, when copying more than one node
- waypoint coordinate arrays for copied internal links

Pasting creates new node and link IDs. It must not reuse IDs from the copied
data. Links to nodes outside the copied selection should be omitted unless a
future explicit paste mode supports rebinding them.

## Concurrency and Panic Safety

### Locking

All workspace-owned state must be protected by an `RWMutex` or a stricter
internal concurrency model.

Public mutations of nodes, links, classes, and libraries must go through the
workspace or scoped views so locking and validation remain centralized.

Avoid recursive-lock deadlocks. Do not call arbitrary library, class, or node
code while holding the workspace write lock unless the hook contract explicitly
forbids re-entry and the call is proven short. Prefer this pattern:

1. Lock and snapshot the state needed for validation.
2. Unlock.
3. Call external/node/library hooks.
4. Lock again.
5. Verify assumptions still hold.
6. Commit or abort atomically.

Every hook contract must state whether the callee may call back into the
workspace.

### Panic Handling

The workspace must recover from panics in library, class, and node code.

Panic recovery should:

- prevent whole-program termination
- log the panic through the `Logger`
- convert the panic into a structured error when possible
- mark affected nodes, classes, or libraries inactive when continuing with them
  would be unsafe
- preserve the workspace invariants

Never let a panic leave the graph partially mutated.

## Save and Restore

The whole workspace should be saveable and restorable, including private node
state, using `configer.Config` from `github.com/asciimoth/configer/configer` or
a small adapter around it.

`configer.Config` is a JSON-like tree abstraction with path-based `Get`, `Set`,
`Delete`, subtree `View`s, read-only views, snapshots, and struct
marshal/unmarshal helpers. The persistence shape can use ordinary JSON-like
objects and arrays. Node private state should be stored as a `configer.Config`
subtree or view so node implementations can manage their own structured data.

Persist:

- libraries/classes by name, not by pointers
- node IDs and public state
- private node state as configer data
- node coordinate strings
- port IDs and port metadata
- link IDs, endpoint IDs, link type, and active/inactive status
- link waypoint coordinate arrays
- workspace ID generator state, or enough information to continue without ID
  collisions

On restore:

1. Load persisted data into an intermediate representation.
2. Validate names, IDs, endpoint references, and type compatibility.
3. Recreate nodes.
4. Initialize node public/private state.
5. Recreate links in a deterministic DAG-safe order.
6. Mark nodes or links inactive when required libraries or classes are
   unavailable.
7. Remove or reject broken links whose endpoints do not exist or cannot form a
   valid model object.

Restore order for nodes should be deterministic. Use the graph ordering implied
by the DAG: nodes without outgoing links to still-uncreated nodes go first, with
node ID as the tie breaker.

Save output should also be deterministic so golden tests are practical.

## Documentation

Add code doc comments for all public types, methods, errors, and lifecycle
contracts.

Create `ARCHITECTURE.md` with:

- the core domain model
- ownership and access-view boundaries
- lifecycle and link creation sequence
- locking rules
- save/restore format overview
- active/inactive semantics and immediate broken-link removal

Create a minimal `AGENTS.md` with:

- how to run tests
- package layout
- development conventions
- warning not to bypass workspace validation/locking

## Testing Strategy

Aim for very high test coverage, with 100% as the target for core validation,
ID/name parsing, graph mutation, and lifecycle behavior.

Tests should cover:

- name and ID validation
- node creation/deletion
- port type compatibility
- port multiplicity
- link creation/deletion
- DAG enforcement
- class recall and library unregister behavior
- inactive and broken state handling
- copy/paste ID remapping
- node coordinate and link waypoint persistence
- save/restore round trips
- deterministic save output
- lifecycle hook ordering
- deletion/inactivation notification for in-flight calls
- panic recovery
- concurrent read/write behavior
- recursive-lock risk cases

Run all tests with:

```sh
go test ./...
go test -race ./...
```

## Suggested Implementation Order

1. Define package-level errors, ID/name types, parsers, formatters, and tests.
2. Define public data structs for node, port, link, class, and library metadata.
3. Implement `Workspace` storage, locking, logger integration, and read-only
   snapshots.
4. Implement library registration, class definition, class recall, and scoped
   library views.
5. Implement node creation, deletion, active/inactive state, and basic lifecycle
   hooks.
6. Implement node private-state updates and opaque node coordinate storage.
7. Implement port updates and type compatibility validation.
8. Implement link creation/deletion, link objects, multiplicity checks, waypoint
   storage, in-flight call notifications, and DAG enforcement.
9. Add panic recovery around all external hooks.
10. Add worker shutdown/close semantics.
11. Implement copy/paste for selected nodes and internal links.
12. Implement save/restore through `configer` or an adapter layer.
13. Add `ARCHITECTURE.md` and `AGENTS.md`.
14. Tighten concurrency tests and run the full suite with `-race`.

## Resolved Design Decisions

These decisions are part of the implementation contract:

- Broken vs inactive links: broken links are removed immediately; inactive links
  are preserved.
- Exact lifecycle hook interface names and signatures: choose names and
  signatures during implementation, but document every hook contract.
- Exact serialization shape expected by `configer`: use any JSON-like
  object/array shape supported by `configer.Config` that is practical for the
  implementation.
- Node private state representation: store it as a `configer.Config` subtree.
- Link setup cancellation primitive: do not require a framework-provided
  primitive; cancellation and unblocking are part of each node/link contract.
- Library cross-link permissions: libraries may create links only between nodes
  they own.
- Undo/redo: implement in higher layers such as UI, not in the core workspace.
