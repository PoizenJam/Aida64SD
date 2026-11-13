// Settings Property Inspector (Settings key). Holds the plugin-wide controls:
// refresh rate and AIDA64.INI, the Settings key's appearance, the global
// threshold library, and the AIDA64 connection status.
//
// State rules:
// - The host status push (sendToPropertyInspector with connectionStatus /
//   pollInterval) is the only input to the refresh/status UI, rendered by
//   pi_utils.js applyStatusPayload. didReceiveGlobalSettings is the raw SDK
//   store blob, not the status shape, and only its globalThresholds are read.
// - This PI never writes global settings; it sends commands and the host
//   persists the merged blob.
// - The key appearance is this key's own action settings: saved with
//   setSettings and applied at once with updateTileAppearance.
var websocket = null,
  uuid = null,
  actionInfo = {},
  appearanceSignature = null;
var globalThresholds = [];

function normalizeHex(value, fallback) {
  if (typeof value !== "string") return fallback;
  var v = value.trim().toLowerCase();
  return /^#[0-9a-f]{6}$/.test(v) ? v : fallback;
}

function readTileSettingsFromUI() {
  var bg = byId("tileBackground"), text = byId("tileTextColor"), label = byId("showLabel");
  if (!bg || !text || !label) {
    return { tileBackground: "#000000", tileTextColor: "#ffffff", showLabel: true };
  }
  return {
    tileBackground: normalizeHex(bg.value, "#000000"),
    tileTextColor: normalizeHex(text.value, "#ffffff"),
    showLabel: label.checked === true
  };
}

function tileSettingsSignature(s) {
  return [s.tileBackground, s.tileTextColor, s.showLabel ? "1" : "0"].join("|");
}

function applyTileSettingsToUI(settings) {
  var bgEl = byId("tileBackground"), textEl = byId("tileTextColor"), showEl = byId("showLabel");
  if (!bgEl || !textEl || !showEl) return;
  var bg = normalizeHex(settings.tileBackground, "#000000");
  var text = normalizeHex(settings.tileTextColor, "#ffffff");
  var show = settings.showLabel !== undefined ? settings.showLabel === true : true;
  if (bgEl.value !== bg) bgEl.value = bg;
  if (textEl.value !== text) textEl.value = text;
  if (showEl.checked !== show) showEl.checked = show;
  appearanceSignature = tileSettingsSignature(readTileSettingsFromUI());
}

// Persist the appearance as this key's action settings and ask the host to
// redraw now. Only the appearance fields are sent; the host keeps the title
// fields (they come from Stream Deck's title parameters).
function saveTileSettings() {
  var settings = readTileSettingsFromUI();
  var sig = tileSettingsSignature(settings);
  if (sig === appearanceSignature) return;
  appearanceSignature = sig;
  sendJson({ event: "setSettings", context: uuid, payload: settings });
  sendValueToPlugin(settings, "updateTileAppearance");
}

function bindAppearanceControls() {
  ["tileBackground", "tileTextColor", "showLabel"].forEach(function (id) {
    var el = byId(id);
    if (!el || el.dataset.bound) return;
    el.dataset.bound = "1";
    el.addEventListener("change", saveTileSettings);
    el.addEventListener("input", saveTileSettings);
  });
}

function connectElgatoStreamDeckSocket(inPort, inUUID, inRegisterEvent, inInfo, inActionInfo) {
  // The key's own settings arrive with the registration info.
  var info = parseJSONOrEmpty(inActionInfo);
  if (info.payload && info.payload.settings) {
    applyTileSettingsToUI(info.payload.settings);
  }
  // The host answers the connect with the status push and the threshold library.
  openPISocket(inPort, inUUID, inRegisterEvent, inActionInfo, function () {
    sendJson({ event: "getSettings", context: inUUID });
  }, function (event, payload) {
    if (event === "didReceiveGlobalSettings") {
      var gs = payload.settings || {};
      if (Array.isArray(gs.globalThresholds)) {
        globalThresholds = gs.globalThresholds;
        renderGlobalThresholds(globalThresholds);
      }
      return;
    }

    if (event === "didReceiveSettings") {
      applyTileSettingsToUI(payload.settings || {});
      return;
    }

    if (event === "sendToPropertyInspector") {
      if (Array.isArray(payload.globalThresholds)) {
        globalThresholds = payload.globalThresholds;
        renderGlobalThresholds(globalThresholds);
      }
      if (payload.connectionStatus !== undefined || payload.pollInterval !== undefined) {
        applyStatusPayload(payload);
      }
    }
  });
}

window.addEventListener("beforeunload", function () {
  sendValueToPlugin("propertyInspectorWillDisappear", "property_inspector");
});

// --- Global threshold library editor ---
// Edits apply plugin-wide; the host broadcasts the updated list to every PI.
var globalThresholdAdvancedOpen = {};

function isActivelyEditing(el) {
  return !!el && document.activeElement === el;
}

function applyInputValue(el, value) {
  if (!el || isActivelyEditing(el)) return;
  var next = value === undefined || value === null ? "" : String(value);
  if (el.value !== next) el.value = next;
}

function sendGlobalThresholdUpdate(id, field, value, checked) {
  if (!websocket || websocket.readyState !== 1) return;
  var upd = { id: id, field: field, value: String(value) };
  if (typeof checked === "boolean") upd.checked = checked;
  websocket.send(JSON.stringify({
    action: actionInfo["action"],
    event: "sendToPlugin",
    context: uuid,
    payload: { updateGlobalThreshold: upd }
  }));
}

function renderGlobalThresholds(thresholds) {
  var container = document.getElementById("globalThresholdsContainer");
  if (!container) return;
  var list = thresholds || [];
  var existingItems = container.querySelectorAll(".threshold-item");
  var existingIds = Array.prototype.map.call(existingItems, function (el) { return el.dataset.thresholdId; });
  var incomingIds = list.map(function (t) { return t.id; });
  if (JSON.stringify(existingIds) !== JSON.stringify(incomingIds)) {
    container.innerHTML = "";
    list.forEach(function (t) { container.appendChild(createGlobalThresholdElement(t)); });
    return;
  }
  var active = document.activeElement;
  list.forEach(function (t) {
    var item = container.querySelector('.threshold-item[data-threshold-id="' + t.id + '"]');
    if (!item || (active && item.contains(active))) return;
    applyInputValue(item.querySelector(".threshold-name"), t.name || "");
    applyInputValue(item.querySelector(".threshold-text"), t.text || "");
    applyInputValue(item.querySelector(".threshold-value"), t.value != null ? t.value : "");
    applyInputValue(item.querySelector(".threshold-hysteresis"), t.hysteresis != null ? t.hysteresis : "");
    applyInputValue(item.querySelector(".threshold-dwell"), t.dwellMs != null ? t.dwellMs : "");
    applyInputValue(item.querySelector(".threshold-cooldown"), t.cooldownMs != null ? t.cooldownMs : "");
  });
}

function createGlobalThresholdElement(threshold) {
  var template = document.getElementById("globalThresholdTemplate");
  if (!template) return document.createDocumentFragment();
  var clone = template.content.cloneNode(true);
  var wrapper = clone.querySelector(".threshold-item");
  wrapper.dataset.thresholdId = threshold.id;

  var nameInput = clone.querySelector(".threshold-name");
  nameInput.value = threshold.name || "";
  var textInput = clone.querySelector(".threshold-text");
  textInput.value = threshold.text || "";
  var readingTypeSelect = clone.querySelector(".threshold-reading-type");
  if (readingTypeSelect) readingTypeSelect.value = threshold.readingType || "";
  var operatorSelect = clone.querySelector(".threshold-operator");
  operatorSelect.value = threshold.operator || ">=";
  var valueInput = clone.querySelector(".threshold-value");
  valueInput.value = threshold.value != null ? threshold.value : "";
  var hysteresisInput = clone.querySelector(".threshold-hysteresis");
  hysteresisInput.value = threshold.hysteresis != null ? threshold.hysteresis : "";
  var dwellInput = clone.querySelector(".threshold-dwell");
  dwellInput.value = threshold.dwellMs != null ? threshold.dwellMs : "";
  var cooldownInput = clone.querySelector(".threshold-cooldown");
  cooldownInput.value = threshold.cooldownMs != null ? threshold.cooldownMs : "";
  var bgInput = clone.querySelector(".threshold-bg");
  bgInput.value = threshold.backgroundColor || "#333300";
  var fgInput = clone.querySelector(".threshold-fg");
  fgInput.value = threshold.foregroundColor || "#999900";
  var hlInput = clone.querySelector(".threshold-hl");
  hlInput.value = threshold.highlightColor || "#ffff00";
  var vtInput = clone.querySelector(".threshold-vt");
  vtInput.value = threshold.valueTextColor || "#ffff00";
  var tcInput = clone.querySelector(".threshold-tc");
  if (tcInput) tcInput.value = threshold.textColor || "#ffffff";

  var toggleBtn = clone.querySelector(".threshold-toggle");
  var settingsDiv = clone.querySelector(".threshold-settings");
  var stickyBtn = clone.querySelector(".threshold-sticky-toggle");
  var advancedToggleBtn = clone.querySelector(".threshold-advanced-toggle");
  var advancedPanel = clone.querySelector(".threshold-advanced-panel");
  var thresholdId = threshold.id;
  var isEnabled = threshold.enabled !== false;
  var isSticky = threshold.sticky === true;
  var isAdvancedOpen = globalThresholdAdvancedOpen[thresholdId] === true;

  function updateToggleState() {
    toggleBtn.textContent = isEnabled ? "on" : "off";
    toggleBtn.style.background = isEnabled ? "#4a4" : "#a44";
    settingsDiv.style.display = isEnabled ? "block" : "none";
  }
  function updateStickyState() {
    stickyBtn.textContent = isSticky ? "on" : "off";
    stickyBtn.style.background = isSticky ? "#4a4" : "#a44";
    stickyBtn.style.color = "#fff";
  }
  function updateAdvancedState() {
    advancedToggleBtn.textContent = isAdvancedOpen ? "Advanced \u25bc" : "Advanced \u25b6";
    advancedPanel.style.display = isAdvancedOpen ? "block" : "none";
  }
  updateToggleState();
  updateStickyState();
  updateAdvancedState();

  toggleBtn.addEventListener("click", function () {
    isEnabled = !isEnabled;
    updateToggleState();
    sendGlobalThresholdUpdate(thresholdId, "thresholdEnabled", isEnabled ? "true" : "false", isEnabled);
  });
  var nameTimeout;
  nameInput.addEventListener("input", function (e) {
    clearTimeout(nameTimeout);
    nameTimeout = setTimeout(function () { sendGlobalThresholdUpdate(thresholdId, "thresholdName", e.target.value); }, 300);
  });
  var textTimeout;
  textInput.addEventListener("input", function (e) {
    clearTimeout(textTimeout);
    textTimeout = setTimeout(function () { sendGlobalThresholdUpdate(thresholdId, "thresholdText", e.target.value); }, 300);
  });
  if (readingTypeSelect) {
    readingTypeSelect.addEventListener("change", function (e) {
      sendGlobalThresholdUpdate(thresholdId, "thresholdReadingType", e.target.value);
    });
  }
  operatorSelect.addEventListener("change", function (e) {
    sendGlobalThresholdUpdate(thresholdId, "thresholdOperator", e.target.value);
  });
  var valueTimeout;
  valueInput.addEventListener("input", function (e) {
    clearTimeout(valueTimeout);
    valueTimeout = setTimeout(function () { sendGlobalThresholdUpdate(thresholdId, "thresholdValue", e.target.value); }, 300);
  });
  var hysteresisTimeout;
  hysteresisInput.addEventListener("input", function (e) {
    clearTimeout(hysteresisTimeout);
    hysteresisTimeout = setTimeout(function () { sendGlobalThresholdUpdate(thresholdId, "thresholdHysteresis", e.target.value); }, 300);
  });
  var dwellTimeout;
  dwellInput.addEventListener("input", function (e) {
    clearTimeout(dwellTimeout);
    dwellTimeout = setTimeout(function () { sendGlobalThresholdUpdate(thresholdId, "thresholdDwellMs", e.target.value); }, 300);
  });
  var cooldownTimeout;
  cooldownInput.addEventListener("input", function (e) {
    clearTimeout(cooldownTimeout);
    cooldownTimeout = setTimeout(function () { sendGlobalThresholdUpdate(thresholdId, "thresholdCooldownMs", e.target.value); }, 300);
  });
  if (stickyBtn) {
    stickyBtn.addEventListener("click", function () {
      isSticky = !isSticky;
      updateStickyState();
      sendGlobalThresholdUpdate(thresholdId, "thresholdSticky", isSticky ? "true" : "false", isSticky);
    });
  }
  if (advancedToggleBtn) {
    advancedToggleBtn.addEventListener("click", function () {
      isAdvancedOpen = !isAdvancedOpen;
      globalThresholdAdvancedOpen[thresholdId] = isAdvancedOpen;
      updateAdvancedState();
    });
  }
  bgInput.addEventListener("change", function (e) { sendGlobalThresholdUpdate(thresholdId, "thresholdBackgroundColor", e.target.value); });
  fgInput.addEventListener("change", function (e) { sendGlobalThresholdUpdate(thresholdId, "thresholdForegroundColor", e.target.value); });
  hlInput.addEventListener("change", function (e) { sendGlobalThresholdUpdate(thresholdId, "thresholdHighlightColor", e.target.value); });
  vtInput.addEventListener("change", function (e) { sendGlobalThresholdUpdate(thresholdId, "thresholdValueTextColor", e.target.value); });
  if (tcInput) {
    tcInput.addEventListener("change", function (e) { sendGlobalThresholdUpdate(thresholdId, "thresholdTextColor", e.target.value); });
  }
  var removeBtn = clone.querySelector(".threshold-remove");
  removeBtn.addEventListener("click", function () {
    delete globalThresholdAdvancedOpen[thresholdId];
    if (websocket && websocket.readyState === 1) {
      websocket.send(JSON.stringify({
        action: actionInfo["action"],
        event: "sendToPlugin",
        context: uuid,
        payload: { deleteGlobalThreshold: thresholdId }
      }));
    }
    wrapper.remove();
  });
  return clone;
}

function bindGlobalThresholdControls() {
  var addBtn = document.getElementById("addGlobalThresholdBtn");
  if (!addBtn || addBtn.dataset.bound) return;
  addBtn.dataset.bound = "1";
  addBtn.addEventListener("click", function (e) {
    e.preventDefault();
    e.stopPropagation();
    var nameEl = document.getElementById("newGlobalThresholdName");
    var name = nameEl ? nameEl.value.trim() : "";
    if (websocket && websocket.readyState === 1) {
      websocket.send(JSON.stringify({
        action: actionInfo["action"],
        event: "sendToPlugin",
        context: uuid,
        payload: { addGlobalThreshold: name }
      }));
    }
    if (nameEl) nameEl.value = "";
  });
}

function initSettingsPI() {
  bindStatusControls(function (value, key) { sendValueToPlugin(value, key); });
  bindAppearanceControls();
  bindGlobalThresholdControls();
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", initSettingsPI);
} else {
  initSettingsPI();
}
