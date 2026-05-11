# Pasta Architecture

Pasta is a Go package for node-based editors and runtimes. A `Workspace` owns
registered libraries, node classes, nodes, links, and ID generation. Links are
directed from one output port to one input port and the workspace keeps the graph
as a DAG.

## Boundaries

`Workspace` is the owner/controller API. `WorkspaceRO` exposes defensive
snapshots for renderers and inspectors. `LibraryScope` limits a library to
defining its own classes and mutating nodes and links owned by that same library.
`NodeScope` is passed to node runtimes and limits them to mutating their own
node state, private data, coordinates, and ports.

Applications provide node behavior and type contracts. The core package stores
public metadata, private state values, coordinates, waypoints, and link objects
without interpreting application-specific behavior.

## Validation

Names are centralized in `names.go`; IDs and composed link names are centralized
in `ids.go`. Link creation validates endpoint existence, direction, type
compatibility, input multiplicity, ownership for scoped callers, and DAG safety
before committing state.

Broken links, where an endpoint or port no longer exists, are removed
immediately. Inactive links, where endpoints still exist but a class or library
is unavailable, are preserved for editor recovery. Defining a missing or
recalled class reactivates preserved nodes and links when their endpoints and
ports are still valid, and reinitializes recovered node runtimes outside the
workspace lock.

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
node state is stored as `any` so callers can use a JSON-like/config tree.
Runtimes that own volatile private state can implement `NodePrivateExportHook`;
`SaveWithRuntimeState` and `Copy` call that hook outside the workspace lock and
use the exported value in the saved or clipboard data. Runtimes that need an
explicit import callback can implement `NodePrivateImportHook`, which runs after
node initialization with the default or restored private value.

`Restore` validates IDs, ports, endpoint references, type compatibility, and DAG
safety. Missing classes restore nodes as inactive. Broken links are skipped.
