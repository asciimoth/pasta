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
validation rules, state management, persistence, runtime
lifecycle hooks, and test helpers that a GUI, API server, runtime host, or other
frontend can build on top of.

## Node menus
Nodes can expose interactive per-node menus using the
[`github.com/asciimoth/formular`](https://pkg.go.dev/github.com/asciimoth/formular)
JSON protocol types. In this model the node is the Formular backend, the
workspace is a cache plus demultiplexer, and external GUI, TUI, web, or RPC code
is the frontend.

Each node menu uses `NodeMenuID(nodeID)`, which formats menu IDs as
`NODE{{NODE ID}}MENU`, for example `NODE12MENU`. Menus are not part of workspace
snapshots, are not supplied by node-add APIs, and are cleared when a node
implementation is replaced. A node should build its menu from `OnInit` or later
by calling `Workspace.SendNodeMenuMsg(nodeID, formularMessage)`. The workspace
applies cacheable messages to that node's `formular.MenuSnapshotState` and then
forwards copies only to watchers subscribed to that node menu.

Workspace notification subscribers do not receive node menu traffic by default.
A frontend that wants to show a clicked node menu should call
`SubscribeNotifications`, then `SubscribeNodeMenu(nodeID, subscriptionID)`.
If the workspace already has cached menu state, the subscriber immediately
receives a `NotificationNodeMenu` carrying a forced Formular `menu.snapshot`.
Call `UnsubscribeNodeMenu` when the menu is hidden.

Frontend-to-backend Formular messages should be sent with
`Workspace.SendNodeFormularMsg(nodeID, message)`. Missing nodes, placeholders,
closed workspaces, and nil messages are dropped silently. Live nodes receive the
message through `Node.OnFormularMsg`.

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
