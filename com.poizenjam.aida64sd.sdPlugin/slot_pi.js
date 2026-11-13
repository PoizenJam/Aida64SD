// Shared by the Composite and Derived Property Inspectors, loaded after
// pi_utils.js. The PI script defines MAX_SLOTS, currentSettings, allSensors,
// allSources, onchangeevt and populateSlotSensorSelects().

function slotSettings(slotIdx) {
  return currentSettings.slots ? currentSettings.slots[slotIdx] : null;
}

// The Source filter preselects the source of the first configured slot.
function firstConfiguredSource() {
  var slots = currentSettings.slots || [];
  for (var i = 0; i < slots.length; i++) {
    if (slots[i] && slots[i].sensorUid) return sourceOfUid(slots[i].sensorUid);
  }
  return "";
}

function selectedSourceFilter() {
  return sourceFilterValue(allSources, "sourceFilter");
}

function renderSourceFilter() {
  renderSourceFilterSelect(allSources, "sourceFilter", firstConfiguredSource(), populateSlotSensorSelects);
}

function sendSdpi(key, value) {
  sendValueToPlugin({ key: key, value: String(value) }, "sdpi_collection");
}

function sendSdpiChecked(key, checked) {
  sendValueToPlugin({ key: key, value: "", checked: checked }, "sdpi_collection");
}

function wireSensorSelect(slotIdx) {
  bindValueChange("slot" + slotIdx + "_sensorSelect", onchangeevt, function (value) {
    sendSdpi("slot" + slotIdx + "_sensorSelect", value);
  });
}

function wireReadingSelect(slotIdx) {
  bindValueChange("slot" + slotIdx + "_readingSelect", onchangeevt, function (value) {
    sendSdpi("slot" + slotIdx + "_readingSelect", value);
  });
}

function updateSlotVisibility(count) {
  for (var i = 0; i < MAX_SLOTS; i++) {
    var sec = byId("slot-section-" + i);
    if (sec) sec.style.display = i < count ? "" : "none";
  }
}

function populateSensorSelect(slotIdx, sensors) {
  var slot = slotSettings(slotIdx);
  fillSensorSelect(byId("slot" + slotIdx + "_sensorSelect"), sensors, slot ? slot.sensorUid : "", selectedSourceFilter());
}

function populateReadingSelect(slotIdx, readings) {
  var el = byId("slot" + slotIdx + "_readingSelect");
  if (!el) return;
  var slot = slotSettings(slotIdx);
  var currentRid = slot ? String(slot.readingId) : "";

  el.innerHTML = "";
  var ph = document.createElement("option");
  ph.text = "Choose a reading";
  ph.value = "";
  ph.disabled = true;
  if (!slot || !slot.isValid) ph.selected = true;
  el.add(ph);

  readings.slice().sort(compareReadings).forEach(function (r) {
    var opt = document.createElement("option");
    opt.text = readingOptionLabel(r);
    opt.value = String(r.id);
    if (String(r.id) === currentRid) opt.selected = true;
    el.add(opt);
  });
  el.removeAttribute("disabled");
}
