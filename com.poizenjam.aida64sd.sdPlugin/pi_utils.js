// Shared by every Property Inspector, loaded before the PI's own script.
//
// PI_VERSION must equal the manifest Version. The Settings PI's Status section
// shows the host's version and flags a mismatch, which means a stale install
// or a cached copy of these scripts.
var PI_VERSION = "1.0.0.0";

/** Normalize a reading type string to its canonical short form ("Temp") */
function normalizeReadingType(t) {
  switch ((t || "").toLowerCase()) {
    case "temp": case "temperature": return "Temp";
    case "volt": case "voltage":     return "Volt";
    case "fan":                      return "Fan";
    case "current":                  return "Current";
    case "power":                    return "Power";
    case "clock":                    return "Clock";
    case "usage": case "load": case "control": case "level": return "Usage";
    case "rate":                     return "Rate";
    case "datasize": case "data size": return "DataSize";
    case "none":                     return "None";
    default:                         return t ? "Other" : "";
  }
}

function naturalCompare(left, right) {
  var a = String(left || "");
  var b = String(right || "");
  var partsA = a.match(/(\d+|\D+)/g) || [];
  var partsB = b.match(/(\d+|\D+)/g) || [];
  var maxLen = Math.max(partsA.length, partsB.length);

  for (var i = 0; i < maxLen; i++) {
    var partA = partsA[i];
    var partB = partsB[i];
    if (partA === undefined) return -1;
    if (partB === undefined) return 1;

    var numA = /^\d+$/.test(partA);
    var numB = /^\d+$/.test(partB);
    if (numA && numB) {
      var diff = parseInt(partA, 10) - parseInt(partB, 10);
      if (diff !== 0) return diff;
      continue;
    }

    var textA = partA.toLowerCase();
    var textB = partB.toLowerCase();
    if (textA > textB) return 1;
    if (textB > textA) return -1;
  }

  return 0;
}

// readingTypeFamily maps a canonical reading type to a picker sort family so
// readings of the same unit type stay together in the large System category:
// utilization first, then clock, rate, capacity, misc.
function readingTypeFamily(r) {
  var t = normalizeReadingType(r ? r.type : "").toLowerCase();
  if (t === "usage") return 0;
  if (t === "clock") return 1;
  if (t === "rate") return 2;
  if (t === "datasize") return 3;
  if (t === "temp") return 10;
  if (t === "volt") return 11;
  if (t === "fan") return 12;
  if (t === "current") return 13;
  if (t === "power") return 14;
  if (t === "none") return 20;
  return 21;
}

// compareReadings groups System-category readings by unit family first (the
// host does not tag per-reading units in shared memory, so the reading type is
// the only reliable grouping signal there). Non-System categories keep the
// historical prefix -> label -> type -> id ordering.
function compareReadings(a, b) {
  if ((a.category || "") === "sys" && (b.category || "") === "sys") {
    var famDiff = readingTypeFamily(a) - readingTypeFamily(b);
    if (famDiff !== 0) return famDiff;
  }
  var prefixDiff = naturalCompare(a.prefix || a.unit || "", b.prefix || b.unit || "");
  if (prefixDiff !== 0) return prefixDiff;

  var labelDiff = naturalCompare(a.label || "", b.label || "");
  if (labelDiff !== 0) return labelDiff;

  var typeDiff = naturalCompare(a.type || "", b.type || "");
  if (typeDiff !== 0) return typeDiff;

  return naturalCompare(String(a.id || ""), String(b.id || ""));
}

// readingOptionLabel is the single source of truth for how a reading is shown in
// every sensor/reading dropdown (key, dial, derived, composite). Keeping it here
// means the actions can never drift apart again (e.g. "Memory (%)" vs "% Memory").
function readingOptionLabel(reading) {
  if (!reading) return "";
  var label = reading.label || "";
  return reading.unit ? label + " (" + reading.unit + ")" : label;
}

function byId(id) {
  return document.getElementById(id);
}

function normalizeHexColor(hex) {
  if (typeof hex !== "string" || !hex) return hex;
  if (hex.length === 4 && hex.charAt(0) === "#") {
    return "#" + hex[1] + hex[1] + hex[2] + hex[2] + hex[3] + hex[3];
  }
  return hex;
}

function setInputValue(id, val) {
  var el = byId(id);
  if (el) {
    el.value = val == null ? "" : val;
  }
  return el;
}

function setColorValue(id, hex) {
  var el = byId(id);
  if (!el || !hex) return el;
  el.value = normalizeHexColor(hex);
  return el;
}

function setSelectValue(id, val) {
  var el = byId(id);
  if (!el || val == null) return el;
  var target = String(val);
  for (var i = 0; i < el.options.length; i++) {
    if (el.options[i].value === target) {
      el.selectedIndex = i;
      break;
    }
  }
  return el;
}

function bindValueChange(id, eventName, handler) {
  var el = byId(id);
  if (!el) return null;
  el[eventName || "onchange"] = function () {
    handler(el.value, el);
  };
  return el;
}

function positionRangeVal(inp) {
  var wrap = inp.parentElement;
  var span = wrap && wrap.querySelector(".range-val");
  if (!span) return;
  span.textContent = inp.value;
  requestAnimationFrame(function() {
    var pct = (parseFloat(inp.value) - parseFloat(inp.min)) / (parseFloat(inp.max) - parseFloat(inp.min));
    span.style.left = (pct * (inp.offsetWidth - 14) + 7) + "px";
  });
}

function bindSdpiValue(id, sendSdpi, eventName, extra) {
  return bindValueChange(id, eventName, function (value, el) {
    sendSdpi(id, value);
    if (extra) {
      extra(value, el);
    }
  });
}

// --- Connection / Refresh block ---
// The host pushes {connectionStatus, pollInterval, sensorCount, autoRefresh,
// iniPath, iniPathSet, iniRateMs, iniRateAppliedMs, iniStatus, iniRateKey,
// iniRateKeySet, iniNote, iniKeyOptions, autoDiagnose, hostVersion} to every
// open PI; the Settings PI renders them with applyStatusPayload. Those pushes
// are its only input besides the optimistic local apply on user input.
//
// Auto refresh follows AIDA64's own rate from AIDA64.INI ([Generic]
// HWMonUpdateFreq* keys, default HWMonUpdateFreqExtApp). The path and key can
// be overridden here; Refresh forces a re-read. When the INI cannot be read
// the manual interval applies and the readout says why.
//
// The readout shows the clamped rate the ticker runs at (iniRateAppliedMs);
// when that differs from the raw INI value, the raw value is shown too.
function iniReadoutText(payload) {
  var manual = payload.pollInterval ? payload.pollInterval + "ms" : "--";
  if (payload.autoRefresh !== true) return manual;
  if (payload.iniStatus === "ok" && payload.iniRateMs) {
    var applied = payload.iniRateAppliedMs || payload.iniRateMs;
    if (applied !== payload.iniRateMs) {
      return "auto (" + applied + "ms, INI " + payload.iniRateMs + "ms)";
    }
    return "auto (" + applied + "ms, " + (payload.iniRateKey || "INI") + ")";
  }
  var reason;
  if (payload.iniStatus === "not-detected") reason = "INI not detected";
  else if (payload.iniStatus === "no-rate-key") reason = "INI has no rate key";
  else if (payload.iniStatus === "path-missing") reason = "INI path missing";
  else if (payload.iniStatus === "read-error") reason = "INI read error";
  else reason = "INI reading";
  return "auto (" + reason + ", " + manual + ")";
}

// The host's one-shot auto-refresh diagnostic, logged to the PI console so a
// state transition is visible without host log access.
function logAutoDiagnose(payload) {
  if (payload && payload.autoDiagnose) {
    console.log("Aida64SD " + payload.autoDiagnose);
  }
}

function iniKeyOptionsEl() {
  var sel = document.getElementById("iniRateKey");
  if (!sel) return null;
  return sel;
}

// (Re)build the rate-key selector: "(default)" plus the known HWMonUpdateFreq*
// keys the host reports, then reflect the configured override (iniRateKeySet).
// Options are only rebuilt when the key list changes; the selection is always
// re-synced so a later push (after a key change) still lands.
function syncIniKeyOptions(payload) {
  var sel = iniKeyOptionsEl();
  if (!sel) return;
  var opts = payload.iniKeyOptions || [];
  if (opts.length) {
    if (sel.dataset.optionsJson !== JSON.stringify(opts) || sel.options.length <= 1) {
      sel.dataset.optionsJson = JSON.stringify(opts);
      sel.length = 0;
      sel.add(new Option("(default: " + (opts.indexOf("HWMonUpdateFreqExtApp") >= 0 ? "HWMonUpdateFreqExtApp" : "auto") + ")", ""));
      for (var i = 0; i < opts.length; i++) {
        sel.add(new Option(opts[i], opts[i]));
      }
    }
  }
  var want = payload.iniRateKeySet || "";
  for (var j = 0; j < sel.options.length; j++) {
    if (sel.options[j].value === want) {
      sel.selectedIndex = j;
      return;
    }
  }
  sel.selectedIndex = 0; // empty override or unknown key: fall back to default
}

function isActivelyEditingEl(el) {
  return !!el && document.activeElement === el;
}

function applyStatusPayload(payload) {
  if (!payload) return;
  logAutoDiagnose(payload);
  var statusEl = document.getElementById("connectionStatus");
  if (statusEl && payload.connectionStatus) {
    statusEl.textContent = payload.connectionStatus;
    statusEl.style.color = payload.connectionStatus === "Connected" ? "#4a4" : "#a44";
  }
  var rateEl = document.getElementById("currentRate");
  var pollEl = document.getElementById("pollInterval");
  var autoEl = document.getElementById("autoRefresh");
  var auto = payload.autoRefresh === true;
  // The select's value is "auto" (the Auto option) or a numeric ms. When
  // auto, the numeric poll interval is informational and must not clobber
  // the "auto" selection; when manual, sync the numeric value.
  if (auto) {
    if (pollEl && pollEl.value !== "auto") {
      pollEl.value = "auto";
    }
  } else if (payload.pollInterval) {
    if (pollEl && pollEl.value !== String(payload.pollInterval)) {
      pollEl.value = String(payload.pollInterval);
    }
  }
  if (autoEl && payload.autoRefresh !== undefined && !autoEl.dataset.userTouched) {
    autoEl.checked = auto;
  }
  // AIDA64.INI path: the configured override (empty = auto-detect). Never
  // clobber while the user is typing.
  var iniEl = document.getElementById("iniPath");
  if (iniEl && !isActivelyEditingEl(iniEl)) {
    var want = payload.iniPathSet || "";
    if (iniEl.value !== want) iniEl.value = want;
  }
  syncIniKeyOptions(payload);
  var verEl = document.getElementById("pluginVersion");
  if (verEl && payload.hostVersion) {
    var stale = payload.hostVersion !== PI_VERSION;
    verEl.textContent = payload.hostVersion + (stale ? " (PI scripts " + PI_VERSION + ": mismatch)" : "");
    verEl.style.color = stale ? "#a44" : "#999";
  }
  if (rateEl) {
    rateEl.textContent = iniReadoutText(payload);
  }
}

function bindStatusControls(sendCommand) {
  var pollEl = document.getElementById("pollInterval");
  if (!pollEl || pollEl.dataset.statusBound) return;
  pollEl.dataset.statusBound = "1";
  var autoEl = document.getElementById("autoRefresh");
  // The dropdown's "auto" option and the checkbox send the same command; the
  // host owns AutoRefresh and pushes the settled state back.
  pollEl.addEventListener("change", function (e) {
    if (e.target.value === "auto") {
      if (autoEl) {
        autoEl.checked = true;
        autoEl.dataset.userTouched = "1";
      }
      sendCommand("auto", "setPollInterval");
      applyStatusPayload({ autoRefresh: true });
    } else {
      var ms = parseInt(e.target.value, 10);
      if (!ms) ms = 1000;
      if (autoEl) {
        autoEl.checked = false;
        autoEl.dataset.userTouched = "1";
      }
      sendCommand(ms, "setPollInterval");
      applyStatusPayload({ pollInterval: ms, autoRefresh: false });
    }
  });
  if (autoEl) {
    autoEl.addEventListener("change", function (e) {
      autoEl.dataset.userTouched = "1";
      if (autoEl.checked) {
        if (pollEl) pollEl.value = "auto";
        sendCommand("auto", "setPollInterval");
        applyStatusPayload({ autoRefresh: true });
      } else {
        // Turning auto off from the checkbox: use the current numeric interval
        // (or the 1 s default when the select was on "auto").
        var ms = parseInt(pollEl && pollEl.value, 10) || 1000;
        if (pollEl) pollEl.value = String(ms);
        sendCommand(ms, "setPollInterval");
        applyStatusPayload({ pollInterval: ms, autoRefresh: false });
      }
    });
  }
  var iniEl = document.getElementById("iniPath");
  if (iniEl) {
    iniEl.addEventListener("change", function (e) {
      sendCommand(e.target.value.trim(), "setIniPath");
    });
  }
  var keySel = document.getElementById("iniRateKey");
  if (keySel) {
    keySel.addEventListener("change", function (e) {
      sendCommand(e.target.value, "setIniRateKey");
    });
  }
  var refreshBtn = document.getElementById("refreshIniBtn");
  if (refreshBtn) {
    refreshBtn.addEventListener("click", function (e) {
      e.preventDefault();
      e.stopPropagation();
      sendCommand(true, "refreshIniRate");
    });
  }
}

// --- Socket and messaging ---
// Each PI script declares the globals websocket, uuid and actionInfo; these
// helpers set and use them, so a PI only supplies its own message handling.

function parseJSONOrEmpty(raw) {
  if (!raw || typeof raw !== "string") return {};
  try {
    return JSON.parse(raw);
  } catch (_err) {
    return {};
  }
}

function sendJson(payload) {
  if (!websocket || websocket.readyState !== 1) return false;
  websocket.send(JSON.stringify(payload));
  return true;
}

// sendToPlugin with this PI's registration UUID as context (Stream Deck
// rejects the action context from a PI).
function sendValueToPlugin(value, param) {
  var payload = {};
  payload[param] = value;
  sendJson({ action: actionInfo.action, event: "sendToPlugin", context: uuid, payload: payload });
}

// openPISocket registers the PI and announces it to the host, whose only
// connect trigger is this propertyInspectorConnected message; the host answers
// with the PI's data. onOpen (optional) runs after both sends; onMessage gets
// (event, payload, message) for every message from Stream Deck.
function openPISocket(inPort, inUUID, inRegisterEvent, inActionInfo, onOpen, onMessage) {
  uuid = inUUID;
  actionInfo = parseJSONOrEmpty(inActionInfo);
  var host = (typeof location !== "undefined" && location.hostname) ? location.hostname : "127.0.0.1";
  websocket = new WebSocket("ws://" + host + ":" + inPort);
  websocket.onopen = function () {
    sendJson({ event: inRegisterEvent, uuid: inUUID });
    sendValueToPlugin("propertyInspectorConnected", "property_inspector");
    if (onOpen) onOpen();
  };
  websocket.onmessage = function (evt) {
    var message = parseJSONOrEmpty(evt.data);
    onMessage(message.event, message.payload || {}, message);
  };
}

// handleErrorPayload applies a host {error, message} push to the #ui and
// #error panels and returns false for any other payload. With reannounce, the
// "show_ui" recovery re-sends propertyInspectorConnected so the host refills
// the PI; the Dial passes false because its host sends show_ui on every
// connect, which would loop.
function handleErrorPayload(payload, reannounce) {
  if (typeof payload.error !== "boolean") return false;
  var ui = byId("ui"), err = byId("error");
  if (ui) ui.style.display = payload.error ? "none" : "";
  if (err) err.style.display = payload.error ? "block" : "none";
  if (reannounce && !payload.error && payload.message === "show_ui") {
    sendValueToPlugin("propertyInspectorConnected", "property_inspector");
  }
  return true;
}

// --- Source filter and sensor picker ---
// The Source filter only appears when the host reports more than one sensor
// source (AIDA64 itself is local-only, so normally it stays hidden).

// Sensor uids from a non-local source are "<source>::<uid>".
function sourceOfUid(uid) {
  var i = (uid || "").indexOf("::");
  return i >= 0 ? uid.slice(0, i) : "";
}

// sourceFilterValue is the chosen source id, or null when there is no filter.
function sourceFilterValue(sources, selectId) {
  if (!sources || sources.length <= 1) return null;
  var el = byId(selectId);
  return el ? el.value : null;
}

// renderSourceFilterSelect fills the Source select and shows its row for more
// than one source. Until the user picks one it preselects fallbackSource (the
// configured sensor's source); onChange runs after a user pick. Returns
// whether the filter is shown.
function renderSourceFilterSelect(sources, selectId, fallbackSource, onChange) {
  var row = byId("sourceFilterRow");
  var heading = byId("sourceHeading");
  var el = byId(selectId);
  if (!row || !el) return false;

  var multi = !!sources && sources.length > 1;
  row.style.display = multi ? "" : "none";
  if (heading) heading.style.display = multi ? "" : "none";
  if (!multi) return false;

  var selected = el.dataset.userChoice !== undefined ? el.value : fallbackSource;
  el.innerHTML = "";
  sources.forEach(function (src) {
    var opt = document.createElement("option");
    opt.value = src.id;
    opt.textContent = src.name;
    if (src.id === selected) opt.selected = true;
    el.add(opt);
  });

  if (!el.dataset.bound) {
    el.dataset.bound = "1";
    el.addEventListener("change", function () {
      el.dataset.userChoice = "1";
      onChange();
    });
  }
  return true;
}

// fillSensorSelect lists sensors in natural order under a placeholder,
// filtered to source unless it is null. The configured sensor stays listed
// even when the filter would hide it.
function fillSensorSelect(el, sensors, currentUid, source) {
  if (!el) return;
  currentUid = currentUid || "";
  var sorted = (Array.isArray(sensors) ? sensors.slice() : []).sort(function (a, b) {
    return naturalCompare(a.name || "", b.name || "");
  });
  if (source !== null) {
    sorted = sorted.filter(function (s) {
      return (s.source || "") === source || s.uid === currentUid;
    });
  }
  el.innerHTML = "";
  var placeholder = document.createElement("option");
  placeholder.text = sorted.length ? "Choose a sensor" : "No sensors yet (AIDA64 catalog empty)";
  placeholder.value = "";
  placeholder.disabled = true;
  if (!currentUid) placeholder.selected = true;
  el.add(placeholder);
  var found = false;
  sorted.forEach(function (s) {
    var option = document.createElement("option");
    option.text = s.name || s.uid;
    option.value = s.uid;
    if (s.uid === currentUid) { option.selected = true; found = true; }
    el.add(option);
  });
  if (!found && currentUid) placeholder.selected = true;
  el.removeAttribute("disabled");
}

// --- Threshold snooze presets (Sensor and Dial PIs) ---
var snoozeDurationOptions = [300000, 900000, 3600000, 0];

function normalizeSnoozeDurations(values) {
  if (!Array.isArray(values)) return [];
  var seen = Object.create(null);
  values.forEach(function (value) {
    var parsed = parseInt(value, 10);
    if (!isNaN(parsed)) seen[parsed] = true;
  });
  return snoozeDurationOptions.filter(function (value) {
    return seen[value] === true;
  });
}

function readSnoozeDurationsFromUI() {
  var selected = [];
  Array.from(document.querySelectorAll(".snooze-duration")).forEach(function (button) {
    if (button.classList.contains("is-selected")) {
      selected.push(parseInt(button.dataset.value, 10));
    }
  });
  return normalizeSnoozeDurations(selected);
}

function setSnoozePresetSelected(button, selected) {
  if (!button || !button.classList) return;
  button.classList.toggle("is-selected", selected === true);
}

// applySnoozeDurationsToUI takes a tile's settings or a dial page.
function applySnoozeDurationsToUI(holder) {
  var selected = normalizeSnoozeDurations(holder && holder.snoozeDurations ? holder.snoozeDurations : []);
  var selectedMap = Object.create(null);
  selected.forEach(function (value) {
    selectedMap[String(value)] = true;
  });
  Array.from(document.querySelectorAll(".snooze-duration")).forEach(function (button) {
    setSnoozePresetSelected(button, selectedMap[button.dataset.value] === true);
  });
}
