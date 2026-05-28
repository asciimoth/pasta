import test from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { execFileSync } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const chromiumExecutable = process.env.CHROMIUM || findExecutable("chromium", "chromium-browser", "google-chrome");
const port = 5183;

test("browser demo boots, renders graph state, and sends Formular edits to WASM", async (t) => {
  const server = spawn("python3", ["-m", "http.server", String(port), "--bind", "127.0.0.1"], {
    cwd: fileURLToPath(new URL("../..", import.meta.url)),
    stdio: "ignore",
  });
  t.after(() => server.kill("SIGTERM"));
  await waitForHTTP(`http://127.0.0.1:${port}/index.html`);

  const profile = await mkdtemp(join(tmpdir(), "pasta-chromium-"));

  const context = await chromium.launchPersistentContext(profile, {
    executablePath: chromiumExecutable,
    headless: true,
    args: [
      "--disable-background-networking",
      "--disable-component-extensions-with-background-pages",
      "--disable-extensions",
      "--disable-sync",
      "--disable-features=WebAuthentication,WebUsb,WebHID,SmartCard",
      "--password-store=basic",
      "--use-mock-keychain",
      "--no-first-run",
      "--no-default-browser-check",
    ],
  });
  t.after(async () => {
    const browserProcess = context.browser()?.process?.();
    await Promise.race([
      context.close(),
      delay(3000).then(() => browserProcess?.kill("SIGKILL")),
    ]);
    await delay(100);
    await rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
  });

  const page = await context.newPage();
  const browserErrors = [];
  page.on("pageerror", (error) => browserErrors.push(error.message));
  await page.goto(`http://127.0.0.1:${port}/index.html`);

  const boot = await page.waitForFunction(() => {
    const api = window.__pastaDemo;
    if (!api) return null;
    const snapshot = api.snapshot();
    const nodes = Object.keys(snapshot.nodes).length;
    if (!nodes) return null;
    return {
      status: document.querySelector("#backend-status")?.textContent,
      nodes,
      links: Object.keys(snapshot.links).length,
      classes: Object.keys(snapshot.classes).length,
      sidekick: document.querySelector("#sidekick")?.textContent || "",
    };
  });
  const bootValue = await boot.jsonValue();

  assert.equal(bootValue.status, "Go WASM backend running");
  assert.equal(bootValue.nodes, 35);
  assert.equal(bootValue.links, 57);
  assert.ok(bootValue.classes >= 20);
  assert.match(bootValue.sidekick, /Create node/);

  const compatibility = await page.evaluate(() => {
    const api = window.__pastaDemo;
    const snapshot = api.snapshot();
    const nodeByName = Object.fromEntries(Object.entries(snapshot.nodes).map(([id, node]) => [node.name, { id: Number(id), node }]));
    const portByName = (nodeName, side, name) => {
      const ids = side === "left" ? nodeByName[nodeName].node.left_ports : nodeByName[nodeName].node.right_ports;
      return ids.find((id) => snapshot.ports[String(id)].name === name);
    };
    return {
      intToSum1: api.canConnect(portByName("A", "right", "output"), portByName("Sum", "left", "input 1")),
      intToProduct1: api.canConnect(portByName("A", "right", "output"), portByName("Product", "left", "input 1")),
      intToComparisonAny: api.canConnect(portByName("A", "right", "output"), portByName("IsProductLarge", "left", "input 1")),
      stringToUpper: api.canConnect(portByName("Greeting", "right", "output"), portByName("Upper", "left", "input 1")),
      stringToSum: api.canConnect(portByName("Greeting", "right", "output"), portByName("Sum", "left", "input 1")),
    };
  });
  assert.equal(compatibility.intToSum1, true);
  assert.equal(compatibility.intToProduct1, true);
  assert.equal(compatibility.intToComparisonAny, true);
  assert.equal(compatibility.stringToUpper, true);
  assert.equal(compatibility.stringToSum, false);

  const dragLink = await page.evaluate(() => {
    const api = window.__pastaDemo;
    const snapshot = api.snapshot();
    const nodeByName = Object.fromEntries(Object.entries(snapshot.nodes).map(([id, node]) => [node.name, { id: Number(id), node }]));
    const portByName = (nodeName, side, name) => {
      const ids = side === "left" ? nodeByName[nodeName].node.left_ports : nodeByName[nodeName].node.right_ports;
      return ids.find((id) => snapshot.ports[String(id)].name === name);
    };
    const output = portByName("A", "right", "output");
    const input = portByName("Sum", "left", "input 1");
    const existing = Object.entries(snapshot.links).find(([, link]) => link.left_port === input);
    if (existing) api.call("removeLink", { id: Number(existing[0]) });
    return {
      output,
      input,
      from: api.portScreenPosition(output),
      to: api.portScreenPosition(input),
    };
  });
  await page.waitForFunction((input) => {
    const snapshot = window.__pastaDemo.snapshot();
    return !Object.values(snapshot.links).some((link) => link.left_port === input);
  }, dragLink.input);
  await page.mouse.move(dragLink.from.x, dragLink.from.y);
  await page.mouse.down();
  await page.mouse.move(dragLink.to.x, dragLink.to.y, { steps: 12 });
  await page.waitForFunction((input) => window.__pastaDemo.highlightedInputPort() === input, dragLink.input);
  await page.mouse.up();
  await page.waitForFunction((input) => {
    const snapshot = window.__pastaDemo.snapshot();
    return Object.values(snapshot.links).some((link) => link.left_port === input);
  }, dragLink.input);

  const canvasMetrics = await page.evaluate(() => {
    const canvas = document.querySelector("#graph-canvas");
    const rect = canvas.getBoundingClientRect();
    return {
      left: Math.round(rect.left),
      bottom: Math.round(rect.bottom),
      cssWidth: Math.round(rect.width),
      cssHeight: Math.round(rect.height),
      bitmapWidth: canvas.width,
      bitmapHeight: canvas.height,
    };
  });
  assert.equal(canvasMetrics.bitmapWidth, canvasMetrics.cssWidth);
  assert.equal(canvasMetrics.bitmapHeight, canvasMetrics.cssHeight);

  const palettePoint = { x: canvasMetrics.left + 36, y: canvasMetrics.bottom - 36 };
  await page.mouse.click(palettePoint.x, palettePoint.y, { button: "right" });
  await page.waitForSelector(".litecontextmenu");
  await page.locator(".litecontextmenu .litemenu-entry", { hasText: "Add Node" }).last().click();
  await page.waitForFunction(() => (document.querySelector(".litecontextmenu:last-child")?.textContent || "").includes("IntConstant"));
  const addMenuText = await page.locator(".litecontextmenu").last().textContent();
  assert.match(addMenuText, /Loopback/);
  assert.doesNotMatch(addMenuText, /WebSocket|Texture|Audio/);
  await page.evaluate(() => document.querySelectorAll(".litecontextmenu").forEach((menu) => menu.remove()));

  await page.mouse.dblclick(palettePoint.x + 24, palettePoint.y - 24);
  await page.waitForSelector(".litesearchbox input");
  await page.fill(".litesearchbox input", "loop");
  await page.waitForFunction(() => (document.querySelector(".litesearchbox .helper")?.textContent || "").includes("Loopback"));
  const searchText = await page.locator(".litesearchbox .helper").textContent();
  assert.match(searchText, /Loopback/);
  assert.doesNotMatch(searchText, /websocket|texture|audio/i);
  await page.keyboard.press("Escape");

  await page.evaluate(() => {
    const api = window.__pastaDemo;
    const entry = Object.entries(api.snapshot().nodes).find(([, node]) => node.name === "A");
    api.selectNode(Number(entry[0]));
  });
  await page.waitForFunction(() => /Node menu/.test(document.querySelector("#sidekick")?.textContent || ""));
  await page.fill("#node-name-input", "Renamed A");
  await page.press("#node-name-input", "Enter");
  await page.waitForFunction(() => Object.values(window.__pastaDemo.snapshot().nodes).some((node) => node.name === "Renamed A"));
  await page.waitForFunction(() => document.querySelector("#node-name-input")?.value === "Renamed A");
  await page.fill("#node-name-input", "Sum");
  await page.press("#node-name-input", "Enter");
  await page.waitForFunction(() => document.querySelector("#node-name-input")?.value === "Renamed A");
  await page.fill("#node-name-input", "A");
  await page.press("#node-name-input", "Enter");
  await page.waitForFunction(() => document.querySelector("#node-name-input")?.value === "A");
  await page.waitForFunction(() => Object.values(window.__pastaDemo.snapshot().nodes).some((node) => node.name === "A"));
  await page.mouse.click(20, 120);
  await page.waitForFunction(() => /Create node/.test(document.querySelector("#sidekick")?.textContent || ""));

  const movedNodeID = await page.evaluate(() => {
    const api = window.__pastaDemo;
    const entry = Object.entries(api.snapshot().nodes).find(([, node]) => node.name === "A");
    const id = Number(entry[0]);
    api.moveNode(id, 140, 120);
    return id;
  });
  await page.waitForFunction(
    (id) => {
      const position = window.__pastaDemo.snapshot().nodes[String(id)]?.position;
      return position && position !== "{\"x\":60,\"y\":80}";
    },
    movedNodeID,
  );
  await page.keyboard.press(process.platform === "darwin" ? "Meta+Z" : "Control+Z");
  await page.waitForFunction(
    (id) => window.__pastaDemo.snapshot().nodes[String(id)]?.position === "{\"x\":60,\"y\":80}",
    movedNodeID,
  );
  await page.keyboard.press(process.platform === "darwin" ? "Meta+Shift+Z" : "Control+Shift+Z");
  await page.waitForFunction(
    (id) => window.__pastaDemo.snapshot().nodes[String(id)]?.position === "{\"x\":140,\"y\":120}",
    movedNodeID,
  );
  await page.click("#save-config");
  const savedConfig = await page.locator("#config-text").inputValue();
  assert.match(savedConfig, /"A":\s*\{[\s\S]*"Pos":\s*"\{\\?"x\\?":140,\\?"y\\?":120\}"/);

  const copiedIDs = await page.evaluate(() => {
    const api = window.__pastaDemo;
    const pairs = Object.entries(api.snapshot().nodes);
    return pairs.filter(([, node]) => node.name === "A" || node.name === "Sum").map(([id]) => Number(id));
  });
  await page.evaluate((ids) => window.__pastaDemo.selectNodes(ids), copiedIDs);
  await page.keyboard.press(process.platform === "darwin" ? "Meta+C" : "Control+C");
  await page.evaluate(() => navigator.clipboard?.writeText("not a pasta clipboard"));
  await page.keyboard.press(process.platform === "darwin" ? "Meta+V" : "Control+V");
  await page.waitForFunction(() => {
    const snapshot = window.__pastaDemo.snapshot();
    const pastedA = Object.values(snapshot.nodes).find((node) => node.name !== "A" && node.class === "pasta/IntConstant" && node.position === "{\"x\":180,\"y\":160}");
    const pastedSum = Object.values(snapshot.nodes).find((node) => node.name !== "Sum" && node.class === "pasta/Sum" && node.position === "{\"x\":360,\"y\":120}");
    return Object.keys(snapshot.nodes).length >= 29 && Object.keys(snapshot.links).length >= 51 && pastedA && pastedSum;
  });
  await page.click("#restart-frontend");
  await page.waitForFunction(() => {
    const snapshot = window.__pastaDemo.snapshot();
    const pastedA = Object.values(snapshot.nodes).find((node) => node.name !== "A" && node.class === "pasta/IntConstant" && node.position === "{\"x\":180,\"y\":160}");
    const pastedSum = Object.values(snapshot.nodes).find((node) => node.name !== "Sum" && node.class === "pasta/Sum" && node.position === "{\"x\":360,\"y\":120}");
    return Object.keys(snapshot.nodes).length >= 29 && Object.keys(snapshot.links).length >= 51 && pastedA && pastedSum;
  });

  const copiedNodeMenu = await page.evaluate(() => {
    const api = window.__pastaDemo;
    const entry = Object.entries(api.snapshot().nodes).find(([, node]) => (
      node.name !== "A" &&
      node.class === "pasta/IntConstant" &&
      node.position === "{\"x\":180,\"y\":160}"
    ));
    return api.nodeScreenPosition(Number(entry[0]));
  });
  await page.mouse.click(copiedNodeMenu.x, copiedNodeMenu.y, { button: "right" });
  await page.waitForSelector(".litecontextmenu");
  const menuText = await page.locator(".litecontextmenu").last().textContent();
  assert.doesNotMatch(menuText, /Clone/);
  await page.keyboard.press("Escape");

  await page.keyboard.press(process.platform === "darwin" ? "Meta+Z" : "Control+Z");
  await page.waitForFunction(() => Object.keys(window.__pastaDemo.snapshot().nodes).length === 35);
  await page.keyboard.press(process.platform === "darwin" ? "Meta+Shift+Z" : "Control+Shift+Z");
  await page.waitForFunction(() => Object.keys(window.__pastaDemo.snapshot().nodes).length >= 29);

  const replacedLink = await page.evaluate(() => {
    const api = window.__pastaDemo;
    const snapshot = api.snapshot();
    const nodeByName = Object.fromEntries(Object.entries(snapshot.nodes).map(([id, node]) => [node.name, { id: Number(id), node }]));
    const aOutput = nodeByName.A.node.right_ports[0];
    const sumInput2 = nodeByName.Sum.node.left_ports.find((portID) => snapshot.ports[String(portID)].name === "input 2");
    api.call("addLink", { from: aOutput, to: sumInput2 });
    const next = api.call("snapshot");
    const linkedToSumInput2 = Object.values(next.links).filter((link) => link.left_port === sumInput2);
    return {
      linkCount: Object.keys(next.links).length,
      linkedToSumInput2,
      aID: nodeByName.A.id,
    };
  });
  assert.equal(replacedLink.linkCount, 58);
  assert.equal(replacedLink.linkedToSumInput2.length, 1);
  assert.equal(replacedLink.linkedToSumInput2[0].right_port_node, replacedLink.aID);

  const label = await page.evaluate(() => {
    const api = window.__pastaDemo;
    const snapshot = api.snapshot();
    const entry = Object.entries(snapshot.nodes).find(([, node]) => node.name === "A");
    const id = Number(entry[0]);
    api.selectNode(id);
    api.call("formular", {
      id,
      message: {
        type: "field.update",
        menuId: `NODE${id}MENU`,
        menuGeneration: 1,
        field: { blockId: "state", fieldId: "value" },
        value: 42,
      },
    });
    return api.call("snapshot").nodes[String(id)].label;
  });
  assert.equal(label, "42");
  assert.deepEqual(browserErrors, []);
});

async function waitForHTTP(url) {
  for (let i = 0; i < 80; i += 1) {
    try {
      const response = await fetch(url, { method: "HEAD" });
      if (response.ok) return;
    } catch {
      // Server not ready yet.
    }
    await delay(50);
  }
  throw new Error(`server did not start: ${url}`);
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function findExecutable(...names) {
  for (const name of names) {
    try {
      return execFileSync("sh", ["-lc", `command -v ${name}`], { encoding: "utf8" }).trim();
    } catch {
      // Try the next common Chromium binary name.
    }
  }
  return "chromium";
}
