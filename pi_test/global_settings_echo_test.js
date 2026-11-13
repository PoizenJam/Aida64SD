// Auto-refresh revert regression test (PI side). Runs the REAL websocket onmessage
// handler of the Settings PI (settings_pi.js, which owns the refresh/status
// controls) against pi_utils.js
// and replays the exact message order Stream Deck delivers:
//
//   user action -> host sendStatusToPIs (correct state)
//               -> host persistGlobalSettings -> SDK echoes the stored blob to
//                  the PI as didReceiveGlobalSettings
//
// An earlier PI fed that echo blob to applyStatusPayload. The blob is
// not the status payload shape (no autoRefresh while auto is on, iniRateKey
// instead of iniRateKeySet), so the echo landed last and reverted the
// Connection block to manual + default key. Each repro is checked against
// both the old partial blob shape and the current always-complete shape.
//
// Usage: node global_settings_echo_test.js <pi_utils.js> <settings_pi.js>
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
Object.assign(els, {
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
function hostStatus(over) {
  deliver({
    event: "sendToPropertyInspector", action: "com.poizenjam.aida64sd.settings", context: "ctx",
    payload: Object.assign({
      connectionStatus: "Connected", pollInterval: 1000, sensorCount: 7,
      autoRefresh: true, iniPath: "C:\\AIDA64\\AIDA64.INI", iniPathSet: "",
      iniRateMs: 1000, iniRateAppliedMs: 1000, iniStatus: "ok",
      iniRateKey: "HWMonUpdateFreqExtApp", iniRateKeySet: "", iniNote: "",
      iniKeyOptions: ["HWMonUpdateFreqExtApp", "HWMonUpdateFreqSensorPanel", "HWMonUpdateFreqSensorPage"],
    }, over),
  });
}
function echo(blob) { deliver({ event: "didReceiveGlobalSettings", payload: { settings: blob } }); }
function fireAuto(checked) { auto.checked = checked; auto.dispatch({ type: "change", target: auto }); }
function pickKey(v) { iniKey.value = v; iniKey.dispatch({ type: "change", target: iniKey }); }

let failed = 0;
function assert(cond, msg) { console_log((cond ? "ok  : " : "FAIL: ") + msg); if (!cond) failed++; }
function console_log(s) { process.stdout.write(s + "\n"); }
const rate = els.currentRate;

for (const shape of ["partial blob", "complete blob"]) {
  const full = shape === "complete blob";
  // Start in auto, settled.
  hostStatus({});

  // Repro 1: toggle OFF then ON.
  fireAuto(false);
  hostStatus({ autoRefresh: false });
  echo(full ? { pollInterval: 1000, autoRefresh: false, iniPath: "", iniRateKey: "" } : { pollInterval: 1000, autoRefresh: false });
  assert(poll.value === "1000" && rate.textContent === "1000ms", shape + " / R1 off: dropdown 1s, readout 1000ms (got " + poll.value + ", " + rate.textContent + ")");

  fireAuto(true);
  hostStatus({ autoRefresh: true });
  echo(full ? { pollInterval: 1000, autoRefresh: true, iniPath: "", iniRateKey: "" } : { pollInterval: 1000 });
  assert(auto.checked === true, shape + " / R1 on: checkbox checked");
  assert(poll.value === "auto", shape + " / R1 on: dropdown 'auto' after the echo (got " + poll.value + ")");
  assert(rate.textContent === "auto (1000ms, HWMonUpdateFreqExtApp)", shape + " / R1 on: readout auto after the echo (got " + rate.textContent + ")");

  // Repro 3: pick a non-default INI key.
  pickKey("HWMonUpdateFreqSensorPanel");
  hostStatus({ iniRateKey: "HWMonUpdateFreqSensorPanel", iniRateKeySet: "HWMonUpdateFreqSensorPanel" });
  echo(full ? { pollInterval: 1000, autoRefresh: true, iniPath: "", iniRateKey: "HWMonUpdateFreqSensorPanel" }
            : { pollInterval: 1000, iniRateKey: "HWMonUpdateFreqSensorPanel" });
  assert(iniKey.value === "HWMonUpdateFreqSensorPanel", shape + " / R3: INI key sticks after the echo (got '" + iniKey.value + "')");
  assert(rate.textContent === "auto (1000ms, HWMonUpdateFreqSensorPanel)", shape + " / R3: readout follows the key (got " + rate.textContent + ")");

  // Reset the key for the next pass.
  pickKey("");
  hostStatus({});
}

// Version line: matching host + PI build, then a stale PI script.
if (typeof sandbox.PI_VERSION === "string") {
  const ver = els.pluginVersion || document.getElementById("pluginVersion");
  hostStatus({ hostVersion: sandbox.PI_VERSION });
  assert(ver.textContent === sandbox.PI_VERSION, "version line shows the host build (got " + ver.textContent + ")");
  hostStatus({ hostVersion: "0.0.0.0" });
  assert(ver.textContent.indexOf("mismatch") >= 0, "version line flags a host/PI mismatch (got " + ver.textContent + ")");
}

console_log(failed ? failed + " FAILED" : "all passed");
process.exitCode = failed ? 1 : 0;
