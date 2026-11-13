// Runs the real Composite or Derived PI (pi_utils.js + slot_pi.js + its own
// script) against a DOM stub: sensor, reading and settings pushes must fill
// the slot dropdowns, and the Source filter must narrow every slot's list.
// Usage: node slot_pi_test.js composite|derived <pi_utils.js> <slot_pi.js> <pi.js>
const fs = require("fs");
const vm = require("vm");

const kind = process.argv[2];
const settingsKey = kind === "derived" ? "derivedSettings" : "compositeSettings";
let failures = 0;
function check(name, cond, detail) {
  console.log((cond ? "ok  : " : "FAIL: ") + kind + ": " + name + (detail ? " (" + detail + ")" : ""));
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
    remove(i) { const [node] = this.children.splice(i, 1); const k = this.options.indexOf(node); if (k >= 0) this.options.splice(k, 1); },
    set selectedIndex(i) { this._selectedIndex = i; if (this.options[i]) this.value = this.options[i].value; },
    get selectedIndex() { return this._selectedIndex || 0; },
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
for (let i = 0; i < 8; i++) { getEl("slot" + i + "_sensorSelect", "select"); getEl("slot" + i + "_readingSelect", "select"); }
["sourceFilter", "allSlots_sensorSelect"].forEach((id) => getEl(id, "select"));
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
for (const f of process.argv.slice(3)) {
  vm.runInContext(fs.readFileSync(f, "utf8"), sandbox, { filename: f });
}

const sensors = [
  { uid: "temp", name: "Temperatures", category: "temp", source: "" },
  { uid: "volt10", name: "Voltages 10", category: "volt", source: "" },
  { uid: "volt9", name: "Voltages 9", category: "volt", source: "" },
  { uid: "remote::temp", name: "Remote Temps", category: "temp", source: "remote" },
];
const readings = [
  { id: "TCPU", label: "CPU Package", unit: "°C", type: "Temp", sensorUid: "temp", category: "temp" },
  { id: "TGPU1", label: "GPU1", unit: "°C", type: "Temp", sensorUid: "temp", category: "temp" },
];
const settings = { slotCount: 2, slots: [{ sensorUid: "temp", readingId: "TCPU", isValid: true }, { sensorUid: "", readingId: "", isValid: false }] };
const deliver = (payload) => sandbox.__ws.onmessage({ data: JSON.stringify({ event: "sendToPropertyInspector", payload }) });

sandbox.connectElgatoStreamDeckSocket(1, "PIUUID", "registerPropertyInspector", "{}",
  JSON.stringify({ action: "com.poizenjam.aida64sd." + kind, context: "CTX" }));
sandbox.__ws.onopen();
check("announces itself to the host on open",
  sent.some((m) => m.payload && m.payload.property_inspector === "propertyInspectorConnected"));

let threw = null;
try {
  const first = { sensors, sources: [{ id: "", name: "Local" }] };
  first[settingsKey] = settings;
  deliver(first);
  const s = {};
  s[settingsKey] = settings;
  deliver(s);
  deliver({ readings, slotIndex: 0 });
} catch (e) {
  threw = e;
}
check("pushes do not throw", !threw, threw && threw.message);

const values = (id) => els[id].options.map((o) => o.value).filter(Boolean);
check("slot sensors in natural order", values("slot0_sensorSelect").join(",") === "remote::temp,temp,volt9,volt10",
  values("slot0_sensorSelect").join(","));
check("configured sensor is selected", els.slot0_sensorSelect.value === "temp", els.slot0_sensorSelect.value);
check("unconfigured slot shows the placeholder", els.slot1_sensorSelect.options[0].selected === true);
check("slot readings filled", values("slot0_readingSelect").join(",") === "TCPU,TGPU1", values("slot0_readingSelect").join(","));

const two = { sensors, sources: [{ id: "", name: "Local" }, { id: "remote", name: "Remote" }] };
deliver(two);
els.sourceFilter.value = "remote";
(els.sourceFilter.listeners.change || []).forEach((f) => f({ type: "change" }));
check("source filter narrows every slot", values("slot1_sensorSelect").join(",") === "remote::temp",
  values("slot1_sensorSelect").join(","));
check("configured sensor survives the filter", values("slot0_sensorSelect").indexOf("temp") !== -1);

if (failures) {
  console.log(failures + " failed");
  process.exit(1);
}
console.log("all passed");
