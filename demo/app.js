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
};

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
      await refresh(await call("deleteNode", { id: state.selectedBackendId }));
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
  state.snapshot = res.data;
  registerClasses(state.snapshot.classes);
  renderPalette(state.snapshot.classes);
  syncGraph(state.snapshot);
  renderMenu();
  updateLogs(res.logs);
}

function registerClasses(classes) {
  for (const cls of classes) {
    const type = nodeType(cls);
    if (state.classRegistry.has(type)) continue;
    function PastaNode() {
      this.title = cls.displayName || cls.shortName;
      this.size = [210, cls.inputs.length && cls.outputs.length ? 104 : 86];
      for (const input of cls.inputs) this.addInput(input.name, liteGraphType(input.fixedType));
      for (const output of cls.outputs) this.addOutput(output.name, liteGraphType(output.fixedType));
      this.properties = { backendId: "" };
    }
    PastaNode.title = cls.displayName || cls.shortName;
    PastaNode.prototype.onDrawForeground = function (ctx) {
      if (!this.backendId) return;
      const snap = findNode(this.backendId);
      if (!snap) return;
      ctx.fillStyle = "#dce5e8";
      ctx.font = "13px sans-serif";
      ctx.fillText(valueLabel(snap), 12, this.size[1] - 16);
      if (snap.state !== "active") {
        ctx.fillStyle = "#d45b5b";
        ctx.fillText(snap.state, 130, this.size[1] - 16);
      }
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
    PastaNode.prototype.onRemoved = async function () {
      if (!state.syncing && this.backendId) {
        await refresh(await call("deleteNode", { id: this.backendId }));
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
          callback: async () => refresh(await call("deleteNode", { id: node.backendId })),
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
    button.addEventListener("click", async () => {
      const x = 80 + (state.snapshot.nodes.length % 3) * 230;
      const y = 80 + Math.floor(state.snapshot.nodes.length / 3) * 140;
      await refresh(await call("createNode", { class: cls.name, x, y, value: 0 }));
    });
    els.palette.appendChild(button);
  }
}

function syncGraph(snapshot) {
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
    for (let i = 0; i < snap.inputs.length; i++) node.inputs[i].pastaPort = snap.inputs[i].id;
    for (let i = 0; i < snap.outputs.length; i++) node.outputs[i].pastaPort = snap.outputs[i].id;
    state.graph.add(node);
    state.backendToLG.set(snap.id, node.id);
    state.lgToBackend.set(node.id, snap.id);
  }

  for (const link of snapshot.links) {
    const output = graphNode(link.output.node);
    const input = graphNode(link.input.node);
    if (!output || !input) continue;
    const outSlot = slotForPort(output.outputs, link.output.port);
    const inSlot = slotForPort(input.inputs, link.input.port);
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
  if (!node || !node.menu) {
    els.menu.textContent = node ? "This node has no menu." : "Select a node.";
    els.menu.className = "panelText";
    return;
  }
  els.menu.className = "";
  const title = document.createElement("div");
  title.className = "status";
  title.textContent = `${node.shortClass} ${node.id}`;
  els.menu.appendChild(title);

  if (node.messages && node.messages.length) {
    const messages = document.createElement("div");
    messages.className = "nodeMessages";
    for (const message of node.messages) {
      const item = document.createElement("div");
      item.className = `nodeMessage ${message.type || "note"}`;
      item.textContent = message.text || "";
      messages.appendChild(item);
    }
    els.menu.appendChild(messages);
  }

  const nameWrap = document.createElement("div");
  nameWrap.className = "field";
  const nameLabel = document.createElement("label");
  nameLabel.textContent = "Name";
  const nameInput = document.createElement("input");
  nameInput.type = "text";
  nameInput.value = node.displayName || node.shortClass;
  nameInput.addEventListener("change", async () => {
    await refresh(await call("renameNode", { id: node.id, name: nameInput.value }));
  });
  nameWrap.append(nameLabel, nameInput);
  els.menu.appendChild(nameWrap);

  for (const block of node.menu.blocks || []) {
    const blockEl = document.createElement("div");
    blockEl.className = "menuBlock";
    for (const field of block.fields || []) {
      const wrap = document.createElement("div");
      wrap.className = "field";
      const label = document.createElement("label");
      label.textContent = field.label || field.id;
      wrap.appendChild(label);
      if (field.readOnly || field.kind === "read-only") {
        const value = document.createElement("div");
        value.className = "readonly";
        value.textContent = formatAny(field.value);
        wrap.appendChild(value);
      } else {
        const input = document.createElement("input");
        input.type = field.kind === "float64" || field.kind === "int64" ? "number" : "text";
        input.value = field.value ?? "";
        input.addEventListener("change", async () => {
          const value = input.type === "number" ? Number(input.value) : input.value;
          await refresh(await call("updateMenuField", {
            node: node.id,
            version: node.menu.version,
            block: block.id,
            field: field.id,
            value,
          }));
        });
        wrap.appendChild(input);
      }
      blockEl.appendChild(wrap);
    }
    for (const buttonSpec of block.buttons || []) {
      const button = document.createElement("button");
      button.type = "button";
      button.disabled = !!buttonSpec.disabled;
      button.textContent = buttonSpec.label || buttonSpec.id;
      button.addEventListener("click", async () => {
        await refresh(await call("triggerMenuButton", { node: node.id, block: block.id, button: buttonSpec.id }));
      });
      blockEl.appendChild(button);
    }
    els.menu.appendChild(blockEl);
  }
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
  await refresh(await call("paste", { clipboard: text, dx: state.pasteOffset, dy: state.pasteOffset }));
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

function portForSlot(backendId, kind, slot) {
  const node = findNode(backendId);
  if (!node) return "";
  const ports = kind === "input" ? node.inputs : node.outputs;
  return ports[slot] ? ports[slot].id : "";
}

function slotForPort(slots, port) {
  return (slots || []).findIndex((slot) => slot.pastaPort === port);
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
    const compact = text.length > 18 ? `${text.slice(0, 17)}...` : text;
    return `text: ${compact}`;
  }
  return `value: ${formatNumber(node.value)}`;
}
