import {
  applyNotification,
  DEFAULT_NODE_TEXT_COLOR,
  DEFAULT_NODE_TITLE_COLOR,
  emptySnapshot,
  formatPosition,
  linkColor,
  nodeMenuID,
  parsePosition,
  selectedNodeIDs,
  isKeyboardShortcut,
  typeColor,
  type ID,
  type LinkSnapshot,
  type NodeSnapshot,
  type PortSnapshot,
  type WorkspaceNotification,
  type WorkspaceSnapshot,
} from "./pasta_model.js";

declare const Go: new () => { importObject: WebAssembly.Imports; run(instance: WebAssembly.Instance): Promise<void> };
declare const LiteGraph: any;
declare const LGraph: any;
declare const LGraphCanvas: any;
type FormularMenuInstance = {
  feed(message: unknown): void;
  destroy(): void;
};
type FormularMenuConstructor = new (
  container: string | HTMLElement,
  menuID: string,
  send: (message: unknown) => void,
) => FormularMenuInstance;
// The built module runs from public/build, while vendored browser dependencies
// are served from vendor at the demo root.
// @ts-expect-error static browser vendor path
const { FormularMenu } = (await import("../../vendor/formular-menu/formular-menu.js")) as {
  FormularMenu: FormularMenuConstructor;
};

type BackendResponse<T> = { ok: true; result: T } | { ok: false; error: string };
type BackendEvent =
  | { type: "notification"; payload: WorkspaceNotification }
  | { type: "log"; payload: LogEntry };
type LogEntry = { at: string; source: string; level: string; text: string };

declare global {
  interface Window {
    pastaBackendCall?: (raw: string) => string;
    pastaFrontendDispatch?: (raw: string) => void;
    __pastaDemo?: {
      call<T>(method: string, params?: Record<string, unknown>): T;
      snapshot(): WorkspaceSnapshot;
      selectNode(id: number): void;
      selectNodes(ids: number[]): void;
      moveNode(id: number, x: number, y: number): void;
      canConnect(from: number, to: number): boolean;
      nodeScreenPosition(id: number): { x: number; y: number } | null;
      portScreenPosition(port: number): { x: number; y: number } | null;
      highlightedInputPort(): number | null;
    };
  }
}

let snapshot: WorkspaceSnapshot = emptySnapshot();
let graph: any;
let canvas: any;
let rendering = false;
let selectedMenu: { node: ID; menu: FormularMenuInstance } | null = null;
let activeTab = "graph";
let sidekickUpdate = 0;
let graphResizeObserver: ResizeObserver | null = null;
let localClipboard = "";
const frontendLogs: LogEntry[] = [];
const backendLogs: LogEntry[] = [];
const liteLinkToPasta = new Map<number, number>();
const debugClipboard = true;

const el = {
  status: document.querySelector<HTMLElement>("#backend-status")!,
  tabs: [...document.querySelectorAll<HTMLButtonElement>("[data-tab]")],
  panels: [...document.querySelectorAll<HTMLElement>("[data-panel]")],
  graphCanvas: document.querySelector<HTMLCanvasElement>("#graph-canvas")!,
  sidekick: document.querySelector<HTMLElement>("#sidekick")!,
  logs: document.querySelector<HTMLElement>("#logs-list")!,
  config: document.querySelector<HTMLTextAreaElement>("#config-text")!,
  save: document.querySelector<HTMLButtonElement>("#save-config")!,
  reload: document.querySelector<HTMLButtonElement>("#reload-config")!,
  restartFrontend: document.querySelector<HTMLButtonElement>("#restart-frontend")!,
};

function log(source: string, level: string, text: string): void {
  const entry = { at: new Date().toLocaleTimeString("en-US", { hour12: false }), source, level, text };
  frontendLogs.push(entry);
  if (frontendLogs.length > 200) frontendLogs.shift();
  renderLogs();
}

function debugLog(message: string, details: Record<string, unknown> = {}): void {
  if (!debugClipboard) return;
  console.log(`[pasta-demo] ${message}`, details);
}

function backend<T>(method: string, params: Record<string, unknown> = {}): T {
  if (!window.pastaBackendCall) throw new Error("Go backend is not ready");
  const raw = window.pastaBackendCall(JSON.stringify({ method, params }));
  const response = JSON.parse(raw) as BackendResponse<T>;
  if (!response.ok) throw new Error(response.error);
  return response.result;
}

function tryBackend<T>(method: string, params: Record<string, unknown> = {}): T | null {
  try {
    return backend<T>(method, params);
  } catch (error) {
    log("api", "warn", `${method}: ${(error as Error).message}`);
    requestSnapshot();
    return null;
  }
}

function registerLiteGraphNode(): void {
  if (LiteGraph.registered_node_types?.["pasta/view"]) return;
  function PastaViewNode(this: any) {
    this.properties = { pastaId: 0 };
    this.resizable = false;
    this.clonable = false;
    this.removable = false;
  }
  PastaViewNode.title = "Pasta";
  PastaViewNode.prototype.onConnectInput = function (
    inputIndex: number,
    _outputType: string,
    outputSlot: { pastaPortId?: number },
    outputNode: any,
    outputIndex: number,
  ): boolean {
    if (rendering) return true;
    const from = outputSlot?.pastaPortId ?? outputNode.outputs?.[outputIndex]?.pastaPortId;
    const to = this.inputs?.[inputIndex]?.pastaPortId;
    if (from && to) tryBackend("addLink", { from, to });
    return false;
  };
  PastaViewNode.prototype.onConnectionsChange = function (
    _type: number,
    _slot: number,
    isConnected: boolean,
    link: { id?: number; data?: { pastaId?: number } },
  ): void {
    if (rendering || isConnected || !link) return;
    const id = link.data?.pastaId ?? (link.id ? liteLinkToPasta.get(link.id) : undefined);
    if (id) tryBackend("removeLink", { id });
  };
  PastaViewNode.prototype.onDrawForeground = function (ctx: CanvasRenderingContext2D): void {
    drawNodeLabel(ctx, this);
  };
  PastaViewNode.prototype.onSelected = scheduleSidekickUpdate;
  PastaViewNode.prototype.onDeselected = scheduleSidekickUpdate;
  LiteGraph.registerNodeType("pasta/view", PastaViewNode);
}

function initGraph(): void {
  registerLiteGraphNode();
  graph = new LGraph();
  canvas = new LGraphCanvas("#graph-canvas", graph);
  canvas.node_title_color = DEFAULT_NODE_TEXT_COLOR;
  canvas.onNodeMoved = flushNodePositions;
  canvas.onSelectionChange = scheduleSidekickUpdate;
  resizeGraphCanvas();
  graphResizeObserver = new ResizeObserver(resizeGraphCanvas);
  graphResizeObserver.observe(el.graphCanvas);
  graph.start();
  el.graphCanvas.addEventListener("mouseup", flushNodePositions);
  document.addEventListener("pointerup", flushNodePositions, { capture: true });
  window.addEventListener("blur", flushNodePositions);
  el.graphCanvas.addEventListener("keydown", graphKeydown, { capture: true });
  el.graphCanvas.tabIndex = 0;
  requestAnimationFrame(resizeGraphCanvas);
}

function resizeGraphCanvas(): void {
  if (!canvas) return;
  const rect = el.graphCanvas.getBoundingClientRect();
  const width = Math.max(1, Math.round(rect.width));
  const height = Math.max(1, Math.round(rect.height));
  canvas.resize(width, height);
}

function renderGraph(): void {
  if (!graph) return;
  rendering = true;
  liteLinkToPasta.clear();
  const selected = new Set(selectedNodeIDs(canvas?.selected_nodes));
  graph.clear();

  for (const [idText, nodeSnapshot] of Object.entries(snapshot.nodes)) {
    const id = Number(idText);
    const node = LiteGraph.createNode("pasta/view");
    node.id = id;
    node.properties.pastaId = id;
    node.properties.pastaLabel = nodeSnapshot.label;
    node.title = nodeTitle(nodeSnapshot);
    const primaryColor = typeColor(nodeSnapshot.primary_type);
    node.color = nodeSnapshot.placeholder ? "#f38ba8" : primaryColor?.color ?? DEFAULT_NODE_TITLE_COLOR;
    node.bgcolor = nodeSnapshot.has_root_path ? "#313244" : "#1e1e2e";
    node.boxcolor = nodeSnapshot.root ? "#a6e3a1" : "#45475a";
    node.pos = parsePosition(nodeSnapshot.position, 80 + graph._nodes.length * 24, 80 + graph._nodes.length * 24);
    for (const portID of nodeSnapshot.left_ports) addPort(node, snapshot.ports[String(portID)], portID);
    for (const portID of nodeSnapshot.right_ports) addPort(node, snapshot.ports[String(portID)], portID);
    node.size = node.computeSize();
    fitLabelInNode(node, nodeSnapshot);
    graph.add(node);
  }

  for (const [idText, link] of Object.entries(snapshot.links)) {
    connectLink(Number(idText), link);
  }

  graph.setDirtyCanvas(true, true);
  if (selected.size) {
    selectGraphNodes([...selected]);
  }
  rendering = false;
  updateSidekick();
}

function selectGraphNodes(ids: number[]): void {
  if (!canvas || !graph) return;
  const nodes = ids.map((id) => graph.getNodeById(id)).filter(Boolean);
  if (nodes.length) canvas.selectNodes(nodes);
}

function nodeTitle(node: NodeSnapshot): string {
  return node.name;
}

function fitLabelInNode(node: any, snapshot: NodeSnapshot): void {
  if (!snapshot.label) return;
  const labelWidth = Math.min(280, 32 + snapshot.label.length * 7);
  node.size[0] = Math.max(node.size[0], labelWidth);
  node.size[1] += 24;
}

function drawNodeLabel(ctx: CanvasRenderingContext2D, node: any): void {
  const label = node.properties?.pastaLabel;
  if (!label || node.flags?.collapsed) return;
  ctx.save();
  ctx.font = "12px Arial";
  ctx.fillStyle = "#cdd6f4";
  ctx.textAlign = "center";
  ctx.textBaseline = "bottom";
  ctx.fillText(String(label), node.size[0] * 0.5, node.size[1] - 7, node.size[0] - 18);
  ctx.restore();
}

function addPort(node: any, port: PortSnapshot | undefined, portID: number): void {
  if (!port) return;
  const type = liteGraphPortType(port.types);
  const extra = { pastaPortId: portID, label: port.name, pastaTypes: port.types };
  if (port.direction === "left") {
    const linkIDs = port.links.length > 1 ? port.links : [undefined];
    for (let i = 0; i < linkIDs.length; i += 1) {
      const name = i === 0 ? port.name : `${port.name} ${i + 1}`;
      node.addInput(name, type, { ...extra, pastaLinkId: linkIDs[i] });
    }
  } else {
    node.addOutput(port.name, type, extra);
  }
}

function liteGraphPortType(types: string[]): string {
  if (!types.length || types.includes("any/any")) return "";
  return types.join(",");
}

function connectLink(id: number, link: LinkSnapshot): void {
  const outNode = graph.getNodeById(link.right_port_node);
  const inNode = graph.getNodeById(link.left_port_node);
  if (!outNode || !inNode) return;
  const outIndex = findPortIndex(snapshot.nodes[String(link.right_port_node)]?.right_ports, link.right_port);
  const inIndex = findInputSlotIndex(inNode, link.left_port, id);
  if (outIndex < 0 || inIndex < 0) return;
  const liteLink = outNode.connect(outIndex, inNode, inIndex);
  if (liteLink) {
    liteLink.color = linkColor(link.type);
    liteLink.data = { pastaId: id };
    liteLinkToPasta.set(liteLink.id, id);
  }
}

function findPortIndex(ports: number[] | undefined, id: number): number {
  return ports ? ports.indexOf(id) : -1;
}

function findInputSlotIndex(node: any, portID: number, linkID: number): number {
  if (!node.inputs) return -1;
  const exact = node.inputs.findIndex((input: { pastaPortId?: number; pastaLinkId?: number }) => (
    input.pastaPortId === portID && input.pastaLinkId === linkID
  ));
  if (exact >= 0) return exact;
  return node.inputs.findIndex((input: { pastaPortId?: number }) => input.pastaPortId === portID);
}

function flushNodePositions(): void {
  if (rendering || !graph) return;
  const changes: Array<{ id: number; position: string }> = [];
  for (const node of graph._nodes as any[]) {
    const pastaNode = snapshot.nodes[String(node.id)];
    if (!pastaNode) continue;
    const next = formatPosition(node.pos);
    if (next !== pastaNode.position) {
      changes.push({ id: node.id, position: next });
    }
  }
  for (const change of changes) {
    tryBackend("setNodePosition", change);
  }
}

function currentSelectedNodeIDs(): number[] {
  return selectedNodeIDs(canvas?.selected_nodes).filter((id) => graph?.getNodeById(id)?.is_selected);
}

function updateSidekick(): void {
  const ids = currentSelectedNodeIDs();
  if (ids.length === 1) renderNodePanel(ids[0]);
  else renderClassPanel(ids.length);
}

function scheduleSidekickUpdate(): void {
  window.clearTimeout(sidekickUpdate);
  sidekickUpdate = window.setTimeout(updateSidekick, 0);
}

function renderClassPanel(selectedCount: number): void {
  destroyMenu();
  el.sidekick.dataset.nodeId = "";
  el.sidekick.dataset.nodeClass = "";
  const classes = Object.values(snapshot.classes).sort((a, b) => a.class.localeCompare(b.class));
  el.sidekick.innerHTML = `
    <div class="panel-title">${selectedCount > 1 ? `${selectedCount} nodes selected` : "Create node"}</div>
    <div class="class-list"></div>
  `;
  const list = el.sidekick.querySelector<HTMLElement>(".class-list")!;
  for (const cls of classes) {
    const primaryColor = typeColor(cls.primary_type);
    const button = document.createElement("button");
    button.className = "class-button";
    button.classList.toggle("class-button-primary", Boolean(primaryColor));
    button.type = "button";
    if (primaryColor) {
      button.style.setProperty("--class-primary-color", primaryColor.color);
      button.style.setProperty("--class-primary-text-color", primaryColor.textColor);
    }
    button.innerHTML = `<strong>${escapeHTML(shortClass(cls.class))}</strong><span>${escapeHTML(cls.short_description)}</span>`;
    button.addEventListener("click", () => {
      const pos = canvas?.convertOffsetToCanvas ? canvas.convertOffsetToCanvas([240, 180]) : [160, 120];
      tryBackend("addNode", { class: cls.class, position: formatPosition(pos) });
    });
    list.append(button);
  }
}

function renderNodePanel(id: number): void {
  const node = snapshot.nodes[String(id)];
  if (!node) return;
  if (
    el.sidekick.dataset.nodeId === String(id) &&
    el.sidekick.dataset.nodeClass === node.class &&
    selectedMenu?.node === id &&
    document.getElementById("node-menu")
  ) {
    return;
  }
  const description = tryBackend<string>("classDescription", { class: node.class }) ?? "";
  destroyMenu();
  el.sidekick.dataset.nodeId = String(id);
  el.sidekick.dataset.nodeClass = node.class;
  el.sidekick.innerHTML = `
    <div class="node-panel-tabs">
      <button class="active" data-node-tab="menu" type="button">Node menu</button>
      <button data-node-tab="docs" type="button">Description</button>
    </div>
    <div class="node-panel-section" data-node-panel="menu"><div id="node-menu"></div></div>
    <div class="node-panel-section hidden" data-node-panel="docs">
      <h2>${escapeHTML(shortClass(node.class))}</h2>
      <p>${escapeHTML(description || "No long description is registered for this node class.")}</p>
    </div>
  `;
  for (const tab of el.sidekick.querySelectorAll<HTMLButtonElement>("[data-node-tab]")) {
    tab.addEventListener("click", () => setNodePanelTab(tab.dataset.nodeTab ?? "menu"));
  }
  mountMenu(id);
}

function setNodePanelTab(name: string): void {
  for (const tab of el.sidekick.querySelectorAll<HTMLButtonElement>("[data-node-tab]")) {
    tab.classList.toggle("active", tab.dataset.nodeTab === name);
  }
  for (const panel of el.sidekick.querySelectorAll<HTMLElement>("[data-node-panel]")) {
    panel.classList.toggle("hidden", panel.dataset.nodePanel !== name);
  }
}

function mountMenu(nodeID: number): void {
  if (selectedMenu?.node === nodeID && document.getElementById("node-menu")) return;
  destroyMenu();
  selectedMenu = {
    node: nodeID,
    menu: new FormularMenu("node-menu", nodeMenuID(nodeID), (message: unknown) => {
      tryBackend("formular", { id: nodeID, message });
    }),
  };
  tryBackend("subscribeNodeMenu", { id: nodeID });
}

function destroyMenu(): void {
  if (!selectedMenu) return;
  tryBackend("unsubscribeNodeMenu", { id: selectedMenu.node });
  selectedMenu.menu.destroy();
  selectedMenu = null;
}

function feedMenu(notification: WorkspaceNotification): void {
  if (!selectedMenu || notification.id !== selectedMenu.node || !notification.formular) return;
  selectedMenu.menu.feed(notification.formular);
}

function requestSnapshot(): void {
  const next = tryBackend<WorkspaceSnapshot>("snapshot");
  if (next) {
    snapshot = next;
    renderGraph();
  }
}

function renderLogs(): void {
  const rows = [...backendLogs, ...frontendLogs].slice(-260);
  el.logs.innerHTML = "";
  for (const row of rows.reverse()) {
    const item = document.createElement("li");
    item.className = `log-${row.level}`;
    item.innerHTML = `<time>${escapeHTML(row.at)}</time><b>${escapeHTML(row.source)}</b><span>${escapeHTML(row.text)}</span>`;
    el.logs.append(item);
  }
}

function setTab(name: string): void {
  activeTab = name;
  for (const tab of el.tabs) tab.classList.toggle("active", tab.dataset.tab === name);
  for (const panel of el.panels) panel.classList.toggle("hidden", panel.dataset.panel !== name);
  if (name === "logs") renderLogs();
  if (name === "graph") setTimeout(resizeGraphCanvas, 0);
}

function graphKeydown(event: KeyboardEvent): void {
  if (activeTab !== "graph" || isEditableTarget(event.target)) return;
  debugLog("graph keydown", {
    key: event.key,
    ctrl: event.ctrlKey,
    meta: event.metaKey,
    shift: event.shiftKey,
    target: event.target instanceof HTMLElement ? event.target.tagName : String(event.target),
    selected: currentSelectedNodeIDs(),
  });
  if (isGraphShortcut(event, "KeyZ", "z")) {
    consumeGraphShortcut(event);
    runHistory(event.shiftKey ? "redo" : "undo");
  } else if (isGraphShortcut(event, "KeyY", "y")) {
    consumeGraphShortcut(event);
    runHistory("redo");
  } else if (isGraphShortcut(event, "KeyC", "c")) {
    consumeGraphShortcut(event);
    copySelection();
  } else if (isGraphShortcut(event, "KeyV", "v")) {
    consumeGraphShortcut(event);
    void pasteSelection();
  } else if (event.key === "Backspace" || event.key === "Delete") {
    const ids = currentSelectedNodeIDs();
    if (!ids.length) return;
    consumeGraphShortcut(event);
    tryBackend("removeNodes", { ids });
  }
}

function isGraphShortcut(event: KeyboardEvent, code: string, key: string): boolean {
  return isKeyboardShortcut(event, code, key);
}

function consumeGraphShortcut(event: KeyboardEvent): void {
  event.preventDefault();
  event.stopImmediatePropagation();
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName.toLowerCase();
  return tag === "input" || tag === "textarea" || tag === "select" || target.isContentEditable;
}

function runHistory(method: "undo" | "redo"): void {
  tryBackend(method);
  requestSnapshot();
}

function copySelection(): void {
  const ids = currentSelectedNodeIDs();
  debugLog("copy selection requested", { ids });
  if (!ids.length) {
    debugLog("copy selection skipped: no selected backend nodes");
    return;
  }
  const data = tryBackend<string>("copy", { ids });
  debugLog("backend copy returned", {
    hasData: Boolean(data),
    length: data?.length ?? 0,
    validPastaClipboard: data ? isPastaClipboard(data) : false,
    preview: data ? data.slice(0, 160) : "",
  });
  if (!data) return;
  localClipboard = data;
  void navigator.clipboard?.writeText(data).catch((error) => {
    debugLog("system copy failed", { error: (error as Error).message });
    log("clipboard", "warn", `system copy failed, using local clipboard: ${(error as Error).message}`);
  });
}

async function pasteSelection(): Promise<void> {
  let data = localClipboard;
  debugLog("paste selection requested", {
    localLength: localClipboard.length,
    localValid: isPastaClipboard(localClipboard),
    localPreview: localClipboard.slice(0, 160),
  });
  try {
    const systemClipboard = await navigator.clipboard?.readText();
    debugLog("system clipboard read", {
      hasData: Boolean(systemClipboard),
      length: systemClipboard?.length ?? 0,
      validPastaClipboard: systemClipboard ? isPastaClipboard(systemClipboard) : false,
      preview: systemClipboard ? systemClipboard.slice(0, 160) : "",
    });
    if (systemClipboard && isPastaClipboard(systemClipboard)) {
      data = systemClipboard;
      localClipboard = systemClipboard;
    }
  } catch (error) {
    debugLog("system paste failed", { error: (error as Error).message });
    log("clipboard", "warn", `system paste failed, using local clipboard: ${(error as Error).message}`);
  }
  if (!isPastaClipboard(data)) {
    debugLog("paste skipped: no valid Pasta clipboard data");
    return;
  }
  const offsetData = offsetClipboardPositions(data, 40, 40);
  debugLog("calling backend paste", {
    dataLength: offsetData.length,
    preview: offsetData.slice(0, 160),
  });
  const pasted = tryBackend<number[]>("paste", { data: offsetData }) ?? [];
  debugLog("backend paste returned", {
    pasted,
    snapshotNodesBeforeRefresh: Object.keys(snapshot.nodes).length,
    snapshotLinksBeforeRefresh: Object.keys(snapshot.links).length,
  });
  requestSnapshot();
  debugLog("snapshot after paste refresh", {
    nodes: Object.keys(snapshot.nodes).length,
    links: Object.keys(snapshot.links).length,
  });
  if (pasted.length) {
    setTimeout(() => selectGraphNodes(pasted), 0);
  }
}

function isPastaClipboard(data: string): boolean {
  try {
    const payload = JSON.parse(data) as { version?: unknown; nodes?: unknown };
    return payload.version === 1 && Array.isArray(payload.nodes);
  } catch (error) {
    return false;
  }
}

function offsetClipboardPositions(data: string, dx: number, dy: number): string {
  try {
    const payload = JSON.parse(data) as { nodes?: Array<{ position?: string }> };
    if (!Array.isArray(payload.nodes)) return data;
    for (const node of payload.nodes) {
      if (!node.position) continue;
      const position = JSON.parse(node.position) as { x?: unknown; y?: unknown };
      if (typeof position.x !== "number" || typeof position.y !== "number") continue;
      node.position = formatPosition([position.x + dx, position.y + dy]);
    }
    return JSON.stringify(payload);
  } catch {
    return data;
  }
}

function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[ch]!);
}

function shortClass(name: string): string {
  return name.split("/").pop() ?? name;
}

function wireUI(): void {
  for (const tab of el.tabs) tab.addEventListener("click", () => setTab(tab.dataset.tab ?? "graph"));
  el.save.addEventListener("click", () => {
    flushNodePositions();
    const text = tryBackend<string>("saveConfig");
    if (text !== null) el.config.value = text;
  });
  el.reload.addEventListener("click", () => {
    tryBackend("reloadConfig", { config: el.config.value });
    requestSnapshot();
  });
  el.restartFrontend.addEventListener("click", restartFrontend);
  document.addEventListener("keydown", (event) => {
    graphKeydown(event);
  }, { capture: true });
}

async function waitForBackend(): Promise<void> {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    if (typeof window.pastaBackendCall === "function") return;
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  throw new Error("Go backend did not export pastaBackendCall");
}

function restartFrontend(): void {
  destroyMenu();
  snapshot = emptySnapshot();
  graph?.clear();
  requestSnapshot();
  log("frontend", "info", "frontend state restarted; backend preserved");
}

function installTestHooks(): void {
  window.__pastaDemo = {
    call: backend,
    snapshot: () => snapshot,
    selectNode: (id: number) => {
      const node = graph?.getNodeById(id);
      if (node) {
        canvas.selectNode(node);
        updateSidekick();
      }
    },
    selectNodes: (ids: number[]) => {
      selectGraphNodes(ids);
      updateSidekick();
    },
    moveNode: (id: number, x: number, y: number) => {
      const node = graph?.getNodeById(id);
      if (!node) return;
      node.pos = [x, y];
      graph.setDirtyCanvas(true, true);
      flushNodePositions();
    },
    canConnect: (from: number, to: number) => {
      const fromPort = snapshot.ports[String(from)];
      const toPort = snapshot.ports[String(to)];
      if (!fromPort || !toPort) return false;
      return LiteGraph.isValidConnection(liteGraphPortType(fromPort.types), liteGraphPortType(toPort.types));
    },
    nodeScreenPosition: (id: number) => {
      const node = graph?.getNodeById(id);
      if (!node || !canvas) return null;
      const rect = el.graphCanvas.getBoundingClientRect();
      return {
        x: rect.left + (node.pos[0] + node.size[0] * 0.5 + canvas.ds.offset[0]) * canvas.ds.scale,
        y: rect.top + (node.pos[1] + node.size[1] * 0.5 + canvas.ds.offset[1]) * canvas.ds.scale,
      };
    },
    portScreenPosition: (port: number) => {
      const portSnapshot = snapshot.ports[String(port)];
      if (!portSnapshot || !graph || !canvas) return null;
      const node = graph.getNodeById(portSnapshot.node);
      if (!node) return null;
      const nodeSnapshot = snapshot.nodes[String(portSnapshot.node)];
      const ids = portSnapshot.direction === "left" ? nodeSnapshot?.left_ports : nodeSnapshot?.right_ports;
      const index = ids?.indexOf(port) ?? -1;
      if (index < 0) return null;
      const pos = node.getConnectionPos(portSnapshot.direction === "left", index);
      const rect = el.graphCanvas.getBoundingClientRect();
      return {
        x: rect.left + (pos[0] + canvas.ds.offset[0]) * canvas.ds.scale,
        y: rect.top + (pos[1] + canvas.ds.offset[1]) * canvas.ds.scale,
      };
    },
    highlightedInputPort: () => canvas?._highlight_input_slot?.pastaPortId ?? null,
  };
}

window.pastaFrontendDispatch = (raw: string) => {
  const event = JSON.parse(raw) as BackendEvent;
  if (event.type === "notification") {
    if (event.payload.kind === "node_menu") {
      feedMenu(event.payload);
    } else {
      snapshot = applyNotification(snapshot, event.payload);
      renderGraph();
    }
  } else if (event.type === "log") {
    backendLogs.push(event.payload);
    if (backendLogs.length > 400) backendLogs.shift();
    renderLogs();
  }
};

async function boot(): Promise<void> {
  wireUI();
  initGraph();
  installTestHooks();
  el.status.textContent = "Starting Go WASM backend";
  const go = new Go();
  const response = await fetch("./public/pasta-demo.wasm");
  const wasm = await WebAssembly.instantiateStreaming(response, go.importObject);
  void go.run(wasm.instance).catch((error) => {
    el.status.textContent = `Backend stopped: ${(error as Error).message}`;
  });
  await waitForBackend();
  el.status.textContent = "Go WASM backend running";
  el.config.value = backend<string>("initialConfig");
  requestSnapshot();
  setTab("graph");
}

boot().catch((error) => {
  el.status.textContent = `Demo failed: ${(error as Error).message}`;
  log("frontend", "error", (error as Error).message);
});
