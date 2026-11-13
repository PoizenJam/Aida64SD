// DOM-stub run of the real refresh/status controls (pi_utils.js):
//  - re-enable auto stays on from BOTH the checkbox and the new "Auto" dropdown
//    option (a stale host push must not flip a user-touched checkbox back off);
//  - picking a number unchecks auto and pins the interval;
//  - the Current readout shows the CLAMPED rate when the INI value is outside
//    the 250ms-10s window, and the plain value when it is inside;
//  - the #pollInterval select carries an "Auto" option.
const fs = require("fs");
const vm = require("vm");

function makeEl(tag) {
  const el = {
    tagName: (tag || "div").toUpperCase(),
    style: {}, dataset: {},
    children: [], options: [], selectedIndex: 0,
    value: "", textContent: "", checked: false,
    listeners: {}, _innerHTML: "",
    addEventListener(ev, fn) { (this.listeners[ev] = this.listeners[ev] || []).push(fn); },
    dispatch(ev) { (this.listeners[ev.type] || []).forEach((f) => f(ev)); },
    add(node) { this.children.push(node); this.options.push(node); return node; },
    get length() { return this.options.length; },
    set length(n) { this.options.length = n; },
  };
  return el;
}
const els = {};
function idEl(id) { if (!els[id]) els[id] = makeEl("div"); els[id].id = id; return els[id]; }

// Pre-create the Connection block controls the code queries by id.
const poll = makeEl("select"); poll.id = "pollInterval";
poll.add({ tagName: "OPTION", value: "auto", text: "Auto (INI)" });
poll.add({ tagName: "OPTION", value: "250", text: "250ms" });
poll.add({ tagName: "OPTION", value: "500", text: "500ms" });
poll.add({ tagName: "OPTION", value: "1000", text: "1s" });
poll.add({ tagName: "OPTION", value: "2000", text: "2s" });
poll.add({ tagName: "OPTION", value: "5000", text: "5s" });
poll.add({ tagName: "OPTION", value: "10000", text: "10s" });
poll.value = "auto";
const auto = makeEl("input"); auto.id = "autoRefresh"; auto.checked = true;
const rate = makeEl("div"); rate.id = "currentRate";
const status = makeEl("div"); status.id = "connectionStatus";
const iniPath = makeEl("input"); iniPath.id = "iniPath";
const iniKey = makeEl("select"); iniKey.id = "iniRateKey";
iniKey.add({ tagName: "OPTION", value: "", text: "(default)" });
const refreshBtn = makeEl("button"); refreshBtn.id = "refreshIniBtn";
Object.assign(els, {
  pollInterval: poll, autoRefresh: auto, currentRate: rate,
  connectionStatus: status, iniPath: iniPath, iniRateKey: iniKey, refreshIniBtn: refreshBtn,
});

const document = {
  readyState: "complete", activeElement: null,
  getElementById(id) { return els[id] || (els[id] = makeEl("div")); },
  querySelector() { return null; },
  addEventListener() {},
  createElement(tag) { return makeEl(tag); },
};
const consoleLines = [];
const sandbox = {
  console: { log: (...a) => consoleLines.push(a.join(" ")) },
  document,
  Option: function (text, value) { this.text = text; this.value = value; this.tagName = "OPTION"; },
  JSON, Array, Object, String, Number, Math, Date,
};
sandbox.window = sandbox;
vm.createContext(sandbox);
vm.runInContext(fs.readFileSync(process.argv[2], "utf8"), sandbox, { filename: process.argv[2] });

// Spy on the host command channel.
const sent = [];
sandbox.bindStatusControls(function (value, key) { sent.push([value, key]); });

function fire(el, value) { el.value = value; el.dispatch({ type: "change", target: el }); }
function fireAuto(checked) { auto.checked = checked; auto.dispatch({ type: "change", target: auto }); }
function assert(cond, msg) { if (!cond) { console.log("FAIL:", msg); process.exitCode = 1; } else { console.log("ok  :", msg); } }

// S1: re-enable from the "Auto" dropdown option sends "auto" and checks the box.
sent.length = 0; fire(poll, "auto");
assert(sent.length === 1 && sent[0][0] === "auto" && sent[0][1] === "setPollInterval", "S1 dropdown 'Auto' sends setPollInterval 'auto'");
assert(auto.checked === true, "S1 dropdown 'Auto' checks the checkbox");

// S2: picking a number unchecks auto and pins the interval.
sent.length = 0; fire(poll, "2000");
assert(sent.length === 1 && sent[0][0] === 2000 && sent[0][1] === "setPollInterval", "S2 dropdown number sends numeric setPollInterval");
assert(auto.checked === false, "S2 dropdown number unchecks the checkbox");

// S3: re-enable from the checkbox sends "auto".
sent.length = 0; fireAuto(true);
assert(sent.length === 1 && sent[0][0] === "auto" && sent[0][1] === "setPollInterval", "S3 checkbox on sends setPollInterval 'auto'");

// S4 (the repro, PI side): a stale host push with autoRefresh:false must not
// flip a user-touched checkbox back off (the checkbox stays on while the host
// settles). The host-side command-authoritative fix is what keeps the readout
// from reverting; here we verify the user's checkbox is not clobbered.
auto.dataset.userTouched = "1";
auto.checked = true;
sandbox.applyStatusPayload({ autoRefresh: false, pollInterval: 2000 });
assert(auto.checked === true, "S4 user-touched checkbox stays on after a stale autoRefresh:false push");

// S5: the host's settled auto push syncs the dropdown to "auto" (select shows auto).
sandbox.applyStatusPayload({ autoRefresh: true, pollInterval: 1000 });
assert(poll.value === "auto", "S5 host auto push sets the dropdown to 'auto'");
assert(auto.checked === true, "S5 host auto push checks the checkbox");

// S6: clamped readout (INI 60s -> applied 10s).
sandbox.applyStatusPayload({ autoRefresh: true, iniStatus: "ok", iniRateMs: 60000, iniRateAppliedMs: 10000, iniRateKey: "HWMonUpdateFreqExtApp" });
assert(rate.textContent === "auto (10000ms, INI 60000ms)", "S6 clamped readout = 'auto (10000ms, INI 60000ms)' (got: " + rate.textContent + ")");

// S7: in-window readout is unchanged (INI 1000ms, key shown).
sandbox.applyStatusPayload({ autoRefresh: true, iniStatus: "ok", iniRateMs: 1000, iniRateAppliedMs: 1000, iniRateKey: "HWMonUpdateFreqExtApp" });
assert(rate.textContent === "auto (1000ms, HWMonUpdateFreqExtApp)", "S7 in-window readout = 'auto (1000ms, HWMonUpdateFreqExtApp)' (got: " + rate.textContent + ")");

// S8: manual mode shows the pinned interval, not auto.
sandbox.applyStatusPayload({ autoRefresh: false, pollInterval: 2000 });
assert(rate.textContent === "2000ms", "S8 manual readout = '2000ms' (got: " + rate.textContent + ")");

// S9: the "Auto" option is present in the select.
const hasAuto = poll.options.some((o) => o.value === "auto");
assert(hasAuto, "S9 #pollInterval carries an 'Auto' option");

// S10: the one-shot auto-refresh diagnostic is surfaced in the PI console.
consoleLines.length = 0;
sandbox.applyStatusPayload({ autoRefresh: true, autoDiagnose: "auto=true from=setPollInterval(auto)" });
assert(consoleLines.some((l) => l.indexOf("auto=true from=setPollInterval(auto)") >= 0), "S10 autoDiagnose line surfaced in PI console");

console.log("done");
