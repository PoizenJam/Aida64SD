// Runs the real dial_pi.js against a DOM stub: a catalog push must fill the
// page Sensor and Reading dropdowns, and the Source filter must narrow the
// sensor list. Guards the 1.0.0 bug where the sensor list threw a
// ReferenceError and stayed empty.
// Usage: node dial_pi_test.js <pi_utils.js> <dial_pi.js>
const fs = require("fs");
const vm = require("vm");

let failures = 0;
function check(name, cond, detail) {
  console.log((cond ? "ok  : " : "FAIL: ") + name + (detail ? " (" + detail + ")" : ""));
  if (!cond) failures++;
}

function makeEl(tag, id) {
  return {
    id: id || "",
    tagName: (tag || "div").toUpperCase(),
    style: {}, dataset: {}, classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } },
    children: [], options: [], listeners: {},
    value: "", text: "", textContent: "", disabled: false, selected: false, checked: false,
    addEventListener(ev, fn) { (this.listeners[ev] = this.listeners[ev] || []).push(fn); },
    dispatchEvent(ev) { (this.listeners[ev.type] || []).forEach((f) => f(ev)); return true; },
    appendChild(node) { return this.add(node); },
    add(node) {
      this.children.push(node);
      if (node.tagName === "OPTION") {
        this.options.push(node);
        if (node.selected || this.options.length === 1) this.value = node.value;
      }
      return node;
    },
    removeAttribute(n) { if (n === "disabled") this.disabled = false; },
    setAttribute() {},
    querySelector() { return null; },
    querySelectorAll() { return []; },
    set innerHTML(v) { this.children = []; this.options = []; this.value = ""; },
    get innerHTML() { return ""; },
  };
}

const els = {};
const getEl = (id, tag) => els[id] || (els[id] = makeEl(tag, id));
["pageSensorSelect", "pageReadingSelect", "pageSourceFilter"].forEach((id) => getEl(id, "select"));
getEl("sourceFilterRow");

const document = {
  createElement: (tag) => makeEl(tag),
  getElementById: (id) => getEl(id),
  querySelector: (sel) => (sel.charAt(0) === "#" ? getEl(sel.slice(1)) : null),
  querySelectorAll: () => [],
  addEventListener() {},
  activeElement: null,
};

const sent = [];
class FakeWebSocket {
  constructor() { this.readyState = 1; sandbox.__ws = this; }
  send(s) { sent.push(JSON.parse(s)); }
}

const sandbox = {
  console: { log() {}, warn() {}, error: console.error },
  document,
  window: { addEventListener() {} },
  location: { hostname: "127.0.0.1" },
  WebSocket: FakeWebSocket,
  Event: class { constructor(type) { this.type = type; } },
  setTimeout: () => 0,
  requestAnimationFrame: () => 0,
};
sandbox.globalThis = sandbox;
vm.createContext(sandbox);
for (const f of process.argv.slice(2)) {
  vm.runInContext(fs.readFileSync(f, "utf8"), sandbox, { filename: f });
}

const sensors = [
  { uid: "temp", name: "Temperatures", category: "temp", source: "" },
  { uid: "fan", name: "Fan Speeds", category: "fan", source: "" },
  { uid: "remote::temp", name: "Remote Temps", category: "temp", source: "remote" },
];
const readings = [
  { id: "TCPU", label: "CPU Package", unit: "°C", type: "Temp", sensorUid: "temp", category: "temp" },
  { id: "FCPU", label: "CPU Fan", unit: "RPM", type: "Fan", sensorUid: "fan", category: "fan" },
];
const deliver = (payload) => sandbox.__ws.onmessage({ data: JSON.stringify({ event: "sendToPropertyInspector", payload }) });

sandbox.connectElgatoStreamDeckSocket(1, "PIUUID", "registerPropertyInspector", "{}",
  JSON.stringify({ action: "com.poizenjam.aida64sd.dial", context: "CTX" }));
sandbox.__ws.onopen();
check("announces itself to the host on open",
  sent.some((m) => m.payload && m.payload.property_inspector === "propertyInspectorConnected"));

let threw = null;
try {
  deliver({ catalog: { sensors, readings, sources: [{ id: "", name: "Local" }] } });
  deliver({ dialSettings: { activeIndex: 0, pages: [{ sensorUid: "temp", readingId: "TCPU", isValid: true }] } });
  deliver({ catalog: { sensors, readings, sources: [{ id: "", name: "Local" }] } });
} catch (e) {
  threw = e;
}
check("catalog push does not throw", !threw, threw && threw.message);

const sensorValues = () => els.pageSensorSelect.options.map((o) => o.value).filter(Boolean);
check("sensor dropdown lists every sensor without a filter",
  sensorValues().length === 3, sensorValues().join(","));
check("reading dropdown lists the selected sensor's readings",
  els.pageReadingSelect.options.some((o) => o.value === "TCPU"),
  els.pageReadingSelect.options.map((o) => o.value).join(","));

// Two sources: the filter row appears and narrows the list (the configured
// sensor stays listed).
deliver({ catalog: { sensors, readings, sources: [{ id: "", name: "Local" }, { id: "remote", name: "Remote" }] } });
els.pageSourceFilter.value = "remote";
(els.pageSourceFilter.listeners.change || []).forEach((f) => f({ type: "change" }));
check("source filter narrows the sensor list",
  sensorValues().indexOf("fan") === -1 && sensorValues().indexOf("remote::temp") !== -1,
  sensorValues().join(","));

if (failures) {
  console.log(failures + " failed");
  process.exit(1);
}
console.log("all passed");
