# Pasta Progress

This file records what has been implemented from `plan.md` and what remains.

## Current Layout

The repo is a Go workspace with the framework module in `./pasta`.

Run tests from the repo root with:

```sh
go test ./pasta/...
go test -race ./pasta/...
```

Or from inside `./pasta`:

```sh
go test ./...
go test -race ./...
```

`go test ./...` from the repo root is not the right command for this workspace
layout because the repo root is not itself a Go module.

## Implemented

- Centralized library, class, and type name validation in `pasta/names.go`.
- Canonical ID and full-link-name parsers/formatters in `pasta/ids.go`.
- Public model structs and interfaces in `pasta/model.go`.
- Structured sentinel errors in `pasta/errors.go`.
- `Workspace` storage protected by `sync.RWMutex`.
- Defensive read-only snapshots for workspace, nodes, and links.
- Library registration and unregister.
- Library-scoped class definition, class recall, node state/coordinate/port
  mutation, and link mutation.
- Node creation and deletion.
- Public node state updates and opaque node coordinate storage.
- Public node metadata updates through workspace, library-scoped, and
  node-scoped APIs.
- Single-key public node metadata editing helpers through workspace,
  library-scoped, and node-scoped APIs.
- Synchronized private node state updates through workspace and library-scoped APIs.
- Node-scoped runtime API for a node to update its own state, private data,
  coordinates, and ports through workspace validation and locking.
- Read-only class lookup for editor/controller and runtime inspection.
- Deterministic read-only class list queries for all classes and
  library-filtered classes.
- Optional private state export/import hooks for runtime-owned state.
- Dynamic node port replacement with validation that existing links remain valid.
- Link creation and deletion.
- Library-scoped link waypoint updates.
- Link creation prepares under lock, runs node hooks outside the workspace lock,
  then revalidates before commit.
- Optional node class/runtime lifecycle interfaces.
- Node runtime initialization for new nodes and pasted nodes.
- Node runtime initialization for restored workspace nodes.
- Panic-safe lifecycle hook execution.
- Panic recovery coverage for library registration, node initialization, link
  attach/detach hooks, inactive hooks, delete hooks, and close hooks.
- Link-object handoff from the input node when no object is supplied by the caller.
- Before/after link attach hooks.
- Before/after link detach hooks for direct link deletion and node deletion.
- Committed after-detach notifications for links pruned as broken during class
  redefinition.
- Committed after-detach notifications for recovered inactive links pruned
  during library registration.
- Link inactive notifications when preserved links become inactive.
- Before/after node inactive hooks for class recall, library unregister, and
  workspace close.
- Before/after node delete hooks.
- Node close hooks for node deletion, class recall, library unregister, and
  workspace close.
- Link type compatibility validation.
- Input port multiplicity validation.
- Non-mutating validation query for proposed node port replacement.
- Non-mutating validation queries for node creation, node deletion, link
  creation, link waypoint updates, and link deletion.
- Library-scoped validation queries with the same ownership boundaries as
  scoped mutations.
- DAG enforcement for links.
- Opaque link waypoint storage.
- Active and inactive state propagation for class recall and library unregister.
- Immediate removal of broken links when nodes or ports disappear.
- Deterministic removal of preserved links that become invalid during inactive
  recovery because of port type or multiplicity changes.
- Copy/paste for selected nodes and internal links with ID remapping.
- Paste validates clipboard node ports before initialization and commit.
- Deterministic `SaveData` DTOs and basic restore path.
- `github.com/asciimoth/configer/configer` save/restore adapter helpers.
- Restore skips broken persisted links, but rejects invalid persisted link
  constraints such as duplicate link IDs, type mismatches, multiplicity
  violations, and cycles.
- Restore rejects duplicate persisted link IDs even when an earlier duplicate
  link would otherwise be skipped as broken.
- Error-returning save path that exports current private state from live
  runtimes while preserving the stable snapshot-only `Save` API.
- Deterministic restore runtime initialization using DAG ordering.
- Late class definition can reactivate preserved inactive nodes and links.
- Class recall recovery reinitializes recovered node runtimes.
- Library unregister/register recovery reinitializes recovered node runtimes.
- Late class definition restores library ownership for recovered nodes.
- Library registration rolls back partial class definitions and reactivation on
  hook errors or panics.
- Initial `ARCHITECTURE.md` and `AGENTS.md`.
- Expanded `ARCHITECTURE.md` with domain model, lifecycle/link creation
  sequence, locking behavior, and persistence DTO details.
- Tests for:
  - name validation
  - ID round trips
  - link multiplicity
  - DAG/cycle rejection
  - inactive link preservation
  - broken link removal
  - linked port update validation
  - non-mutating controller validation queries
  - save/restore
  - configer-backed save/restore round trip
  - deterministic save output
  - deterministic restore initialization order
  - broken persisted link skipping
  - invalid persisted node IDs, duplicate node IDs, invalid persisted class
    names, invalid saved ports, and rollback
  - invalid persisted link constraints and rollback
  - duplicate persisted link IDs across skipped broken links
  - copy/paste ID remapping
  - invalid clipboard ports and paste rollback
  - node metadata update helpers, single-key edits, and defensive metadata snapshots
  - private state updates in snapshots, save, and copy
  - lifecycle hook order
  - restore lifecycle initialization and rollback
  - link attach rollback on hook errors and panics
  - link attach hook read-only workspace re-entry
  - link creation revalidation after concurrent interleavings
  - inactive hook notifications and rollback
  - panic recovery across lifecycle hook families
  - library-scoped ownership enforcement for classes, node state/coordinate/port
    edits, and links
  - library-scoped link waypoint updates
  - read-only class lookup/list queries and defensive class snapshots
  - node-scoped runtime updates and deleted/closed scope errors
  - class definition reactivation and rollback
  - library ownership restoration for recovered nodes
  - class definition recovery pruning incompatible restored links
  - library unregister/register recovery and rollback
  - explicit lifecycle hook order for node deletion and workspace close with
    attached links
  - detach notifications for links pruned as broken during class redefinition
    and library registration recovery
  - runtime close/shutdown on delete, inactive transitions, and workspace close
  - runtime private state export/import, defensive copy behavior, and rollback
    on hook failures
  - concurrent read/write smoke coverage under the race detector
  - recursive-lock risk coverage for node lifecycle hooks that read workspace
    snapshots

## Verified

These commands pass:

```sh
go test ./pasta/...
go test -race ./pasta/...
go vet ./pasta/...
```

## Still To Do

- Add tests for any remaining restore edge cases and inactive recovery paths as
  lifecycle and persistence contracts evolve.

## Notes

- Inactive links are preserved when both endpoints still exist but cannot
  operate.
- Broken links are removed immediately when an endpoint node or port no longer
  exists.
- The DTO save/restore implementation stores private state as JSON-like `any`;
  callers that need path-based config access can use the configer adapter.
