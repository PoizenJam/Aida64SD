// Smoke test: runs the real index_pi.js sensor picker (Sensor + Reading
// dropdowns) against a DOM stub and prints the option lists it renders.
// Fails only if rendering throws.
const fs = require("fs");
const vm = require("vm");

function makeEl(tag) {
  const el = {
    tagName: (tag || "div").toUpperCase(),
    style: {},
    dataset: {},
    attributes: { setNamedItem(a) { this[a.name] = a.value; }, getNamedItem(n) { return this[n]; } },
    children: [],
    options: [],
    selectedIndex: 0,
    value: "",
    text: "",
    textContent: "",
    selected: false,
    disabled: false,
    checked: false,
    listeners: {},
    _innerHTML: "",
    addEventListener(ev, fn) { (this.listeners[ev] = this.listeners[ev] || []).push(fn); },
    dispatchEvent(ev) { (this.listeners[ev.type] || []).forEach((f) => f(ev)); return true; },
    removeAttribute(n) { delete this.attributes[n]; if (n === "disabled") this.disabled = false; },
    setAttribute(n, v) { this.attributes[n] = v; },
    add(node) { this.children.push(node); if (node.tagName === "OPTION") this.options.push(node); if (node.tagName === "OPTGROUP") { this.options.push(node); node._parent = this; } return node; },
    remove(i) { this.children.splice(i, 1); },
    querySelector() { return null; },
    querySelectorAll() { return []; },
    set innerHTML(v) { this._innerHTML = v; this.children = []; this.options = []; },
    get innerHTML() { return this._innerHTML; },
    get parentElement() { return this._parent || null; },
    get offsetWidth() { return 100; },
    get min() { return 0; }, get max() { return 1; },
  };
  return el;
}

const els = {};
function idEl(id) { if (!els[id]) els[id] = makeEl("div"); els[id].id = id; return els[id]; }
// Pre-create the selects/rows the code queries by id.
const sensorSel = makeEl("select"); sensorSel.id = "sensorSelect"; sensorSel.disabled = true;
const readingSel = makeEl("select"); readingSel.id = "readingSelect"; readingSel.disabled = true;
const sourceRow = makeEl("div"); sourceRow.id = "sourceFilterRow";
const sourceHeading = makeEl("div"); sourceHeading.id = "sourceHeading";
const sourceFilter = makeEl("select"); sourceFilter.id = "sourceFilter";
const graphUnitContainer = makeEl("div"); graphUnitContainer.id = "graphUnitContainer";
els["#sensorSelect"] = sensorSel;
els["#readingSelect"] = readingSel;
els["#sourceFilterRow"] = sourceRow;
els["#sourceHeading"] = sourceHeading;
els["#sourceFilter"] = sourceFilter;
els["#graphUnitContainer"] = graphUnitContainer;

class FakeEvent { constructor(type) { this.type = type; } }

const document = {
  readyState: "complete",
  createAttribute(n) { return { name: n, value: "open" }; },
  createElement(tag) { return makeEl(tag); },
  addEventListener() {},
  querySelector(sel) {
    if (sel.startsWith("#")) return els[sel] || null;
    return null;
  },
  getElementById(id) { return els[id] || (els[id] = makeEl("div")); },
};

const window = { addEventListener() {}, location: { hostname: "127.0.0.1" } };
const navigator = { appVersion: "QtWebEngine" };

const sandbox = {
  console, document, window, navigator, Event: FakeEvent,
  location: { hostname: "127.0.0.1" },
  setTimeout: (fn) => 0, // suppress deferred change dispatch
  JSON, Array, Object, String, Number, Math, Date, Promise,
};
sandbox.globalThis = sandbox;
vm.createContext(sandbox);

function load(file) {
  const src = fs.readFileSync(file, "utf8");
  vm.runInContext(src, sandbox, { filename: file });
}

load(process.argv[2]); // pi_utils.js
load(process.argv[3]); // index_pi.js

// A realistic catalog the host sends: 3 sensors (AIDA64 categories), readings inside.
const reading = (id, label, unit, type, uid, name, cat) => ({
  id, label, prefix: unit, unit, type, sensorUid: uid, sensorName: name, category: cat, source: "",
});
const catalog = {
  sensors: [
    { uid: "temp", name: "Temperatures", category: "temp", source: "" },
    { uid: "fan", name: "Fan Speeds", category: "fan", source: "" },
    { uid: "pwr", name: "Power", category: "pwr", source: "" },
  ],
  readings: [
    reading("TCPU", "CPU Package", "C", "Temp", "temp", "Temperatures", "temp"),
    reading("TGPU1", "GPU1", "C", "Temp", "temp", "Temperatures", "temp"),
    reading("FCPU", "CPU Fan", "RPM", "Fan", "fan", "Fan Speeds", "fan"),
    reading("PCPUPKG", "CPU Package", "W", "Power", "pwr", "Power", "pwr"),
  ],
  sources: [{ id: "", name: "Local" }],
  connectionStatus: "Connected",
  pollInterval: 1000,
  sensorCount: 3,
  autoRefresh: true,
  detectedCadenceMs: 1000,
};

function optsOf(sel) { return sel.children.filter((c) => c.tagName === "OPTION" && c.value); }
function placeholderOf(sel) { return sel.children.find((c) => c.tagName === "OPTION" && !c.value); }

// --- Scenario 1: catalog push, no sensor chosen yet. ---
try {
  sandbox.currentCatalog = catalog;
  sandbox.currentSensors = catalog.sensors;
  sandbox.currentReadingsBySensor = sandbox.readingsBySensorMap(catalog.readings);
  sandbox.currentSources = catalog.sources;
  sandbox.currentSensorSettings = { sensorUid: "", readingId: "", min: 0, max: 0, format: "" };
  sandbox.applyStatusPayload(catalog);
  sandbox.renderSourceFilter();
  sandbox.populateSensorSelect(catalog.sensors);
  sandbox.renderReadingSelect(false);
  sandbox.syncReadingType();
} catch (e) {
  console.log("RENDER THREW:", e.message);
  process.exit(2);
}
console.log("S1 sensorOptions =", optsOf(sensorSel).map((o) => o.text + "[" + o.value + "]").join(" | "));
console.log("S1 sensorPlaceholder =", JSON.stringify(placeholderOf(sensorSel) && placeholderOf(sensorSel).text), " disabled =", sensorSel.disabled);
console.log("S1 readingOptions =", optsOf(readingSel).length, " placeholder =", JSON.stringify(placeholderOf(readingSel) && placeholderOf(readingSel).text));

// --- Scenario 2: user picks the "temp" sensor (host would reset reading; PI local-sync). ---
sandbox.currentSensorSettings = { sensorUid: "temp", readingId: "", isValid: false, min: 0, max: 0, format: "" };
sandbox.renderReadingSelect(false);
sandbox.syncReadingType();
console.log("S2 readingOptions =", optsOf(readingSel).map((o) => o.text + "[" + o.value + "]").join(" | "));
console.log("S2 readingPlaceholder =", JSON.stringify(placeholderOf(readingSel) && placeholderOf(readingSel).text));

// --- Scenario 3: user picks a reading; host commits; PI reflects selection. ---
sandbox.currentSensorSettings = { sensorUid: "temp", readingId: "TCPU", isValid: true, min: 0, max: 0, format: "" };
sandbox.renderReadingSelect(false);
const selReading = optsOf(readingSel).find((o) => o.selected) || placeholderOf(readingSel);
console.log("S3 selectedReading =", selReading ? (selReading.value || selReading.text) : "none");

// --- Scenario 4: host pushes per-sensor readings for the picked sensor. ---
// The payload carries one sensor's readings and REPLACES that group (a re-push
// must not duplicate). temp already has 2 from the catalog; pushing the same
// 2 again must leave the group at 2.
sandbox.currentSensorSettings = { sensorUid: "temp", readingId: "TGPU1", isValid: true, min: 0, max: 0, format: "" };
(function () {
  const rs = [
    reading("TCPU", "CPU Package", "C", "Temp", "temp", "Temperatures", "temp"),
    reading("TGPU1", "GPU1", "C", "Temp", "temp", "Temperatures", "temp"),
  ];
  const byUid = {};
  rs.forEach(function (r) {
    const uid = r.sensorUid;
    if (!byUid[uid]) byUid[uid] = { category: r.category, name: r.sensorName, source: r.source || "", readings: [] };
    byUid[uid].readings.push(r);
  });
  Object.keys(byUid).forEach(function (uid) {
    const existing = sandbox.currentReadingsBySensor[uid];
    if (existing) { existing.readings = byUid[uid].readings; existing.category = byUid[uid].category || existing.category; existing.name = byUid[uid].name || existing.name; }
    else { sandbox.currentReadingsBySensor[uid] = byUid[uid]; }
  });
  sandbox.renderReadingSelect(true);
  sandbox.syncReadingType();
})();
console.log("S4 selectedReading =", (optsOf(readingSel).find((o) => o.selected) || placeholderOf(readingSel)).value);
console.log("S4 readingCount =", optsOf(readingSel).length);

// --- Scenario 5: empty catalog (AIDA64 not loaded). ---
sandbox.currentCatalog = { sensors: [], readings: [], sources: [{ id: "", name: "Local" }] };
sandbox.currentSensors = [];
sandbox.currentReadingsBySensor = sandbox.readingsBySensorMap([]);
sandbox.currentSensorSettings = { sensorUid: "", readingId: "", isValid: false, min: 0, max: 0, format: "" };
try {
  sandbox.renderSourceFilter();
  sandbox.populateSensorSelect([]);
  sandbox.renderReadingSelect(false);
  sandbox.syncReadingType();
} catch (e) {
  console.log("EMPTY RENDER THREW:", e.message);
}
console.log("S5 sensorOptions =", optsOf(sensorSel).length, " placeholder =", JSON.stringify(placeholderOf(sensorSel) && placeholderOf(sensorSel).text));
console.log("S5 readingOptions =", optsOf(readingSel).length, " placeholder =", JSON.stringify(placeholderOf(readingSel) && placeholderOf(readingSel).text));
