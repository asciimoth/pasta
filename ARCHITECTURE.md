# Pasta Architecture

Pasta is a Go package for node-based editors and runtimes. A `Workspace` owns
registered libraries, node classes, nodes, links, and ID generation. Links are
directed from one output port to one input port and the workspace keeps the graph
as a DAG.

## Boundaries

`Workspace` is the owner/controller API. `WorkspaceRO` exposes defensive
snapshots for renderers and inspectors. `LibraryScope` limits a library to
defining its own classes and mutating nodes and links owned by that same library.

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
is unavailable, are preserved for editor recovery.

## Locking

Workspace-owned state is protected by an `RWMutex`. Snapshot and lookup methods
return copies rather than internal slices or maps. Public mutation methods are
the intended synchronization boundary for editors, controllers, and scoped
library access.

The current implementation keeps class-definition hooks outside the initial
registration lock and recovers panics from library registration. Node lifecycle
hooks are intentionally not wired yet; when added, hook contracts must document
lock and re-entry behavior.

## Persistence

`Save` produces deterministic `SaveData`: nodes and links are sorted, IDs are
formatted through canonical helpers, and ID generator state is included. Private
node state is stored as `any` so callers can use a JSON-like/config tree.

`Restore` validates IDs, ports, endpoint references, type compatibility, and DAG
safety. Missing classes restore nodes as inactive. Broken links are skipped.
