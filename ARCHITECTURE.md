# Pasta Architecture

Pasta is a Go package for node-based editors and runtimes. A `Workspace` owns
registered libraries, node classes, nodes, links, and ID generation. Links are
directed from one output port to one input port and the workspace keeps the graph
as a DAG.

## Boundaries

`Workspace` is the owner/controller API. `WorkspaceRO` exposes defensive
snapshots and targeted class queries for renderers and inspectors.
`LibraryScope` limits a library to defining its own classes, querying its own
classes, and mutating nodes and links owned by that same library. `NodeScope` is
passed to node runtimes and limits them to mutating their own node state,
private data, coordinates, and ports.

Controller-facing `Can...` methods validate common edits without changing the
model. They cover node creation/deletion, link creation/deletion, link waypoint
updates, and linked port replacement. Library-scoped query methods apply the
same ownership checks as their corresponding scoped mutations.

Node metadata can be replaced as a whole map or edited one key at a time through
the workspace, library-scoped, and node-scoped mutation APIs. Snapshots and
persistence always receive defensive metadata copies.

Ephemeral node messages are transient text notifications of type `note`, `warn`,
or `err`. They can be attached through the full workspace API, through the
owning library scope, or by the node runtime through `NodeScope`, and can be
removed later. Message watchers subscribe to add/remove events for external
renderers such as popup UIs. Messages are exposed in current snapshots, but are
not model state for save, copy, paste, or restore.

Ephemeral node menus are JSON-serializable control documents that combine a
simple UI schema with current state. They support blocks, scalar fields,
read-only fields, fixed value lists, checkboxes, buttons, and repeatable item
templates. Menus can be replaced or cleared by the workspace, owning library, or
node scope. External state updates are validated against the current schema and
may be accepted, rejected, or normalized by an optional runtime hook before
commit. Menu watchers observe replacement, clearing, accepted state changes, and
button triggers. Menus are exposed in current snapshots, but are not model state
for save, copy, paste, or restore.

Applications provide node behavior and type contracts. The core package stores
public metadata, private state values, coordinates, waypoints, and link objects
without interpreting application-specific behavior.

The core deliberately does not implement undo/redo. Higher layers such as
editors or controllers can build command histories around the validated
workspace mutation API.

## Domain Model

Libraries define classes under their own qualified-name prefix. A class provides
default node state, default ports, metadata, optional single-node cardinality,
and the optional runtime factory used to initialize each active node instance.
Nodes keep a stable ID, class name, owning library name, active/inactive state,
dynamic public/private state, ports, and an optional runtime value.

Single-node classes set `ClassSpec.SingleNode` and may have zero or one node in
the workspace. `CanCreateNode`, `CreateNode`, and `Paste` reject attempts to add
another node of that class with `ErrMultiplicity`. During restore, if persisted
data contains multiple nodes of a single-node class, the workspace preserves the
node with the lowest `NodeID` and discards the others before link validation and
before any node initialization or private-state import hooks run. Links attached
to discarded nodes are treated as broken persisted links and are skipped.

Links connect one output `FullPortID` to one input `FullPortID`, carry one fixed
type name, and may store opaque waypoint strings for editors. Link endpoints are
directional: each runtime sees the link through a `LinkEndpoint` whose `Self`
field is its own port and whose `Peer` field is the other endpoint. Link objects
are application-owned values; the workspace only hands them from the input-side
provider or caller to both attach hooks.

Each port has either one fixed type or an accepted set of types. A link always
has one fixed type, and at least one endpoint involved in link creation must
provide a fixed type. The chosen link type is normally the type of the port
where a UI started the drag, but callers may request a type explicitly. A link
may attach only to a fixed port of that same type or to a flexible port whose
accepted set contains that type. Unlinked ports may freely change their fixed
type or accepted set; linked ports may only change in ways that keep every
attached link valid. Type compatibility is implemented through the same pure
validation path used by `Can...` queries and mutating methods.

Node coordinates and link waypoints are opaque editor strings. The workspace
stores, copies, and persists them, but does not parse coordinate systems,
layouts, or path geometry.

## Validation

Names are centralized in `names.go`; IDs and composed link names are centralized
in `ids.go`. Qualified class and type names are URL-like ASCII names containing
letters, digits, dots, dashes, and slashes, and must start with a letter. Library
names are domain-like ASCII names containing letters, digits, dots, and dashes.
Classes must be defined under their library prefix and start with an uppercase
letter after that prefix. Type names start with a lowercase letter after their
library-like prefix; any library may use any valid type name, so the prefix is a
namespace rather than an access-control rule.

Workspace-generated IDs are stable across save/restore and unique within their
object kind. Canonical forms are `123N` for nodes, `456i` or `456o` for ports,
`123N456o` for full ports, `789L` for links, and
`789L:123N456i:321N654o` for full link names.

Link creation validates endpoint existence, direction, type compatibility,
input multiplicity, ownership for scoped callers, and DAG safety before
committing state.

Broken links, where an endpoint or port no longer exists, are removed
immediately. Inactive links, where endpoints still exist but a class or library
is unavailable, are preserved for editor recovery. Defining a missing or
recalled class reactivates preserved nodes and links when their endpoints and
ports are still valid, and reinitializes recovered node runtimes outside the
workspace lock.

Libraries may be registered and unregistered at runtime. Registering a library
asks it to define its currently known classes; registered libraries may define
more classes later or recall their own classes. Recalled classes, unregistered
libraries, and missing libraries/classes on restore make affected nodes inactive
instead of deleting them. A library scope may define and recall only its own
classes, mutate only its own nodes, and create or mutate links only when both
endpoint nodes are owned by that library.

## Lifecycle

Node runtimes are initialized by `NodeClass.InitNode`. The call receives a
`NodeContext`, an initialized `NodeState`, and an `InitMode` of `new` or
`restore`. If the runtime implements `NodePrivateImportHook`, the workspace calls
it immediately after initialization with the cloned private state. Runtime
initialization happens outside the workspace lock; a node-scoped API remains
valid during initialization and is finalized after the runtime is committed or
rolled back.

Link creation follows a transactional sequence:

1. Reserve a link ID and validate endpoints, directions, type compatibility,
   input multiplicity, and DAG safety under the workspace lock.
2. Release the lock and obtain the link object from the input runtime when the
   caller did not provide one.
3. Call input then output `BeforeLinkAttach` hooks outside the lock.
4. Reacquire the lock, revalidate the pending link, and commit it.
5. Call input then output `AfterLinkAttach` hooks outside the lock.

If link-object creation or a before-attach hook fails or panics, the reserved ID
is rolled back and the workspace graph is unchanged. After-attach panics are
logged but do not roll back the committed link.

Link objects are opaque `any` values whose behavioral contract belongs to the
link type. They may be configuration structs, callback functions, interface
implementations, or richer objects. Endpoint runtimes are responsible for
type-checking the object during attach and rejecting incompatible values early.
The framework does not define whether a link is push-based, pull-based, or
mixed; those call patterns happen directly between runtimes and are not forced
through the workspace lock.

Nodes and links can be deleted or inactivated while a long-lived inter-node call
is in flight. The workspace notifies affected runtimes through deletion,
detachment, inactivation, broken-link, and close hooks. The runtime or link
contract is responsible for unblocking ongoing work, typically through an error,
closed channel, context, callback, or another type-specific mechanism. Pasta
does not require a built-in cancellation primitive and does not try to stop
runtime goroutines directly.

Link deletion calls input then output `BeforeLinkDetach` hooks outside the lock,
removes the link, and then calls input then output `AfterLinkDetach` hooks.
Deleting a node first calls `BeforeDelete`, detaches its links through the same
link deletion path, removes the node, then calls `AfterDelete` and `Close`.
Links pruned during invariant repair, such as links made invalid by class
redefinition, cannot veto removal; after the repair commits, the workspace calls
their `AfterLinkDetach` hooks as deletion/broken-link notifications.

Class recall, library unregister, and workspace close gather affected active
node and link events under the lock, call `BeforeInactive` hooks outside the
lock, commit inactive state, then call `AfterInactive`,
`AfterLinkInactive`, and `Close` outside the lock. Preserved inactive nodes have
their runtimes cleared. If those nodes later recover because their class or
library becomes active again, the workspace initializes fresh runtimes in
deterministic DAG order.

All external lifecycle calls are panic-recovered. Panics are logged through the
configured `Logger`; before-hook panics become operation errors, while
after-hook panics are logged after the state change has committed.

## Locking

Workspace-owned state is protected by an `RWMutex`. Snapshot and lookup methods
return copies rather than internal slices or maps. Public mutation methods are
the intended synchronization boundary for editors, controllers, scoped library
access, and node-scoped runtime updates.

Node initialization and link lifecycle hooks run outside the workspace lock, then
mutations revalidate before commit. The implementation recovers panics from
library registration and node lifecycle hooks. Library registration snapshots
the workspace model before class-definition hooks and restores it if the hook
returns an error or panics, so partial class reactivation does not leak into the
workspace.

`NodeCloseHook` is the runtime shutdown hook. It runs outside the workspace lock
after a node is deleted, after a node becomes inactive through class recall or
library unregister, and after workspace close inactivation notifications. When a
preserved inactive node is later recovered, the workspace initializes a fresh
runtime instead of reusing the closed one.

## Persistence

`Save` produces deterministic `SaveData`: nodes and links are sorted, IDs are
formatted through canonical helpers, and ID generator state is included. Private
node state is stored in the DTO as JSON-like `any` values. `SaveConfig` and
`SaveConfigWithRuntimeState` wrap that DTO in a `configer.Config` tree for
callers that want path-based config access, and `RestoreConfig` restores from
that config-backed shape.
Runtimes that own volatile private state can implement `NodePrivateExportHook`;
`SaveWithRuntimeState` and `Copy` call that hook outside the workspace lock and
use the exported value in the saved or clipboard data. Runtimes that need an
explicit import callback can implement `NodePrivateImportHook`, which runs after
node initialization with the default or restored private value.

`Restore` validates IDs, ports, endpoint references, type compatibility, and DAG
safety before active links are accepted. Missing classes restore nodes as
inactive. Persisted links whose endpoint nodes or ports are missing are skipped
as broken, including links to duplicate nodes discarded for single-node classes.
Persisted links that reference existing endpoints but violate type,
multiplicity, duplicate-ID, or DAG constraints reject the restore and roll the
workspace back to its previous state.

Restore initializes active nodes in deterministic DAG order: nodes without
outgoing links to still-uncreated nodes come first, with node ID as the tie
breaker.

The persistence DTO is intentionally small:

- `SaveData.NextNode` and `SaveData.NextLink` preserve generator progress.
- `SaveNode` stores the canonical node ID, class name, dynamic state, and port
  specs.
- `SaveLink` stores the canonical full link name, fixed link type, and
  waypoint strings.

The DTO stores only model state, not Go runtime values or link objects. Runtime
state that should survive save/copy must be exported through
`NodePrivateExportHook` into the node private state field.
Ephemeral node messages are intentionally excluded from the DTO and clipboard
data, and restore clears messages from the previous workspace contents.

## Copy And Paste

`Copy` serializes selected nodes with class names, public state, private state,
coordinates, current ports, and internal links whose endpoints are both in the
selection. Copied internal links include their type and waypoint strings. Links
to nodes outside the selection are omitted.

`Paste` creates new node and link IDs and never reuses copied IDs. Pasted active
nodes are initialized with `InitModeRestore`, matching restore semantics for
private and public state.
