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
- Library-scoped class definition, class recall, node mutation, and link
  mutation.
- Node creation and deletion.
- Public node state updates and opaque node coordinate storage.
- Synchronized private node state updates through workspace and library-scoped APIs.
- Node-scoped runtime API for a node to update its own state, private data,
  coordinates, and ports through workspace validation and locking.
- Dynamic node port replacement with validation that existing links remain valid.
- Link creation and deletion.
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
- Link inactive notifications when preserved links become inactive.
- Before/after node inactive hooks for class recall, library unregister, and
  workspace close.
- Before/after node delete hooks.
- Node close hooks when the workspace closes.
- Link type compatibility validation.
- Input port multiplicity validation.
- DAG enforcement for links.
- Opaque link waypoint storage.
- Active and inactive state propagation for class recall and library unregister.
- Immediate removal of broken links when nodes or ports disappear.
- Copy/paste for selected nodes and internal links with ID remapping.
- Deterministic `SaveData` DTOs and basic restore path.
- Deterministic restore runtime initialization using DAG ordering.
- Initial `ARCHITECTURE.md` and `AGENTS.md`.
- Tests for:
  - name validation
  - ID round trips
  - link multiplicity
  - DAG/cycle rejection
  - inactive link preservation
  - broken link removal
  - linked port update validation
  - save/restore
  - deterministic save output
  - deterministic restore initialization order
  - copy/paste ID remapping
  - private state updates in snapshots, save, and copy
  - lifecycle hook order
  - restore lifecycle initialization and rollback
  - link attach rollback on hook errors and panics
  - link attach hook read-only workspace re-entry
  - link creation revalidation after concurrent interleavings
  - inactive hook notifications and rollback
  - panic recovery across lifecycle hook families
  - library-scoped ownership enforcement for classes, nodes, and links
  - node-scoped runtime updates and deleted/closed scope errors

## Verified

These commands pass:

```sh
go test ./pasta/...
go test -race ./pasta/...
go vet ./pasta/...
```

## Still To Do

- Complete node lifecycle hooks:
  - richer link deleted/inactivated notifications if needed by link contracts
  - export/import private state
- Add explicit worker shutdown/close semantics for nodes with goroutines.
- Expand library/class runtime behavior:
  - late class definition after registration
  - class recall recovery
  - library unregister/register recovery
  - reactivation of preserved inactive nodes and links
- Replace or wrap the current JSON-like `any` persistence shape with a concrete
  `github.com/asciimoth/configer` integration.
- Expand controller/query APIs:
  - possible class queries
  - richer "can I do this?" validation queries
  - metadata editing helpers
  - disable/enable APIs if needed
- Add lifecycle hook-order tests.
- Add concurrent read/write and recursive-lock risk tests.
- Add tests for restore edge cases, inactive recovery, and broken persisted
  links.
- Expand `ARCHITECTURE.md` after lifecycle and persistence contracts are final.

## Notes

- Inactive links are preserved when both endpoints still exist but cannot
  operate.
- Broken links are removed immediately when an endpoint node or port no longer
  exists.
- The current save/restore implementation stores private state as `any`; callers
  should treat that as provisional until the persistence adapter is finalized.
