import test from "node:test";
import assert from "node:assert/strict";
import {
  DEFAULT_LINK_COLOR,
  applyNotification,
  emptySnapshot,
  formatPosition,
  isKeyboardShortcut,
  linkColor,
  nodeMenuID,
  parsePosition,
  selectedNodeIDs,
  typeColor,
} from "../public/build/pasta_model.js";

test("applies graph notifications in frontend-visible order", () => {
  let snapshot = emptySnapshot();
  snapshot = applyNotification(snapshot, {
    kind: "workspace_snapshot",
    snapshot: {
      classes: {
        "pasta/IntConstant": {
          class: "pasta/IntConstant",
          short_description: "Integer constant",
          unique: false,
          primary_type: "pasta/int",
          initial_ports: [],
        },
      },
      nodes: {},
      ports: {},
      links: {},
    },
  });

  snapshot = applyNotification(snapshot, {
    kind: "node_added",
    id: 10,
    node: {
      class: "pasta/IntConstant",
      name: "A",
      primary_type: "pasta/int",
      label: "1",
      position: "{\"x\":10,\"y\":20}",
      placeholder: false,
      root: false,
      has_root_path: false,
      left_ports: [],
      right_ports: [11],
    },
  });
  snapshot = applyNotification(snapshot, {
    kind: "port_added",
    id: 11,
    port: { direction: "right", node: 10, name: "output", types: ["pasta/int"], links: [] },
  });
  snapshot = applyNotification(snapshot, {
    kind: "node_updated",
    id: 10,
    node: { ...snapshot.nodes["10"], label: "42" },
  });

  assert.equal(snapshot.nodes["10"].label, "42");
  assert.equal(snapshot.ports["11"].name, "output");
  assert.equal(snapshot.classes["pasta/IntConstant"].primary_type, "pasta/int");
});

test("tracks complex link add and removal without mutating previous snapshots", () => {
  const base = {
    ...emptySnapshot(),
    nodes: {
      "1": {
        class: "pasta/IntConstant",
        name: "A",
        primary_type: "pasta/int",
        label: "2",
        position: "",
        placeholder: false,
        root: false,
        has_root_path: false,
        left_ports: [],
        right_ports: [2],
      },
      "3": {
        class: "pasta/Sum",
        name: "Sum",
        primary_type: "pasta/int",
        label: "2",
        position: "",
        placeholder: false,
        root: false,
        has_root_path: false,
        left_ports: [4],
        right_ports: [5],
      },
    },
  };
  const linked = applyNotification(base, {
    kind: "link_added",
    id: 6,
    link: {
      type: "pasta/int",
      placeholder: false,
      left_port: 4,
      left_port_node: 3,
      right_port: 2,
      right_port_node: 1,
    },
  });
  const removed = applyNotification(linked, { kind: "link_removed", id: 6 });

  assert.equal(base.links["6"], undefined);
  assert.equal(linked.links["6"].right_port_node, 1);
  assert.equal(removed.links["6"], undefined);
});

test("formats opaque Pasta node positions consistently", () => {
  assert.deepEqual(parsePosition("{\"x\":12.2,\"y\":40.8}", 0, 0), [12.2, 40.8]);
  assert.deepEqual(parsePosition("not json", 7, 9), [7, 9]);
  assert.equal(formatPosition([12.2, 40.8]), "{\"x\":12,\"y\":41}");
});

test("routes Formular menus by Pasta node id", () => {
  assert.equal(nodeMenuID(12), "NODE12MENU");
});

test("extracts selected LiteGraph node ids for copy, paste, and delete commands", () => {
  assert.deepEqual(selectedNodeIDs({ a: { id: 2 }, b: { id: 5 } }), [2, 5]);
  assert.deepEqual(selectedNodeIDs(null), []);
});

test("recognizes physical shortcut keys across keyboard layouts", () => {
  assert.equal(isKeyboardShortcut({ ctrlKey: true, metaKey: false, code: "KeyV", key: "м" }, "KeyV", "v"), true);
  assert.equal(isKeyboardShortcut({ ctrlKey: false, metaKey: true, code: "KeyC", key: "с" }, "KeyC", "c"), true);
  assert.equal(isKeyboardShortcut({ ctrlKey: true, metaKey: false, code: "", key: "v" }, "KeyV", "v"), true);
  assert.equal(isKeyboardShortcut({ ctrlKey: false, metaKey: false, code: "KeyV", key: "v" }, "KeyV", "v"), false);
});

test("maps standard Pasta types to colors and leaves unknown types on the default color", () => {
  assert.equal(typeColor("pasta/int")?.color, "#b37a00");
  assert.equal(typeColor("pasta/float")?.color, "#bd5a20");
  assert.equal(typeColor("pasta/string")?.color, "#087d73");
  assert.equal(typeColor("pasta/bool")?.color, "#3b7f3e");
  assert.equal(typeColor("any/any"), null);
  assert.equal(linkColor("example.com/custom"), DEFAULT_LINK_COLOR);
});
