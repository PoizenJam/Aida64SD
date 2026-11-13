// Settings PI command test: runs the real settings_pi.js against
// pi_utils.js and checks what it sends to Stream Deck for each control.
// Usage: node settings_pi_test.js <pi_utils.js> <settings_pi.js>
const fs = require("fs");
const vm = require("vm");

function makeEl(tag) {
  const el = {
    tagName: (tag || "div").toUpperCase(),
    style: {}, dataset: {}, classList: { add() {}, remove() {}, contains() { return false; } },
    children: [], options: [], selectedIndex: 0,
    value: "", textContent: "", innerHTML: "", checked: false, disabled: false,
    listeners: {},
    addEventListener(ev, fn) { (this.listeners[ev] = this.listeners[ev] || []).push(fn); },
    dispatch(ev) { (this.listeners[ev.type] || []).forEach((f) => f(ev)); },
    add(node) { this.children.push(node); this.options.push(node); return node; },
    appendChild(node) { this.children.push(node); return node; },
    setAttribute() {}, removeAttribute() {}, getAttribute() { return null; },
    querySelector() { return makeEl("div"); },
    querySelectorAll() { return []; },
    get length() { return this.options.length; },
    set length(n) { this.options.length = n; },
  };
  // select.value mirrors selectedIndex like a real <select>.
  if (el.tagName === "SELECT") {
    Object.defineProperty(el, "value", {
      get() { const o = this.options[this.selectedIndex]; return o ? o.value : ""; },
      set(v) { const i = this.options.findIndex((o) => o.value === String(v)); if (i >= 0) this.selectedIndex = i; },
    });
  }
  return el;
}

const els = {};
const poll = makeEl("select");
["auto", "250", "500", "1000", "2000", "5000", "10000"].forEach((v) => poll.add({ tagName: "OPTION", value: v, text: v }));
poll.value = "auto";
const auto = makeEl("input"); auto.checked = true; // HTML ships it checked
const iniKey = makeEl("select"); iniKey.add({ tagName: "OPTION", value: "", text: "(default)" });
const tileBg = makeEl("input"); tileBg.value = "#000000";
const tileText = makeEl("input"); tileText.value = "#ffffff";
const showLabel = makeEl("input"); showLabel.checked = true;
Object.assign(els, {
  tileBackground: tileBg, tileTextColor: tileText, showLabel: showLabel,
  pollInterval: poll, autoRefresh: auto, currentRate: makeEl("div"),
  connectionStatus: makeEl("div"), iniPath: makeEl("input"), iniRateKey: iniKey,
  refreshIniBtn: makeEl("button"),
});

const document = {
  readyState: "complete", activeElement: null, body: makeEl("body"), head: makeEl("head"),
  getElementById(id) { return els[id] || (els[id] = makeEl("div")); },
  querySelector() { return makeEl("div"); },
  querySelectorAll() { return []; },
  addEventListener() {},
  createElement(tag) { return makeEl(tag); },
  createTextNode() { return makeEl("text"); },
  createAttribute(n) { return { name: n, value: "" }; },
};

let socket = null;
function FakeWebSocket() { socket = this; this.sent = []; this.readyState = 1; }
FakeWebSocket.prototype.send = function (s) { this.sent.push(JSON.parse(s)); };
FakeWebSocket.OPEN = 1;

const sandbox = {
  console: { log() {}, warn() {}, error() {} },
  document, WebSocket: FakeWebSocket,
  location: { hostname: "127.0.0.1" },
  navigator: { appVersion: "QtWebEngine" },
  setTimeout: () => 0, clearTimeout() {}, requestAnimationFrame: () => 0,
  Option: function (text, value) { this.text = text; this.value = value; this.tagName = "OPTION"; },
  Event: function (t) { this.type = t; },
  JSON, Array, Object, String, Number, Math, Date, Promise, parseInt, parseFloat, isNaN,
};
sandbox.window = sandbox;
sandbox.addEventListener = function () {};
vm.createContext(sandbox);
for (const f of process.argv.slice(2, 4)) {
  vm.runInContext(fs.readFileSync(f, "utf8"), sandbox, { filename: f });
}

const info = JSON.stringify({ colors: { buttonPressedBackgroundColor: "#303030", buttonPressedBorderColor: "#646464", buttonPressedTextColor: "#969696", disabledColor: "#007AFF59", highlightColor: "#007AFF", mouseDownColor: "#2EA8FF" } });
const actionInfo = JSON.stringify({ action: "com.poizenjam.aida64sd.settings", context: "ctx", payload: { settings: {} } });
sandbox.connectElgatoStreamDeckSocket("1234", "pi-uuid", "registerPropertyInspector", info, actionInfo);
socket.onopen && socket.onopen();

function deliver(msg) { socket.onmessage({ data: JSON.stringify(msg) }); }

let failed = 0;
function assert(cond, msg) { process.stdout.write((cond ? "ok  : " : "FAIL: ") + msg + "\n"); if (!cond) failed++; }
function toPlugin() { return socket.sent.filter((m) => m.event === "sendToPlugin").map((m) => m.payload); }
function last(key) { const p = toPlugin().filter((x) => key in x); return p.length ? p[p.length - 1][key] : undefined; }

assert(socket.sent.some((m) => m.event === "registerPropertyInspector"), "registers with Stream Deck");
assert(last("property_inspector") === "propertyInspectorConnected", "announces propertyInspectorConnected (host connect path)");

auto.checked = false; auto.dispatch({ type: "change", target: auto });
assert(last("setPollInterval") !== undefined && last("setPollInterval") !== "auto", "unticking auto sends a numeric setPollInterval");
auto.checked = true; auto.dispatch({ type: "change", target: auto });
assert(last("setPollInterval") === "auto", "ticking auto sends setPollInterval auto");

poll.value = "2000"; poll.dispatch({ type: "change", target: poll });
assert(String(last("setPollInterval")) === "2000", "interval select sends setPollInterval 2000");

els.iniRateKey.add({ tagName: "OPTION", value: "HWMonUpdateFreqSensorPanel", text: "SensorPanel" });
els.iniRateKey.value = "HWMonUpdateFreqSensorPanel"; els.iniRateKey.dispatch({ type: "change", target: els.iniRateKey });
assert(last("setIniRateKey") === "HWMonUpdateFreqSensorPanel", "INI key select sends setIniRateKey");

els.refreshIniBtn.dispatch({ type: "click", target: els.refreshIniBtn, preventDefault() {}, stopPropagation() {} });
assert(last("refreshIniRate") !== undefined, "Refresh button sends refreshIniRate");

tileBg.value = "#202020"; tileBg.dispatch({ type: "change", target: tileBg });
const saved = socket.sent.filter((m) => m.event === "setSettings").pop();
assert(saved && saved.payload.tileBackground === "#202020" && saved.context === "pi-uuid", "appearance change saves setSettings with the PI uuid as context");
assert(saved && !("title" in saved.payload), "appearance save leaves the title fields to the host");
const appear = last("updateTileAppearance");
assert(appear && appear.tileBackground === "#202020", "appearance change sends updateTileAppearance for an immediate redraw");

const sentCount = socket.sent.length;
tileBg.dispatch({ type: "input", target: tileBg });
assert(socket.sent.length === sentCount, "an unchanged appearance is not re-sent");

assert(!socket.sent.some((m) => m.event === "setGlobalSettings"), "never writes global settings (the host owns them)");

process.stdout.write(failed ? failed + " FAILED\n" : "all passed\n");
process.exitCode = failed ? 1 : 0;
