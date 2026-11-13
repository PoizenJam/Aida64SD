var websocket = null,
  uuid = null,
  actionInfo = {},
  allSensors = [],
  allSources = [],
  currentSettings = {},
  globalThresholds = [];

var MAX_SLOTS = 8;
var onchangeevt = "onchange";

function updateRangeVal(id) {
  var inp = document.getElementById(id) || document.querySelector("#" + id + " input[type=range]");
  if (inp) positionRangeVal(inp);
}

function wireRangeVal(id) {
  var el = document.getElementById(id);
  var inp = (el && el.type === "range") ? el : (el && el.querySelector("input[type=range]")) || document.querySelector("#" + id + " input[type=range]");
  if (inp) inp.oninput = function() { positionRangeVal(this); };
}

function connectElgatoStreamDeckSocket(inPort, inUUID, inRegisterEvent, inInfo, inActionInfo) {
  openPISocket(inPort, inUUID, inRegisterEvent, inActionInfo, null, function (event, payload) {
    if (event !== "sendToPropertyInspector") return;
    if (handleErrorPayload(payload, true)) return;

    // Sensor sources (local + remote machines); must land before the
    // sensor list so the Source filter applies to this payload's sensors.
    if (Array.isArray(payload.sources)) {
      allSources = payload.sources;
      renderSourceFilter();
    }

    if (Array.isArray(payload.sensors)) {
      allSensors = payload.sensors;
      populateSlotSensorSelects();
    }

    // Global threshold library updates
    if (Array.isArray(payload.globalThresholds)) {
      globalThresholds = payload.globalThresholds;
      renderDerivedActiveGlobals();
    }

    // Full settings object; must come before populateReadingSelect so currentSettings is current
    if (payload.derivedSettings) {
      currentSettings = payload.derivedSettings;
      applySettingsToUI(currentSettings);
      renderDerivedActiveGlobals();
    }

    // Readings for a specific slot
    if (Array.isArray(payload.readings) && typeof payload.slotIndex === "number") {
      populateReadingSelect(payload.slotIndex, payload.readings);
    }
  });
}

// --- sensor / reading dropdowns ---

function populateSlotSensorSelects() {
  for (var i = 0; i < MAX_SLOTS; i++) {
    populateSensorSelect(i, allSensors);
  }
  populateAllSlotsSensorSelect(allSensors);
}

function applySourceFilterToSensors(sorted, currentUid) {
  var source = selectedSourceFilter();
  if (source === null) return sorted;
  return sorted.filter(function (sensor) {
    return (sensor.source || "") === source || sensor.uid === currentUid;
  });
}

function populateAllSlotsSensorSelect(sensors) {
  var el = byId("allSlots_sensorSelect");
  if (!el) return;
  var sorted = applySourceFilterToSensors(sensors.slice().sort(function (a, b) {
    return naturalCompare(a.name || "", b.name || "");
  }), "");

  while (el.options.length) el.remove(0);
  var ph = document.createElement("option");
  ph.value = "";
  ph.text = "Set all slots to…";
  ph.disabled = true;
  ph.selected = true;
  el.add(ph);

  sorted.forEach(function (sensor) {
    var opt = document.createElement("option");
    opt.text = sensor.name;
    opt.value = sensor.uid;
    el.add(opt);
  });
  el.removeAttribute("disabled");
}

// --- populate all UI fields from settings ---

function applySettingsToUI(s) {
  var formula = s.formula || "sum";
  var slotCount = s.slotCount || 2;
  setInputValue("titleFontSize", s.titleFontSize != null ? s.titleFontSize : 10.5);
  updateRangeVal("titleFontSize");
  setInputValue("valueFontSize", s.valueFontSize != null ? s.valueFontSize : 10.5);
  updateRangeVal("valueFontSize");
  setSelectValue("derived_formula", formula);
  setSelectValue("derived_slotCount", String(slotCount));
  updateSlotCountForFormula(formula);
  updateSlotVisibility(slotCount);

  setColorValue("derived_highlightColor", s.highlightColor);
  setColorValue("derived_foregroundColor", s.foregroundColor);
  setColorValue("derived_backgroundColor", s.backgroundColor);
  setColorValue("derived_valueTextColor", s.valueTextColor);
  setColorValue("derived_titleColor", s.titleColor);
  setInputValue("derived_graphHeightPct", s.graphHeightPct || 100);
  updateRangeVal("derived_graphHeightPct");
  setInputValue("derived_graphLineThickness", s.graphLineThickness || 1);
  updateRangeVal("derived_graphLineThickness");
  var tsDerived = byId("derived_textStroke");
  if (tsDerived) tsDerived.checked = s.textStroke === true;
  setColorValue("derived_textStrokeColor", s.textStrokeColor || "#000000");
  setSelectValue("derived_updateIntervalOverrideMs", String(s.updateIntervalOverrideMs || 0));
  var saInp = byId("derived_smoothingAlpha") && byId("derived_smoothingAlpha").querySelector("input[type=range]");
  if (saInp) { saInp.value = s.smoothingAlpha > 0 ? s.smoothingAlpha : 1; positionRangeVal(saInp); }
  setInputValue("derived_min", s.min != null ? s.min : "");
  setInputValue("derived_max", s.max != null ? s.max : "");
  var darEl = byId("derived_autoRange");
  if (darEl) darEl.checked = s.autoRange !== false;
  setInputValue("derived_format", s.format || "");
  setInputValue("derived_divisor", s.divisor || "");
  setSelectValue("derived_graphUnit", s.graphUnit || "");

  var slots = s.slots || [];
  for (var i = 0; i < MAX_SLOTS; i++) {
    var slot = slots[i] || {};
    setInputValue("slot" + i + "_divisor", slot.divisor || "");
    setSelectValue("slot" + i + "_graphUnit", slot.graphUnit || "");

    if (allSensors.length > 0) {
      setSelectValue("slot" + i + "_sensorSelect", slot.sensorUid || "");
    }
  }
}

// --- slot count locking ---

function updateSlotCountForFormula(formula) {
  var el = byId("derived_slotCount");
  if (!el) return;
  if (formula === "delta") {
    el.value = "2";
    el.style.pointerEvents = "none";
    el.style.opacity = "0.5";
    updateSlotVisibility(2);
    sendSdpi("derived_slotCount", "2");
  } else {
    el.style.pointerEvents = "";
    el.style.opacity = "";
  }
}

// --- slot visibility ---

// --- wire up all events after DOM ready ---

document.addEventListener("DOMContentLoaded", function () {
  // All-slots sensor select
  var allSlotsSel = byId("allSlots_sensorSelect");
  if (allSlotsSel) {
    allSlotsSel[onchangeevt] = function () {
      if (!allSlotsSel.value) return;
      sendSdpi("allSlots_sensorSelect", allSlotsSel.value);
      // Reset to placeholder after triggering
      allSlotsSel.selectedIndex = 0;
    };
  }

  // Tile-wide
  bindSdpiValue("titleFontSize", sendSdpi, onchangeevt);
  wireRangeVal("titleFontSize");
  bindSdpiValue("valueFontSize", sendSdpi, onchangeevt);
  wireRangeVal("valueFontSize");
  bindSdpiValue("derived_formula", sendSdpi, onchangeevt, function (val) {
    updateSlotCountForFormula(val);
  });
  bindSdpiValue("derived_slotCount", sendSdpi, onchangeevt, function (val) {
    updateSlotVisibility(parseInt(val, 10));
  });
  bindSdpiValue("derived_highlightColor", sendSdpi, onchangeevt);
  bindSdpiValue("derived_foregroundColor", sendSdpi, onchangeevt);
  bindSdpiValue("derived_backgroundColor", sendSdpi, onchangeevt);
  bindSdpiValue("derived_valueTextColor", sendSdpi, onchangeevt);
  bindSdpiValue("derived_titleColor", sendSdpi, onchangeevt);
  bindSdpiValue("derived_min", sendSdpi, "onchange");
  bindSdpiValue("derived_max", sendSdpi, "onchange");
  var derivedAutoRangeEl = byId("derived_autoRange");
  if (derivedAutoRangeEl) {
    derivedAutoRangeEl.onchange = function() { sendSdpiChecked("derived_autoRange", this.checked); };
  }
  bindSdpiValue("derived_format", sendSdpi, "onchange");
  bindSdpiValue("derived_divisor", sendSdpi, "onchange");
  bindSdpiValue("derived_graphUnit", sendSdpi, onchangeevt);
  bindSdpiValue("derived_graphHeightPct", sendSdpi, onchangeevt);
  wireRangeVal("derived_graphHeightPct");
  bindSdpiValue("derived_graphLineThickness", sendSdpi, onchangeevt);
  wireRangeVal("derived_graphLineThickness");
  var tsDerivedEl = byId("derived_textStroke");
  if (tsDerivedEl) tsDerivedEl.onchange = function() { sendSdpiChecked("derived_textStroke", this.checked); };
  bindSdpiValue("derived_textStrokeColor", sendSdpi, onchangeevt);
  bindSdpiValue("derived_updateIntervalOverrideMs", sendSdpi, onchangeevt);
  (function() {
    var inp = document.querySelector("#derived_smoothingAlpha input[type=range]");
    if (inp) {
      inp.oninput = function() { positionRangeVal(this); };
      inp.onchange = function() { sendSdpi("derived_smoothingAlpha", this.value); };
    }
  })();

  for (var i = 0; i < MAX_SLOTS; i++) {
    wireSensorSelect(i);
    wireReadingSelect(i);
    bindSdpiValue("slot" + i + "_divisor", sendSdpi, "onchange");
    bindSdpiValue("slot" + i + "_graphUnit", sendSdpi, onchangeevt);
  }
});

// --- active global thresholds for derived tile ---
// Derived tiles aggregate multiple sensor types, so only "all types" globals (readingType="") are shown.

function renderDerivedActiveGlobals() {
  var container = byId("derivedGlobalRefsContainer");
  if (!container) return;
  container.innerHTML = "";
  var active = (globalThresholds || []).filter(function(gt) { return !gt.readingType; });
  if (active.length === 0) return;
  var suppressed = Array.isArray(currentSettings.suppressedGlobalIDs) ? currentSettings.suppressedGlobalIDs : [];
  active.forEach(function(gt) {
    var isSuppressed = suppressed.indexOf(gt.id) !== -1;
    var row = document.createElement("div");
    row.className = "sdpi-item";
    var label = document.createElement("div");
    label.className = "sdpi-item-label";
    label.textContent = gt.name || gt.id;
    var valCell = document.createElement("div");
    valCell.className = "sdpi-item-value";
    valCell.style.cssText = "display:flex;align-items:center;gap:4px;";
    var span = document.createElement("span");
    span.style.color = "#888";
    span.style.fontSize = "9pt";
    span.textContent = (gt.operator || ">=") + " " + (gt.value != null ? gt.value : "");
    var btn = document.createElement("button");
    btn.style.cssText = "width:50px;padding:0;background:" + (isSuppressed ? "#a44" : "#4a4") + ";color:#fff;";
    btn.textContent = isSuppressed ? "off" : "on";
    (function(gtId) {
      btn.addEventListener("click", function() {
        var sups = Array.isArray(currentSettings.suppressedGlobalIDs) ? currentSettings.suppressedGlobalIDs.slice() : [];
        var idx = sups.indexOf(gtId);
        if (idx !== -1) {
          sups.splice(idx, 1);
          sendSdpi("derived_unsuppressGlobal", gtId);
        } else {
          sups.push(gtId);
          sendSdpi("derived_suppressGlobal", gtId);
        }
        currentSettings.suppressedGlobalIDs = sups;
        var nowSuppressed = sups.indexOf(gtId) !== -1;
        btn.style.background = nowSuppressed ? "#a44" : "#4a4";
        btn.textContent = nowSuppressed ? "off" : "on";
      });
    })(gt.id);
    valCell.appendChild(span);
    valCell.appendChild(btn);
    row.appendChild(label);
    row.appendChild(valCell);
    container.appendChild(row);
  });
}
