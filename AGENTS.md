# Repository Guidelines

For details see ARCHITECTURE.md

## Project Structure & Module Organization

This repository contains a Go workspace for `github.com/asciimoth/pasta/pasta`.
The main module lives in `pasta/`; root-level files define tooling and CI.

- `pasta/`: core headless graph framework, workspace, nodes, ports, links, undo/restore, persistence, and notifications.
- `pasta/std/`: standard node and type implementations such as bool, math, comparison, select, and string nodes.
- `*_test.go`: tests are colocated with the package they cover.
- `README.md`: public overview and API usage notes.
- `Justfile`, `flake.nix`, `typos.toml`: development commands, Nix shell, and spelling configuration.

## Build, Test, and Development Commands
Prefer running commands from the repository root:
- `just test`: run `go test ./...` in `pasta/` with the race detector and no test cache.
- `just coverage`: generate `coverage.out` for Coveralls-compatible reporting.
- `just vet`: run `go vet ./...`.
- `just lint`: run `golangci-lint` across `./pasta/...`.
- `just tidy`: run `go mod tidy` in `pasta/` and sync `go.work`.

## Coding Style & Naming Conventions
Use golangci-lint for linting and romatting and keep package names short and lowercase.
Exported identifiers should be clear API names with doc comments when they are part of the public surface.
Tests should use descriptive names such as `TestWorkspaceSaveRestore` or table-driven subtests when covering variants. 
Keep implementation files grouped by concept, following existing patterns like `node_string_upper.go`, `resource.go`, and `undo_test.go`.

## Testing Guidelines
Add or update colocated `*_test.go` files for behavioral changes.

