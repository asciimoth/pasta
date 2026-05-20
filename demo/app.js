/* global Go, LiteGraph, LGraph, LGraphCanvas */

const state = {
  graph: null,
  canvas: null,
  snapshot: null,
  syncing: false,
  selectedBackendId: null,
  backendToLG: new Map(),
  lgToBackend: new Map(),
  classRegistry: new Set(),
  pasteOffset: 30,
  flushingPositions: false,
  reactiveRefresh: false,
  reactiveQueued: false,
  deletingNodes: new Set(),
  pendingMenuUpdates: new Map(),
  menuDrafts: new Map(),
};

const typeStyles = {
  "calc.pasta.example.com/number": {
    name: "number",
    color: "#48b8ff",
    bg: "#162838",
    border: "#2f86c3",
    portOff: "#2f5d7c",
  },
  "strings.pasta.demo/string": {
    name: "string",
    color: "#f0b84f",
    bg: "#332717",
    border: "#b87d25",
    portOff: "#725225",
  },
  "stream.pasta.demo/stream": {
    name: "stream",
    color: "#5ee0a0",
    bg: "#173226",
    border: "#2b9a68",
    portOff: "#2d6248",
  },
  "network.pasta.demo/network": {
    name: "network",
    color: "#f06f6c",
    bg: "#331d22",
    border: "#b84f58",
    portOff: "#73333a",
  },
};

const nodeMetrics = {
  minWidth: 210,
  maxWidth: 420,
  minHeight: 106,
  portRowHeight: 20,
  titleFont: "14px Arial",
  bodyFont: "13px sans-serif",
};

let measureCtx = null;
const els = {};

window.addEventListener("load", async () => {
  Object.assign(els, {
    canvas: document.getElementById("graphCanvas"),
    palette: document.getElementById("palette"),
    menu: document.getElementById("menuPanel"),
    dump: document.getElementById("dumpText"),
    log: document.getElementById("log"),
    seed: document.getElementById("seedBtn"),
    clear: document.getElementById("clearBtn"),
    save: document.getElementById("saveBtn"),
    restore: document.getElementById("restoreBtn"),
    copy: document.getElementById("copyBtn"),
    paste: document.getElementById("pasteBtn"),
  });

  await startWASM();
  setupGraph();
  bindUI();
  startNotifications();
  await refresh(await call("seed"));
});

async function startWASM() {
  const go = new Go();
  const result = await WebAssembly.instantiateStreaming(fetch("app.wasm"), go.importObject);
  go.run(result.instance);
}

function setupGraph() {
  state.graph = new LGraph();
  state.canvas = new LGraphCanvas("#graphCanvas", state.graph);
  state.canvas.background_image = "";
  applyTypeStylesToCanvas(state.canvas);
  state.canvas.onNodeSelected = (node) => {
    state.selectedBackendId = node.backendId || null;
    renderMenu();
  };
  state.canvas.onNodeDeselected = () => {
    state.selectedBackendId = null;
    renderMenu();
  };
  els.canvas.addEventListener("pointerup", () => {
    flushNodePositions().catch((err) => flashError(err.message));
  });
  state.graph.start();
  resizeCanvas();
  window.addEventListener("resize", resizeCanvas);
}

function resizeCanvas() {
  const rect = els.canvas.parentElement.getBoundingClientRect();
  els.canvas.width = Math.max(320, Math.floor(rect.width));
  els.canvas.height = Math.max(320, Math.floor(rect.height));
  if (state.canvas) state.canvas.resize();
}

function bindUI() {
  els.seed.addEventListener("click", async () => refresh(await call("seed")));
  els.clear.addEventListener("click", async () => refresh(await call("clear")));
  els.save.addEventListener("click", async () => {
    const res = await call("save");
    els.dump.value = res.data || "";
    updateLogs(res.logs);
  });
  els.restore.addEventListener("click", async () => refresh(await call("restore", els.dump.value)));
  els.copy.addEventListener("click", copySelected);
  els.paste.addEventListener("click", pasteFromDump);

  document.addEventListener("keydown", async (event) => {
    const tag = event.target && event.target.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA") return;
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "c") {
      event.preventDefault();
      await copySelected();
    }
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "v") {
      event.preventDefault();
      await pasteFromDump();
    }
    if ((event.key === "Delete" || event.key === "Backspace") && state.selectedBackendId) {
      event.preventDefault();
      await deleteNodeById(state.selectedBackendId);
    }
  });
}

async function call(method, payload = "") {
  if (shouldFlushPositions(method)) await flushNodePositions();
  return rawCall(method, payload);
}

async function rawCall(method, payload = "") {
  const raw = typeof payload === "string" ? payload : JSON.stringify(payload);
  const res = JSON.parse(window.pastaDemoCall(method, raw));
  updateLogs(res.logs);
  if (!res.ok) {
    flashError(res.error);
    throw new Error(res.error);
  }
  return res;
}

function shouldFlushPositions(method) {
  return !state.syncing &&
    !state.flushingPositions &&
    state.snapshot &&
    state.graph &&
    !["snapshot", "moveNode", "createNode", "seed", "clear", "restore", "paste", "renameNode"].includes(method);
}

async function flushNodePositions() {
  if (state.syncing || state.flushingPositions || !state.snapshot || !state.graph) return;
  state.flushingPositions = true;
  try {
    const byID = new Map(state.snapshot.nodes.map((node) => [node.id, node]));
    for (const graphNode of state.graph._nodes || []) {
      if (!graphNode.backendId) continue;
      const snap = byID.get(graphNode.backendId);
      if (!snap) continue;
      const x = Number(graphNode.pos[0] || 0);
      const y = Number(graphNode.pos[1] || 0);
      const oldX = Number(snap.coordinate[0] || 0);
      const oldY = Number(snap.coordinate[1] || 0);
      if (Math.abs(x - oldX) < 0.5 && Math.abs(y - oldY) < 0.5) continue;
      const res = await rawCall("moveNode", { id: graphNode.backendId, x, y });
      updateLogs(res.logs);
      snap.coordinate = [x, y];
    }
  } finally {
    state.flushingPositions = false;
  }
}

async function refresh(res) {
  state.snapshot = normalizeSnapshot(res.data);
  registerClasses(state.snapshot.classes);
  renderPalette(state.snapshot.classes);
  syncGraph(state.snapshot);
  renderMenu();
  updateLogs(res.logs);
}

function startNotifications() {
  const res = JSON.parse(window.pastaDemoSubscribe(() => scheduleReactiveRefresh()));
  updateLogs(res.logs);
  if (!res.ok) flashError(res.error);
}

function scheduleReactiveRefresh() {
  if (state.reactiveQueued) return;
  state.reactiveQueued = true;
  window.queueMicrotask(() => {
    state.reactiveQueued = false;
    refreshFromNotification().catch(() => {
      // rawCall already reports the backend error in the log panel.
    });
  });
}

async function refreshFromNotification() {
  if (!state.snapshot) return;
  if (state.syncing || state.flushingPositions || state.reactiveRefresh) {
    window.setTimeout(scheduleReactiveRefresh, 50);
    return;
  }
  state.reactiveRefresh = true;
  try {
    const res = await rawCall("snapshot");
    applyReactiveSnapshot(res.data);
    updateLogs(res.logs);
  } finally {
    state.reactiveRefresh = false;
  }
}

function applyReactiveSnapshot(snapshot) {
  snapshot = normalizeSnapshot(snapshot);
  if (!sameTopology(state.snapshot, snapshot)) {
    state.snapshot = snapshot;
    registerClasses(snapshot.classes);
    renderPalette(snapshot.classes);
    syncGraph(snapshot);
    renderMenu();
    return;
  }
  state.snapshot = snapshot;
  for (const snap of snapshot.nodes) {
    const node = graphNode(snap.id);
    if (!node) continue;
    node.title = nodeTitle(snap);
    resizeGraphNode(node, snap);
  }
  patchMenu();
  state.graph.setDirtyCanvas(true, true);
}

function sameTopology(a, b) {
  a = normalizeSnapshot(a);
  b = normalizeSnapshot(b);
  if (!a || !b || a.nodes.length !== b.nodes.length || a.links.length !== b.links.length) return false;
  const nodeIDs = new Set(a.nodes.map((node) => node.id));
  for (const node of b.nodes) {
    if (!nodeIDs.has(node.id)) return false;
  }
  const linkIDs = new Set(a.links.map((link) => link.id));
  for (const link of b.links) {
    if (!linkIDs.has(link.id)) return false;
  }
  return true;
}

function registerClasses(classes) {
  for (const cls of classes) {
    const type = nodeType(cls);
    if (state.classRegistry.has(type)) continue;
    function PastaNode() {
      this.title = cls.displayName || cls.shortName;
      this.size = defaultNodeSize(cls);
      for (const input of cls.inputs) addPastaInput(this, input);
      for (const output of cls.outputs) addPastaOutput(this, output);
      this.properties = { backendId: "" };
    }
    PastaNode.title = cls.displayName || cls.shortName;
    const style = classTypeStyle(cls);
    if (style) {
      PastaNode.title_color = style.border;
      PastaNode.title_text_color = contrastTextColor(style.border);
    }
    PastaNode.prototype.onDrawForeground = function (ctx) {
      if (!this.backendId) return;
      const snap = findNode(this.backendId);
      if (!snap) return;
      resizeGraphNode(this, snap);
      ctx.fillStyle = "#dce5e8";
      ctx.font = "13px sans-serif";
      ctx.fillText(valueLabel(snap), 12, this.size[1] - 34);
      ctx.fillStyle = snap.keyAccess ? "#5ee0a0" : "#a9b1b8";
      ctx.fillText(nodeStatusLabel(snap), 12, this.size[1] - 16);
      drawNodeMessages(ctx, this, snap);
    };
    PastaNode.prototype.onConnectionsChange = async function (type, slot, connected, linkInfo) {
      if (state.syncing || !linkInfo) return;
      try {
        if (connected && type === LiteGraph.INPUT) {
          const inputNode = this.backendId;
          const outputNode = backendIdForLG(linkInfo.origin_id);
          const inputPort = this.inputs[slot].pastaPort;
          const outputPort = outputNode && portForSlot(outputNode, "output", linkInfo.origin_slot);
          if (inputNode && outputNode && inputPort && outputPort) {
            await refresh(await call("createLink", { inputNode, inputPort, outputNode, outputPort }));
          }
        } else if (!connected && type === LiteGraph.INPUT) {
          const backendLink = findLinkByLiteInfo(linkInfo);
          if (backendLink) await refresh(await call("deleteLink", { id: backendLink.id }));
        }
      } catch (_) {
        await refresh(await call("snapshot"));
      }
    };
    PastaNode.prototype.onBeforeConnectInput = function (slot) {
      const input = this.inputs && this.inputs[slot];
      if (!input || !input.pastaMultiple || input.link == null) return slot;
      return addVirtualInputSlot(this, input);
    };
    PastaNode.prototype.onRemoved = async function () {
      if (!state.syncing && this.backendId) {
        await deleteNodeById(this.backendId);
      }
    };
    PastaNode.prototype.getExtraMenuOptions = function () {
      const node = this;
      const options = [
        {
          content: "Copy selection",
          callback: () => copySelected(),
        },
        {
          content: "Paste",
          callback: () => pasteFromDump(),
        },
        null,
        {
          content: "Delete node",
          callback: async () => deleteNodeById(node.backendId),
        },
      ];
      const snap = findNode(node.backendId);
      if (snap && snap.class === "calc.pasta.example.com/Result") {
        options.unshift({
          content: "Pull result",
          callback: async () => refresh(await call("triggerMenuButton", { node: node.backendId, block: "main", button: "pull" })),
        });
      }
      return options;
    };
    LiteGraph.registerNodeType(type, PastaNode);
    state.classRegistry.add(type);
  }
}

function renderPalette(classes) {
  els.palette.replaceChildren();
  for (const cls of classes) {
    const style = classTypeStyle(cls);
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = cls.displayName || cls.shortName;
    button.title = cls.description || cls.name;
    if (style) {
      button.style.setProperty("--type-color", style.color);
      button.style.setProperty("--type-bg", style.bg);
      button.dataset.type = style.name;
    }
    if (cls.singleNode && hasNodeOfClass(cls.name)) {
      button.disabled = true;
      button.title = `${button.title} (already in workspace)`;
    }
    button.addEventListener("click", async () => {
      const x = 80 + (state.snapshot.nodes.length % 3) * 460;
      const y = 80 + Math.floor(state.snapshot.nodes.length / 3) * 220;
      try {
        await refresh(await call("createNode", { class: cls.name, x, y, value: 0 }));
      } catch (_) {
        await refresh(await call("snapshot"));
      }
    });
    els.palette.appendChild(button);
  }
}

async function deleteNodeById(id) {
  if (!id || state.deletingNodes.has(id)) return;
  state.deletingNodes.add(id);
  if (state.selectedBackendId === id) state.selectedBackendId = null;
  try {
    await refresh(await call("deleteNode", { id }));
  } catch (_) {
    await refresh(await call("snapshot"));
  } finally {
    state.deletingNodes.delete(id);
  }
}

function normalizeSnapshot(snapshot) {
  if (!snapshot) return snapshot;
  snapshot.classes = Array.isArray(snapshot.classes) ? snapshot.classes : [];
  snapshot.nodes = Array.isArray(snapshot.nodes) ? snapshot.nodes : [];
  snapshot.links = Array.isArray(snapshot.links) ? snapshot.links : [];
  return snapshot;
}

function syncGraph(snapshot) {
  snapshot = normalizeSnapshot(snapshot);
  state.syncing = true;
  state.graph.clear();
  state.backendToLG.clear();
  state.lgToBackend.clear();

  for (const snap of snapshot.nodes) {
    const node = LiteGraph.createNode(nodeType({ class: snap.class, shortName: snap.shortClass }));
    node.title = nodeTitle(snap);
    node.backendId = snap.id;
    node.properties.backendId = snap.id;
    node.pos = [snap.coordinate[0], snap.coordinate[1]];
    applyNodeTypeStyle(node, snap.primaryType);
    applySnapshotPortMetadata(node, snap);
    resizeGraphNode(node, snap);
    state.graph.add(node);
    state.backendToLG.set(snap.id, node.id);
    state.lgToBackend.set(node.id, snap.id);
  }

  for (const link of snapshot.links) {
    const output = graphNode(link.output.node);
    const input = graphNode(link.input.node);
    if (!output || !input) continue;
    const outSlot = slotForPort(output.outputs, link.output.port);
    const inSlot = inputSlotForLink(input, link.input.port);
    if (outSlot >= 0 && inSlot >= 0) {
      output.connect(outSlot, input, inSlot);
      applyLinkTypeStyle(output, input, outSlot, inSlot, link);
    }
  }

  state.graph.setDirtyCanvas(true, true);
  state.syncing = false;
}

function renderMenu() {
  els.menu.replaceChildren();
  const node = state.selectedBackendId ? findNode(state.selectedBackendId) : null;
  els.menu.dataset.nodeId = node ? node.id : "";
  els.menu.dataset.menuKey = node && node.menu ? menuStructureKey(node.menu) : "";
  if (!node) {
    els.menu.textContent = "Select a node.";
    els.menu.className = "panelText";
    return;
  }
  els.menu.className = "";
  const title = document.createElement("div");
  title.className = "status";
  title.dataset.role = "menu-title";
  title.textContent = `${node.shortClass} ${node.id}`;
  els.menu.appendChild(title);

  els.menu.appendChild(renderNodeStatus(node));

  if (node.messages && node.messages.length) {
    els.menu.appendChild(renderNodeMessages(node.messages));
  }

  const nameWrap = document.createElement("div");
  nameWrap.className = "field";
  nameWrap.dataset.role = "name-field";
  const nameLabel = document.createElement("label");
  nameLabel.textContent = "Name";
  const nameInput = document.createElement("input");
  nameInput.type = "text";
  nameInput.dataset.role = "name-input";
  nameInput.value = node.displayName || node.shortClass;
  nameInput.addEventListener("change", async () => {
    await refresh(await call("renameNode", { id: node.id, name: nameInput.value }));
  });
  nameWrap.append(nameLabel, nameInput);
  els.menu.appendChild(nameWrap);

  if (!node.menu) {
    const empty = document.createElement("div");
    empty.className = "panelText";
    empty.textContent = "This node has no menu.";
    els.menu.appendChild(empty);
    return;
  }

  for (const block of node.menu.blocks || []) {
    const blockEl = document.createElement("div");
    blockEl.className = "menuBlock";
    blockEl.dataset.block = block.id;
    if (block.title) {
      const heading = document.createElement("div");
      heading.className = "menuBlockTitle";
      heading.textContent = block.title;
      blockEl.appendChild(heading);
    }
    for (const field of block.fields || []) {
      blockEl.appendChild(renderMenuField(node, block.id, field));
    }
    for (const repeat of block.repeats || []) {
      blockEl.appendChild(renderMenuRepeat(node, block.id, repeat));
    }
    for (const buttonSpec of block.buttons || []) {
      const button = document.createElement("button");
      button.type = "button";
      button.dataset.block = block.id;
      button.dataset.button = buttonSpec.id;
      button.disabled = !!buttonSpec.disabled;
      button.textContent = buttonSpec.label || buttonSpec.id;
      button.addEventListener("click", async () => {
        await waitForMenuUpdates(node.id);
        const current = findNode(node.id) || node;
        await refresh(await call("triggerMenuButton", { node: current.id, block: block.id, button: buttonSpec.id }));
      });
      blockEl.appendChild(button);
    }
    els.menu.appendChild(blockEl);
  }
  if (node.menu.committable) {
    const actions = document.createElement("div");
    actions.className = "menuActions";
    const apply = document.createElement("button");
    apply.type = "button";
    apply.dataset.role = "menu-apply";
    apply.textContent = "Apply";
    apply.disabled = !hasMenuDraft(node.id);
    apply.addEventListener("click", async () => applyMenuDraft(node.id));
    const cancel = document.createElement("button");
    cancel.type = "button";
    cancel.dataset.role = "menu-cancel";
    cancel.textContent = "Cancel";
    cancel.disabled = !hasMenuDraft(node.id);
    cancel.addEventListener("click", () => {
      state.menuDrafts.delete(node.id);
      renderMenu();
    });
    actions.append(apply, cancel);
    els.menu.appendChild(actions);
  }
}

function renderMenuField(node, blockId, field) {
  const wrap = document.createElement("div");
  wrap.className = "field";
  wrap.dataset.block = blockId;
  wrap.dataset.field = field.id;
  const label = document.createElement("label");
  label.textContent = field.label || field.id;
  wrap.appendChild(label);

  if (field.readOnly || field.kind === "read-only") {
    const value = document.createElement("div");
    value.className = "readonly";
    value.dataset.role = "field-value";
    value.textContent = formatAny(field.value);
    wrap.appendChild(value);
    return wrap;
  }

  const input = createMenuInput(field);
  input.dataset.role = "field-input";
  input.dataset.kind = field.kind;
  setMenuInputValue(input, menuFieldDisplayValue(node, blockId, field));
  input.addEventListener("change", async () => {
    const value = readMenuInputValue(input, field.kind);
    if (node.menu && node.menu.committable) {
      setMenuDraftValue(node.id, blockId, field.id, value);
      patchMenuActions(node.id);
      return;
    }
    await updateMenuField(node.id, blockId, field.id, value);
  });
  wrap.appendChild(input);
  return wrap;
}

function renderMenuRepeat(node, blockId, repeat) {
  const wrap = document.createElement("div");
  wrap.className = "menuRepeat";
  wrap.dataset.block = blockId;
  wrap.dataset.repeat = repeat.id;

  const header = document.createElement("div");
  header.className = "menuRepeatHeader";
  const title = document.createElement("div");
  title.className = "menuRepeatTitle";
  title.textContent = repeat.title || repeat.id;
  header.appendChild(title);

  const add = document.createElement("button");
  add.type = "button";
  add.className = "menuRepeatAdd";
  add.textContent = "Add";
  add.addEventListener("click", async () => {
    const items = cloneRepeatItems(menuRepeatDisplayItems(node, blockId, repeat));
    items.push(defaultRepeatItem(repeat, items.length + 1));
    await commitRepeatItems(node, blockId, repeat, items);
  });
  header.appendChild(add);
  wrap.appendChild(header);

  const items = menuRepeatDisplayItems(node, blockId, repeat);
  if (!items.length) {
    const empty = document.createElement("div");
    empty.className = "panelText";
    empty.textContent = "No items.";
    wrap.appendChild(empty);
  }
  for (const item of items) {
    wrap.appendChild(renderMenuRepeatItem(node, blockId, repeat, item));
  }
  return wrap;
}

function renderMenuRepeatItem(node, blockId, repeat, item) {
  const itemEl = document.createElement("div");
  itemEl.className = "menuRepeatItem";
  itemEl.dataset.item = item.id;

  const header = document.createElement("div");
  header.className = "menuRepeatItemHeader";
  const title = document.createElement("div");
  title.className = "menuRepeatItemTitle";
  title.textContent = item.title || item.id;
  header.appendChild(title);

  const remove = document.createElement("button");
  remove.type = "button";
  remove.className = "menuRepeatRemove";
  remove.textContent = "Remove";
  remove.addEventListener("click", async () => {
    const items = cloneRepeatItems(menuRepeatDisplayItems(node, blockId, repeat)).filter((candidate) => candidate.id !== item.id);
    await commitRepeatItems(node, blockId, repeat, items);
  });
  header.appendChild(remove);
  itemEl.appendChild(header);

  for (const field of item.fields || []) {
    itemEl.appendChild(renderMenuRepeatField(node, blockId, repeat, item, field));
  }
  return itemEl;
}

function renderMenuRepeatField(node, blockId, repeat, item, field) {
  const wrap = document.createElement("div");
  wrap.className = "field";
  wrap.dataset.block = blockId;
  wrap.dataset.repeat = repeat.id;
  wrap.dataset.item = item.id;
  wrap.dataset.field = field.id;
  const label = document.createElement("label");
  label.textContent = field.label || field.id;
  wrap.appendChild(label);

  if (field.readOnly || field.kind === "read-only") {
    const value = document.createElement("div");
    value.className = "readonly";
    value.dataset.role = "repeat-field-value";
    value.textContent = formatAny(field.value);
    wrap.appendChild(value);
    return wrap;
  }

  const input = createMenuInput(field);
  input.dataset.role = "repeat-field-input";
  input.dataset.kind = field.kind;
  setMenuInputValue(input, field.value);
  input.addEventListener("change", async () => {
    const value = readMenuInputValue(input, field.kind);
    const items = cloneRepeatItems(menuRepeatDisplayItems(node, blockId, repeat));
    const target = items.find((candidate) => candidate.id === item.id);
    if (!target) return;
    const targetField = (target.fields || []).find((candidate) => candidate.id === field.id);
    if (!targetField) return;
    targetField.value = value;
    await commitRepeatItems(node, blockId, repeat, items);
  });
  wrap.appendChild(input);
  return wrap;
}

function createMenuInput(field) {
  if (field.options && field.options.length) {
    const select = document.createElement("select");
    for (const optionSpec of field.options) {
      const option = document.createElement("option");
      option.value = JSON.stringify(optionSpec.value);
      option.textContent = optionSpec.label || formatAny(optionSpec.value);
      option.disabled = !!optionSpec.disabled;
      select.appendChild(option);
    }
    return select;
  }
  if (field.kind === "bool") {
    const input = document.createElement("input");
    input.type = "checkbox";
    return input;
  }
  const input = document.createElement("input");
  input.type = field.kind === "float64" || field.kind === "int64" ? "number" : "text";
  return input;
}

function menuFieldDisplayValue(node, blockId, field) {
  if (!node.menu || !node.menu.committable) return field.value;
  const draft = state.menuDrafts.get(node.id);
  const key = menuDraftKey(blockId, field.id);
  if (draft && draft.values.has(key)) return draft.values.get(key);
  return field.value;
}

function setMenuInputValue(input, value) {
  if (input.type === "checkbox") {
    input.checked = !!value;
    return;
  }
  const text = input.tagName === "SELECT" ? JSON.stringify(value) : String(value ?? "");
  if (input.value !== text) input.value = text;
}

function readMenuInputValue(input, kind) {
  if (input.type === "checkbox") return input.checked;
  if (input.tagName === "SELECT") {
    try {
      return JSON.parse(input.value);
    } catch (_) {
      return input.value;
    }
  }
  if (kind === "float64" || kind === "int64") return Number(input.value);
  return input.value;
}

function menuDraftKey(blockId, fieldId) {
  return `${blockId}\u0000${fieldId}`;
}

function menuRepeatDraftKey(blockId, repeatId) {
  return `${blockId}\u0000${repeatId}`;
}

function setMenuDraftValue(nodeId, blockId, fieldId, value) {
  let draft = state.menuDrafts.get(nodeId);
  if (!draft) {
    draft = newMenuDraft();
    state.menuDrafts.set(nodeId, draft);
  }
  draft.values.set(menuDraftKey(blockId, fieldId), value);
}

function setMenuDraftRepeat(nodeId, blockId, repeatId, items) {
  let draft = state.menuDrafts.get(nodeId);
  if (!draft) {
    draft = newMenuDraft();
    state.menuDrafts.set(nodeId, draft);
  }
  draft.repeats.set(menuRepeatDraftKey(blockId, repeatId), { block: blockId, repeat: repeatId, items: repeatItemsToState(items) });
}

function newMenuDraft() {
  return { values: new Map(), repeats: new Map() };
}

function hasMenuDraft(nodeId) {
  const draft = state.menuDrafts.get(nodeId);
  return !!draft && (draft.values.size > 0 || draft.repeats.size > 0);
}

async function applyMenuDraft(nodeId) {
  const draft = state.menuDrafts.get(nodeId);
  if (!hasMenuDraft(nodeId)) return;
  const fields = [];
  for (const [key, value] of draft.values) {
    const [block, field] = key.split("\u0000");
    fields.push({ block, field, value });
  }
  const repeats = Array.from(draft.repeats.values());
  await waitForMenuUpdates(nodeId);
  state.menuDrafts.delete(nodeId);
  try {
    await refresh(await call("updateMenuState", {
      node: nodeId,
      version: 0,
      fields,
      repeats,
    }));
    await refresh(await call("snapshot"));
  } catch (err) {
    state.menuDrafts.set(nodeId, draft);
    patchMenuActions(nodeId);
    throw err;
  }
}

function patchMenuActions(nodeId) {
  const disabled = !hasMenuDraft(nodeId);
  for (const action of els.menu.querySelectorAll('[data-role="menu-apply"], [data-role="menu-cancel"]')) {
    action.disabled = disabled;
  }
}

async function updateMenuField(nodeId, blockId, fieldId, value) {
  const previous = state.pendingMenuUpdates.get(nodeId) || Promise.resolve();
  const task = previous.catch(() => {}).then(async () => {
    await refresh(await call("updateMenuField", {
      node: nodeId,
      version: 0,
      block: blockId,
      field: fieldId,
      value,
    }));
  });
  state.pendingMenuUpdates.set(nodeId, task);
  try {
    await task;
  } finally {
    if (state.pendingMenuUpdates.get(nodeId) === task) {
      state.pendingMenuUpdates.delete(nodeId);
    }
  }
}

async function waitForMenuUpdates(nodeId) {
  const pending = state.pendingMenuUpdates.get(nodeId);
  if (pending) await pending;
}

function patchMenu() {
  const node = state.selectedBackendId ? findNode(state.selectedBackendId) : null;
  if (!node || els.menu.dataset.nodeId !== node.id) {
    renderMenu();
    return;
  }

  const title = els.menu.querySelector('[data-role="menu-title"]');
  if (!title) {
    renderMenu();
    return;
  }
  const titleText = `${node.shortClass} ${node.id}`;
  if (title.textContent !== titleText) title.textContent = titleText;

  patchNodeStatus(node);
  patchMessages(node.messages || []);

  const nameInput = els.menu.querySelector('[data-role="name-input"]');
  if (!nameInput) {
    renderMenu();
    return;
  }
  patchInputValue(nameInput, node.displayName || node.shortClass);

  if (!node.menu) {
    renderMenu();
    return;
  }
  if (els.menu.dataset.menuKey !== menuStructureKey(node.menu)) {
    renderMenu();
    return;
  }

  for (const block of node.menu.blocks || []) {
    if (!els.menu.querySelector(`.menuBlock[data-block="${cssEscape(block.id)}"]`)) {
      renderMenu();
      return;
    }
    for (const field of block.fields || []) {
      const wrap = els.menu.querySelector(`.field[data-block="${cssEscape(block.id)}"][data-field="${cssEscape(field.id)}"]:not([data-repeat])`);
      if (!wrap) {
        renderMenu();
        return;
      }
      const label = wrap.querySelector("label");
      const labelText = field.label || field.id;
      if (!label || label.textContent !== labelText) {
        renderMenu();
        return;
      }
      if (field.readOnly || field.kind === "read-only") {
        const value = wrap.querySelector('[data-role="field-value"]');
        if (!value) {
          renderMenu();
          return;
        }
        const text = formatAny(field.value);
        if (value.textContent !== text) value.textContent = text;
      } else {
        const input = wrap.querySelector('[data-role="field-input"]');
        if (!input) {
          renderMenu();
          return;
        }
        if (!menuInputMatchesField(input, field)) {
          renderMenu();
          return;
        }
        patchMenuInputValue(input, menuFieldDisplayValue(node, block.id, field));
      }
    }
    for (const repeat of block.repeats || []) {
      if (!patchMenuRepeat(node, block.id, repeat)) {
        renderMenu();
        return;
      }
    }
    for (const buttonSpec of block.buttons || []) {
      const button = els.menu.querySelector(`button[data-block="${cssEscape(block.id)}"][data-button="${cssEscape(buttonSpec.id)}"]`);
      if (!button) {
        renderMenu();
        return;
      }
      button.disabled = !!buttonSpec.disabled;
      const text = buttonSpec.label || buttonSpec.id;
      if (button.textContent !== text) button.textContent = text;
    }
  }
  patchMenuActions(node.id);
}

function patchMenuRepeat(node, blockId, repeat) {
  const repeatEl = els.menu.querySelector(`.menuRepeat[data-block="${cssEscape(blockId)}"][data-repeat="${cssEscape(repeat.id)}"]`);
  if (!repeatEl) return false;
  const title = repeatEl.querySelector(".menuRepeatTitle");
  const titleText = repeat.title || repeat.id;
  if (!title || title.textContent !== titleText) return false;

  const items = menuRepeatDisplayItems(node, blockId, repeat);
  const renderedItems = Array.from(repeatEl.querySelectorAll(":scope > .menuRepeatItem"));
  if (items.length !== renderedItems.length) return false;
  for (const item of items) {
    const itemEl = repeatEl.querySelector(`:scope > .menuRepeatItem[data-item="${cssEscape(item.id)}"]`);
    if (!itemEl) return false;
    const itemTitle = itemEl.querySelector(".menuRepeatItemTitle");
    const itemTitleText = item.title || item.id;
    if (!itemTitle || itemTitle.textContent !== itemTitleText) return false;

    for (const field of item.fields || []) {
      const wrap = itemEl.querySelector(`.field[data-field="${cssEscape(field.id)}"]`);
      if (!wrap) return false;
      const label = wrap.querySelector("label");
      const labelText = field.label || field.id;
      if (!label || label.textContent !== labelText) return false;
      if (field.readOnly || field.kind === "read-only") {
        const value = wrap.querySelector('[data-role="repeat-field-value"]');
        if (!value) return false;
        const text = formatAny(field.value);
        if (value.textContent !== text) value.textContent = text;
      } else {
        const input = wrap.querySelector('[data-role="repeat-field-input"]');
        if (!input || !menuInputMatchesField(input, field)) return false;
        patchMenuInputValue(input, field.value);
      }
    }
  }
  return true;
}

function menuStructureKey(menu) {
  return JSON.stringify({
    committable: !!menu.committable,
    blocks: (menu.blocks || []).map((block) => ({
      id: block.id,
      title: block.title || "",
      fields: (block.fields || []).map((field) => ({
        id: field.id,
        label: field.label || "",
        kind: field.kind,
        readOnly: !!field.readOnly,
        render: field.render || "",
        options: (field.options || []).map((option) => ({
          value: option.value,
          label: option.label || "",
          disabled: !!option.disabled,
        })),
      })),
      buttons: (block.buttons || []).map((button) => ({
        id: button.id,
        label: button.label || "",
      })),
      repeats: (block.repeats || []).map((repeat) => ({
        id: repeat.id,
        title: repeat.title || "",
        template: (repeat.template || []).map((field) => menuFieldStructure(field)),
        items: (repeat.items || []).map((item) => ({
          id: item.id,
          title: item.title || "",
          fields: (item.fields || []).map((field) => menuFieldStructure(field)),
        })),
      })),
    })),
  });
}

function menuFieldStructure(field) {
  return {
    id: field.id,
    label: field.label || "",
    kind: field.kind,
    readOnly: !!field.readOnly,
    render: field.render || "",
    options: (field.options || []).map((option) => ({
      value: option.value,
      label: option.label || "",
      disabled: !!option.disabled,
    })),
  };
}

function menuRepeatDisplayItems(node, blockId, repeat) {
  if (!node.menu || !node.menu.committable) return repeat.items || [];
  const draft = state.menuDrafts.get(node.id);
  const key = menuRepeatDraftKey(blockId, repeat.id);
  if (draft && draft.repeats && draft.repeats.has(key)) {
    return repeatStateToItems(repeat, draft.repeats.get(key).items || []);
  }
  return repeat.items || [];
}

async function commitRepeatItems(node, blockId, repeat, items) {
  if (node.menu && node.menu.committable) {
    setMenuDraftRepeat(node.id, blockId, repeat.id, items);
    renderMenu();
    return;
  }
  await updateMenuRepeat(node.id, blockId, repeat.id, items);
}

async function updateMenuRepeat(nodeId, blockId, repeatId, items) {
  const previous = state.pendingMenuUpdates.get(nodeId) || Promise.resolve();
  const task = previous.catch(() => {}).then(async () => {
    await refresh(await call("updateMenuState", {
      node: nodeId,
      version: 0,
      repeats: [{ block: blockId, repeat: repeatId, items: repeatItemsToState(items) }],
    }));
  });
  state.pendingMenuUpdates.set(nodeId, task);
  try {
    await task;
  } finally {
    if (state.pendingMenuUpdates.get(nodeId) === task) {
      state.pendingMenuUpdates.delete(nodeId);
    }
  }
}

function repeatItemsToState(items) {
  return (items || []).map((item) => {
    const fields = {};
    for (const field of item.fields || []) {
      if (field.readOnly || field.kind === "read-only") continue;
      fields[field.id] = field.value;
    }
    return {
      id: item.id,
      title: item.title || "",
      fields,
    };
  });
}

function repeatStateToItems(repeat, states) {
  const originalItems = new Map((repeat.items || []).map((item) => [item.id, item]));
  return (states || []).map((stateItem) => {
    const values = stateItem.fields || {};
    const originalFields = new Map(((originalItems.get(stateItem.id) || {}).fields || []).map((field) => [field.id, field]));
    return {
      id: stateItem.id,
      title: stateItem.title || stateItem.id,
      fields: (repeat.template || []).map((template) => {
        const original = originalFields.get(template.id);
        return {
          ...template,
          value: Object.prototype.hasOwnProperty.call(values, template.id) ? values[template.id] : original ? original.value : defaultMenuValue(template),
        };
      }),
    };
  });
}

function cloneRepeatItems(items) {
  return (items || []).map((item) => ({
    id: item.id,
    title: item.title || "",
    fields: (item.fields || []).map((field) => ({
      ...field,
      options: (field.options || []).map((option) => ({ ...option })),
      metadata: field.metadata ? { ...field.metadata } : undefined,
    })),
    metadata: item.metadata ? { ...item.metadata } : undefined,
  }));
}

function defaultRepeatItem(repeat, ordinal) {
  const id = uniqueRepeatItemId(repeat, ordinal);
  return {
    id,
    title: `Row ${ordinal}`,
    fields: (repeat.template || []).map((field) => ({
      ...field,
      value: defaultMenuValue(field),
    })),
  };
}

function uniqueRepeatItemId(repeat, ordinal) {
  const existing = new Set((repeat.items || []).map((item) => item.id));
  let id = `row${ordinal}`;
  while (existing.has(id)) {
    ordinal += 1;
    id = `row${ordinal}`;
  }
  return id;
}

function defaultMenuValue(field) {
  if (field.value !== undefined) return field.value;
  if (field.options && field.options.length) return field.options[0].value;
  if (field.kind === "bool") return false;
  if (field.kind === "float64" || field.kind === "int64") return 0;
  if (field.kind === "read-only") return "";
  return "";
}

function menuInputMatchesField(input, field) {
  if (field.options && field.options.length) return input.tagName === "SELECT";
  if (field.kind === "bool") return input.type === "checkbox";
  const type = field.kind === "float64" || field.kind === "int64" ? "number" : "text";
  return input.tagName === "INPUT" && input.type === type;
}

function patchMenuInputValue(input, value) {
  if (document.activeElement === input) return;
  setMenuInputValue(input, value);
}

function patchMessages(messages) {
  const nextKey = JSON.stringify(messages.map((message) => ({
    id: message.id,
    type: message.type || "note",
    text: message.text || "",
  })));
  const current = els.menu.querySelector(".nodeMessages");
  if (current && current.dataset.key === nextKey) return;
  if (current) current.remove();
  if (!messages.length) return;
  const title = els.menu.querySelector('[data-role="menu-title"]');
  const rendered = renderNodeMessages(messages);
  rendered.dataset.key = nextKey;
  if (title && title.nextSibling) {
    els.menu.insertBefore(rendered, title.nextSibling);
  } else {
    els.menu.appendChild(rendered);
  }
}

function renderNodeMessages(messages) {
  const wrap = document.createElement("div");
  wrap.className = "nodeMessages";
  wrap.dataset.key = JSON.stringify((messages || []).map((message) => ({
    id: message.id,
    type: message.type || "note",
    text: message.text || "",
  })));
  for (const message of messages || []) {
    const item = document.createElement("div");
    item.className = `nodeMessage ${message.type || "note"}`;
    item.textContent = message.text || "";
    wrap.appendChild(item);
  }
  return wrap;
}

function renderNodeStatus(node) {
  const status = document.createElement("div");
  status.className = "nodeStatus";
  status.dataset.role = "node-status";

  const keyAccess = document.createElement("div");
  keyAccess.className = `accessBadge ${node.keyAccess ? "on" : "off"}`;
  keyAccess.dataset.role = "key-access";
  keyAccess.textContent = keyAccessText(node);
  status.appendChild(keyAccess);

  if (node.state !== "active") {
    const nodeState = document.createElement("div");
    nodeState.className = "stateBadge";
    nodeState.dataset.role = "node-state";
    nodeState.textContent = `state: ${node.state}`;
    status.appendChild(nodeState);
  }

  return status;
}

function patchNodeStatus(node) {
  const status = els.menu.querySelector('[data-role="node-status"]');
  if (!status) {
    renderMenu();
    return;
  }
  const keyAccess = status.querySelector('[data-role="key-access"]');
  if (!keyAccess) {
    renderMenu();
    return;
  }
  keyAccess.className = `accessBadge ${node.keyAccess ? "on" : "off"}`;
  const text = keyAccessText(node);
  if (keyAccess.textContent !== text) keyAccess.textContent = text;

  const nodeState = status.querySelector('[data-role="node-state"]');
  if (node.state === "active") {
    if (nodeState) nodeState.remove();
    return;
  }
  if (!nodeState) {
    renderMenu();
    return;
  }
  const stateText = `state: ${node.state}`;
  if (nodeState.textContent !== stateText) nodeState.textContent = stateText;
}

function patchInputValue(input, value) {
  const text = String(value ?? "");
  if (document.activeElement === input) return;
  if (input.value !== text) input.value = text;
}

function cssEscape(value) {
  if (window.CSS && typeof window.CSS.escape === "function") return window.CSS.escape(String(value));
  return String(value).replace(/["\\]/g, "\\$&");
}

async function copySelected() {
  const ids = selectedBackendIds();
  if (!ids.length && state.selectedBackendId) ids.push(state.selectedBackendId);
  if (!ids.length) return;
  const res = await call("copy", ids);
  els.dump.value = res.data || "";
  if (navigator.clipboard) {
    navigator.clipboard.writeText(els.dump.value).catch(() => {});
  }
}

async function pasteFromDump() {
  const text = els.dump.value.trim();
  state.pasteOffset += 30;
  try {
    await refresh(await call("paste", { clipboard: text, dx: state.pasteOffset, dy: state.pasteOffset }));
  } catch (_) {
    await refresh(await call("snapshot"));
  }
}

function selectedBackendIds() {
  const out = [];
  const selected = state.canvas && state.canvas.selected_nodes ? state.canvas.selected_nodes : {};
  for (const key of Object.keys(selected)) {
    const backend = selected[key].backendId;
    if (backend) out.push(backend);
  }
  return out;
}

function findLinkByLiteInfo(info) {
  const backendID = backendLinkIDForLiteInfo(info);
  if (backendID && state.snapshot) {
    const link = state.snapshot.links.find((candidate) => candidate.id === backendID);
    if (link) return link;
  }

  const outputNode = backendIdForLG(info.origin_id);
  const inputNode = backendIdForLG(info.target_id);
  if (!outputNode || !inputNode || !state.snapshot) return null;
  const outputPort = portForSlot(outputNode, "output", info.origin_slot);
  const inputPort = portForSlot(inputNode, "input", info.target_slot);
  return state.snapshot.links.find((link) =>
    link.output.node === outputNode &&
    link.output.port === outputPort &&
    link.input.node === inputNode &&
    link.input.port === inputPort
  );
}

function backendLinkIDForLiteInfo(info) {
  if (!info) return "";
  if (info.backendId) return info.backendId;
  const graphLink = state.graph && info.id != null ? state.graph.links[info.id] : null;
  return graphLink && graphLink.backendId ? graphLink.backendId : "";
}

function graphNode(backendId) {
  const lgID = state.backendToLG.get(backendId);
  return lgID == null ? null : state.graph.getNodeById(lgID);
}

function backendIdForLG(lgID) {
  return state.lgToBackend.get(lgID) || null;
}

function findNode(backendId) {
  return state.snapshot && state.snapshot.nodes.find((node) => node.id === backendId);
}

function hasNodeOfClass(className) {
  return !!(state.snapshot && state.snapshot.nodes.some((node) => node.class === className));
}

function portForSlot(backendId, kind, slot) {
  const node = findNode(backendId);
  if (!node) return "";
  const ports = kind === "input" ? node.inputs : node.outputs;
  return ports[slot] ? ports[slot].id : "";
}

function slotForPort(slots, port) {
  return (slots || []).findIndex((slot) => slot.pastaPort === port);
}

function inputSlotForLink(node, port) {
  const slots = node.inputs || [];
  let fallback = -1;
  for (let i = 0; i < slots.length; i++) {
    const slot = slots[i];
    if (slot.pastaPort !== port) continue;
    if (fallback < 0) fallback = i;
    if (slot.link == null) return i;
  }
  if (fallback < 0 || !slots[fallback].pastaMultiple) return fallback;
  return addVirtualInputSlot(node, slots[fallback]);
}

function addPastaInput(node, port) {
  node.addInput(port.name, liteGraphType(port.fixedType), {
    pastaPort: port.id,
    pastaMultiple: !!port.multiple,
    pastaName: port.name,
    pastaType: liteGraphType(port.fixedType),
  });
}

function addPastaOutput(node, port) {
  node.addOutput(port.name, liteGraphType(port.fixedType), {
    pastaPort: port.id,
    pastaName: port.name,
    pastaType: liteGraphType(port.fixedType),
  });
}

function addVirtualInputSlot(node, source) {
  const input = node.addInput(source.pastaName || source.name, source.pastaType || source.type, {
    pastaPort: source.pastaPort,
    pastaMultiple: true,
    pastaName: source.pastaName || source.name,
    pastaType: source.pastaType || source.type,
    pastaVirtual: true,
  });
  return node.inputs.indexOf(input);
}

function applySnapshotPortMetadata(node, snap) {
  for (const port of snap.inputs || []) {
    const slots = (node.inputs || []).filter((slot) => slot.pastaPort === port.id);
    for (const slot of slots) {
      slot.pastaPort = port.id;
      slot.pastaMultiple = !!port.multiple;
      slot.pastaName = port.name;
      slot.pastaType = liteGraphType(port.fixedType);
    }
  }
  for (const port of snap.outputs || []) {
    const slots = (node.outputs || []).filter((slot) => slot.pastaPort === port.id);
    for (const slot of slots) {
      slot.pastaPort = port.id;
      slot.pastaName = port.name;
      slot.pastaType = liteGraphType(port.fixedType);
    }
  }
}

function nodeType(cls) {
  const name = cls.name || cls.class || cls.shortName;
  return `pasta/${name.replace(/[^A-Za-z0-9_/-]/g, "_")}`;
}

function liteGraphType(type) {
  return type || "any";
}

function classTypeStyle(cls) {
  const ports = [...(cls.outputs || []), ...(cls.inputs || [])];
  const fixedType = ports.find((port) => port.fixedType)?.fixedType;
  return typeStyle(fixedType);
}

function typeStyle(type) {
  return typeStyles[type] || null;
}

function applyTypeStylesToCanvas(canvas) {
  LGraphCanvas.link_type_colors = LGraphCanvas.link_type_colors || {};
  canvas.default_connection_color_byType = canvas.default_connection_color_byType || {};
  canvas.default_connection_color_byTypeOff = canvas.default_connection_color_byTypeOff || {};
  for (const [type, style] of Object.entries(typeStyles)) {
    LGraphCanvas.link_type_colors[type] = style.color;
    canvas.default_connection_color_byType[type] = style.color;
    canvas.default_connection_color_byTypeOff[type] = style.portOff;
  }
}

function applyNodeTypeStyle(node, primaryType) {
  const style = typeStyle(primaryType);
  if (!style) return;
  node.color = style.border;
  node.bgcolor = style.bg;
  node.boxcolor = style.color;
}

function defaultNodeSize(cls) {
  const rows = Math.max((cls.inputs || []).length, (cls.outputs || []).length, 1);
  return [
    nodeMetrics.minWidth,
    Math.max(nodeMetrics.minHeight, 64 + rows * nodeMetrics.portRowHeight),
  ];
}

function resizeGraphNode(graphNode, snap) {
  const next = nodeSizeForSnapshot(snap);
  if (Math.abs(graphNode.size[0] - next[0]) < 0.5 && Math.abs(graphNode.size[1] - next[1]) < 0.5) return;
  graphNode.size = next;
}

function nodeSizeForSnapshot(snap) {
  const ctx = textMeasureContext();
  const labels = [
    nodeTitle(snap),
    valueLabel(snap),
    nodeStatusLabel(snap),
    ...(snap.inputs || []).map((port) => port.name || port.id),
    ...(snap.outputs || []).map((port) => port.name || port.id),
  ];
  let width = nodeMetrics.minWidth;
  for (let i = 0; i < labels.length; i++) {
    ctx.font = i === 0 ? nodeMetrics.titleFont : nodeMetrics.bodyFont;
    width = Math.max(width, Math.ceil(ctx.measureText(String(labels[i])).width + 64));
  }
  const rows = Math.max((snap.inputs || []).length, (snap.outputs || []).length, 1);
  const height = Math.max(nodeMetrics.minHeight, 84 + rows * nodeMetrics.portRowHeight);
  return [Math.min(nodeMetrics.maxWidth, width), height];
}

function textMeasureContext() {
  if (!measureCtx) {
    const canvas = document.createElement("canvas");
    measureCtx = canvas.getContext("2d");
  }
  return measureCtx;
}

function contrastTextColor(hex) {
  const rgb = parseHexColor(hex);
  if (!rgb) return "#ffffff";
  const linear = rgb.map((part) => {
    const value = part / 255;
    return value <= 0.03928 ? value / 12.92 : Math.pow((value + 0.055) / 1.055, 2.4);
  });
  const luminance = 0.2126 * linear[0] + 0.7152 * linear[1] + 0.0722 * linear[2];
  const contrastWithDark = (luminance + 0.05) / 0.05;
  const contrastWithLight = 1.05 / (luminance + 0.05);
  return contrastWithDark >= contrastWithLight ? "#101315" : "#ffffff";
}

function parseHexColor(hex) {
  const text = String(hex || "").trim();
  const match = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(text);
  if (!match) return null;
  const value = match[1].length === 3
    ? match[1].split("").map((part) => part + part).join("")
    : match[1];
  return [
    Number.parseInt(value.slice(0, 2), 16),
    Number.parseInt(value.slice(2, 4), 16),
    Number.parseInt(value.slice(4, 6), 16),
  ];
}

function applyLinkTypeStyle(output, input, outSlot, inSlot, backendLink) {
  const graphLink = findGraphLink(output, input, outSlot, inSlot);
  const style = typeStyle(backendLink.type);
  if (!graphLink || !style) return;
  graphLink.backendId = backendLink.id;
  graphLink.color = backendLink.state === "active" ? style.color : style.portOff;
  graphLink.type = backendLink.type;
}

function findGraphLink(output, input, outSlot, inSlot) {
  const linkIDs = (output.outputs[outSlot] && output.outputs[outSlot].links) || [];
  for (const linkID of linkIDs) {
    const link = state.graph.links[linkID];
    if (!link) continue;
    if (link.origin_id === output.id && link.origin_slot === outSlot &&
        link.target_id === input.id && link.target_slot === inSlot) {
      return link;
    }
  }
  return null;
}

function nodeTitle(node) {
  const name = (node.displayName || node.shortClass || "").trim();
  return `${name || node.shortClass} ${node.id}`;
}

function nodeStatusLabel(node) {
  const parts = [keyAccessText(node)];
  if (node.state !== "active") parts.push(`state: ${node.state}`);
  return parts.join(" | ");
}

function keyAccessText(node) {
  return `HasKeyNodeAccess: ${node.keyAccess ? "yes" : "no"}`;
}

function drawNodeMessages(ctx, graphNode, snap) {
  const messages = snap.messages || [];
  if (!messages.length) return;
  const message = messages[messages.length - 1];
  const text = String(message.text || "");
  const compact = text.length > 42 ? `${text.slice(0, 41)}...` : text;
  const width = Math.min(230, Math.max(90, ctx.measureText(compact).width + 22));
  const height = 24;
  const x = 8;
  const y = -height - 8;
  ctx.fillStyle = messageColor(message.type);
  ctx.fillRect(x, y, width, height);
  ctx.fillStyle = "#101315";
  ctx.font = "12px sans-serif";
  ctx.fillText(compact, x + 10, y + 16);
  if (messages.length > 1) {
    ctx.fillStyle = "#eef2f4";
    ctx.fillText(`+${messages.length - 1}`, graphNode.size[0] - 30, 18);
  }
}

function messageColor(type) {
  if (type === "err") return "#ee7474";
  if (type === "warn") return "#e1aa4d";
  return "#78c7f0";
}

function updateLogs(logs) {
  if (!logs) return;
  els.log.textContent = logs.join("\n");
  els.log.scrollTop = els.log.scrollHeight;
}

function flashError(message) {
  const line = document.createElement("div");
  line.textContent = message;
  line.style.color = "#ff8b8b";
  els.log.appendChild(line);
  els.log.scrollTop = els.log.scrollHeight;
}

function formatNumber(value) {
  return Number(value || 0).toLocaleString(undefined, { maximumFractionDigits: 4 });
}

function formatAny(value) {
  return typeof value === "number" ? formatNumber(value) : String(value ?? "");
}

function valueLabel(node) {
  if (node.primaryType === "strings.pasta.demo/string") {
    const text = String(node.text ?? "");
    const compact = compactNodeText(text);
    return `text: ${compact}`;
  }
  if (node.primaryType === "stream.pasta.demo/stream") {
    const text = String(node.text ?? "");
    const compact = compactNodeText(text);
    return `stream: ${compact}`;
  }
  return `value: ${formatNumber(node.value)}`;
}

function compactNodeText(text) {
  return text.length > 56 ? `${text.slice(0, 55)}...` : text;
}
