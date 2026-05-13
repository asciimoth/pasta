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
validation rules, state management, persistence, scoped mutation APIs, runtime
lifecycle hooks, and test helpers that a GUI, API server, runtime host, or other
frontend can build on top of.

## What Pasta Provides
- A concurrent-safe `Workspace` that owns libraries, node classes, nodes, links,
  ID generation, snapshots, copy/paste, save/restore, and close/inactivation.
- Directed links from output ports to input ports, with DAG validation, endpoint
  validation, input multiplicity checks, and type compatibility checks.
- Application-defined libraries and node classes with default state, typed ports,
  metadata, and optional runtime factories.
- Runtime lifecycle hooks for node initialization, link attach/detach,
  inactivation, deletion, private-state import/export, and runtime-provided link
  objects.
- Persistent graph model data through `Save`, `Restore`, config-backed save
  helpers, and clipboard-oriented `Copy`/`Paste`.
- Opaque editor values for node coordinates and link waypoints, so each UI can
  use its own layout and routing format.
- Ephemeral node menus and messages for GUI controls, popups, diagnostics, and
  other transient frontend state.
- Preservation of inactive nodes and links when their model endpoints still
  exist, allowing editors to recover graphs after missing libraries, class
  recall, or library unregister events.

Pasta intentionally does not decide whether a graph is push-based, pull-based, or
mixed. Link objects and node runtimes are application-owned Go values, so a
library can implement callbacks, channels, interface contracts, shared objects,
or any other communication model appropriate for its domain.

## Repository Contents
- `pasta/`: the Go framework package.
- `pasta/examples/`: a calculator node library showing push, pull, mixed flow,
  menus, save/restore, and copy/paste.
- `pasta/pastatest/`: reusable conformance tests and helpers for downstream
  Pasta libraries.
- `demo/`: a static web demo using Pasta compiled to WebAssembly as the backend
  and LiteGraph as the browser editor.
- `tutorials/`: step-by-step guides for workspace usage, library creation, and
  UI integration.

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

