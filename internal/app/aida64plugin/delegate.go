package aida64plugin

import (
	"encoding/json"
	"fmt"
	"image/color"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/poizenjam/aida64sd/pkg/graph"
	service "github.com/poizenjam/aida64sd/pkg/service"
	"github.com/poizenjam/aida64sd/pkg/streamdeck"
)

const (
	tileWidth  = 72
	tileHeight = 72
)

func boolPtr(v bool) *bool {
	return &v
}

// isSettingsPayload reports whether a sendToPlugin payload carries a
// plugin-wide command (refresh, INI, global thresholds, Settings key
// appearance). They route on payload shape, not on the sending action.
func isSettingsPayload(m map[string]*json.RawMessage) bool {
	if m == nil {
		return false
	}
	for _, k := range []string{"setPollInterval", "addGlobalThreshold",
		"deleteGlobalThreshold", "updateGlobalThreshold", "refreshIniRate",
		"setIniPath", "setIniRateKey", "updateTileAppearance", "requestSettingsStatus"} {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

// OnConnected event
func (p *Plugin) OnConnected(c *websocket.Conn) {
	// Request global settings on connect
	if err := p.sd.GetGlobalSettings(); err != nil {
		log.Printf("GetGlobalSettings failed: %v\n", err)
	}
}

// OnWillAppear event
func (p *Plugin) OnWillAppear(event *streamdeck.EvWillAppear) {
	if event.Action == dialAction && event.Payload.Controller == "Encoder" {
		p.handleDialWillAppear(event)
		return
	}

	if event.Action == settingsAction {
		p.handleSettingsWillAppear(event)
		return
	}

	if event.Action == derivedAction {
		ds, _ := decodeDerivedSettings(event.Payload.Settings)
		p.mu.Lock()
		p.derivedSettings[event.Context] = &ds
		p.derivedStates[event.Context] = &derivedState{graph: initDerivedGraph(&ds)}
		p.mu.Unlock()
		return
	}

	if event.Action == compositeAction {
		cs, _ := decodeCompositeSettings(event.Payload.Settings)
		p.mu.Lock()
		p.compositeSettings[event.Context] = &cs
		p.compositeStates[event.Context] = &compositeState{graphs: initCompositeGraphs(&cs)}
		p.mu.Unlock()
		return
	}

	settings, migrated, err := decodeActionSettings(event.Payload.Settings)
	if err != nil {
		log.Println("OnWillAppear settings unmarshal", err)
	}
	tfSize := 10.5
	vfSize := 10.5
	var fgColor *color.RGBA
	var bgColor *color.RGBA
	var hlColor *color.RGBA
	var tColor *color.RGBA
	var vtColor *color.RGBA
	if settings.TitleFontSize != 0 {
		tfSize = settings.TitleFontSize
	}
	if settings.ValueFontSize != 0 {
		vfSize = settings.ValueFontSize
	}
	if settings.ForegroundColor == "" {
		fgColor = &color.RGBA{0, 81, 40, 255}
	} else {
		fgColor = hexToRGBA(settings.ForegroundColor)
	}
	if settings.BackgroundColor == "" {
		bgColor = &color.RGBA{0, 0, 0, 255}
	} else {
		bgColor = hexToRGBA(settings.BackgroundColor)
	}
	if settings.HighlightColor == "" {
		hlColor = &color.RGBA{0, 158, 0, 255}
	} else {
		hlColor = hexToRGBA(settings.HighlightColor)
	}
	if settings.TitleColor == "" {
		tColor = &color.RGBA{183, 183, 183, 255}
	} else {
		tColor = hexToRGBA(settings.TitleColor)
	}
	if settings.ValueTextColor == "" {
		vtColor = &color.RGBA{255, 255, 255, 255}
	} else {
		vtColor = hexToRGBA(settings.ValueTextColor)
	}
	drawTitle := true
	if settings.ShowTitleInGraph != nil {
		drawTitle = *settings.ShowTitleInGraph
	} else {
		settings.ShowTitleInGraph = boolPtr(drawTitle)
	}
	g := graph.NewGraph(tileWidth, tileHeight, settings.Min, settings.Max, fgColor, bgColor, hlColor)
	g.SetLabel(0, "", 19, tColor)
	g.SetLabelFontSize(0, tfSize)
	g.SetLabel(1, "", 44, vtColor)
	g.SetLabelFontSize(1, vfSize)
	g.SetLabel(2, "", 56, vtColor)
	g.SetLabelFontSize(2, vfSize)
	if settings.GraphHeightPct > 0 {
		g.SetHeightPct(settings.GraphHeightPct)
	}
	if settings.GraphLineThickness > 0 {
		g.SetLineThickness(settings.GraphLineThickness)
	}
	g.SetTextStroke(settings.TextStroke)
	if settings.TextStrokeColor != "" {
		g.SetTextStrokeColor(hexToRGBA(settings.TextStrokeColor))
	}
	if drawTitle {
		g.SetLabelText(0, settings.Title)
	}
	p.mu.Lock()
	p.graphs[event.Context] = g
	p.mu.Unlock()
	p.resetThresholdRuntimeState(event.Context, "")

	// Reset threshold state so updateTiles will re-evaluate and apply correct colors on first run
	if settings.CurrentThresholdID != "" {
		settings.CurrentThresholdID = ""
		migrated = true
	}
	// Legacy: reset old alert state
	if settings.CurrentAlertState != "" {
		settings.CurrentAlertState = ""
		migrated = true
	}

	// Ensure all enabled thresholds have operators
	for i := range settings.Thresholds {
		if settings.Thresholds[i].Enabled && settings.Thresholds[i].Operator == "" {
			settings.Thresholds[i].Operator = ">="
			migrated = true
		}
	}

	// Legacy: Set default operators for old Warning/Critical if still present
	if settings.WarningEnabled && settings.WarningOperator == "" {
		settings.WarningOperator = ">="
		migrated = true
	}
	if settings.CriticalEnabled && settings.CriticalOperator == "" {
		settings.CriticalOperator = ">="
		migrated = true
	}

	p.am.SetAction(event.Action, event.Context, &settings)
	if migrated {
		_ = p.sd.SetSettings(event.Context, &settings)
	}
}

// OnWillDisappear event
func (p *Plugin) OnWillDisappear(event *streamdeck.EvWillDisappear) {
	if event.Action == dialAction && event.Payload.Controller == "Encoder" {
		p.handleDialWillDisappear(event)
		return
	}

	if event.Action == settingsAction {
		p.handleSettingsWillDisappear(event)
		return
	}

	// A key that reappears gets its manifest image back, so a disconnected
	// key must be marked again to show the disconnected image.
	p.clearDisconnected(event.Context)

	if event.Action == derivedAction {
		p.mu.Lock()
		delete(p.derivedSettings, event.Context)
		delete(p.derivedStates, event.Context)
		p.mu.Unlock()
		return
	}

	if event.Action == compositeAction {
		p.mu.Lock()
		delete(p.compositeSettings, event.Context)
		delete(p.compositeStates, event.Context)
		p.mu.Unlock()
		for i := 0; i < 4; i++ {
			p.clearThresholdRuntimeState(event.Context + "|" + strconv.Itoa(i))
		}
		return
	}

	_, _, err := decodeActionSettings(event.Payload.Settings)
	if err != nil {
		log.Println("OnWillDisappear settings unmarshal", err)
	}
	p.mu.Lock()
	delete(p.graphs, event.Context)
	delete(p.divisorCache, event.Context)
	delete(p.piContexts, event.Context)
	p.mu.Unlock()
	p.clearThresholdRuntimeState(event.Context)
	p.am.RemoveAction(event.Context)
}

// OnKeyDown snoozes or resumes active threshold alerts for reading tiles.
func (p *Plugin) OnKeyDown(event *streamdeck.EvKeyDown) {
	if event.Action != "com.poizenjam.aida64sd.reading" {
		return
	}

	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		log.Printf("OnKeyDown getSettings: %v\n", err)
		return
	}

	if settings.CurrentThresholdID == "" {
		return
	}

	if configured := normalizeThresholdSnoozeDurations(settings.SnoozeDurations); len(configured) > 0 {
		now := time.Now()
		currentSnooze, snoozed := p.currentThresholdSnooze(event.Context, now)
		var current *thresholdSnoozeState
		if snoozed {
			current = &currentSnooze
		}

		if nextDuration, ok := nextThresholdSnoozeDuration(configured, current); ok {
			p.setThresholdSnooze(event.Context, nextDuration, now)
		} else if !p.clearThresholdSnooze(event.Context) {
			return
		}

		p.refreshAction(event.Action, event.Context)
		return
	}

	if !p.clearStickyThreshold(event.Context, settings.CurrentThresholdID) {
		return
	}

	settings.CurrentThresholdID = ""
	if err := p.sd.SetSettings(event.Context, &settings); err != nil {
		log.Printf("OnKeyDown SetSettings: %v\n", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	p.refreshAction(event.Action, event.Context)
}

// isAida64App reports whether an application event is for AIDA64 itself (the
// app this plugin's backend depends on). Only AIDA64's own launch/terminate
// change bridge liveness; the Stream Deck app and other apps are irrelevant.
func isAida64App(app string) bool {
	return strings.EqualFold(strings.TrimSpace(app), "AIDA64.EXE")
}

// OnApplicationDidLaunch: AIDA64 (re)started. It needs a moment to open the
// block, so stay Disconnected and drop the stale cached token; the 1 s sampler
// flips to Connected once the bridge reports the block available. The log
// line confirms whether Stream Deck delivers this event at all.
func (p *Plugin) OnApplicationDidLaunch(event *streamdeck.EvApplication) {
	if event == nil || !isAida64App(event.Payload.Application) {
		return
	}
	log.Printf("AIDA64 launched\n")
	rt := p.bridge
	rt.mu.Lock()
	invalidatePollCacheForRuntime(rt)
	rt.mu.Unlock()
	p.mu.Lock()
	p.bridgeAvailable = false
	p.mu.Unlock()
	p.sendStatusToPIs()
}

// OnApplicationDidTerminate: AIDA64 exited. Mark the bridge down and drop the
// cached token so the Connection line reads Disconnected immediately. This is
// only a fast path: the 1 s sampler's liveness read is authoritative and also
// covers a crash that never fires the event.
func (p *Plugin) OnApplicationDidTerminate(event *streamdeck.EvApplication) {
	if event == nil || !isAida64App(event.Payload.Application) {
		return
	}
	log.Printf("AIDA64 terminated\n")
	rt := p.bridge
	rt.mu.Lock()
	invalidatePollCacheForRuntime(rt)
	rt.mu.Unlock()
	p.mu.Lock()
	p.bridgeAvailable = false
	p.mu.Unlock()
	p.sendStatusToPIs()
}

// OnTitleParametersDidChange event
func (p *Plugin) OnTitleParametersDidChange(event *streamdeck.EvTitleParametersDidChange) {
	if event.Action == settingsAction {
		p.handleSettingsTitleParametersDidChange(event)
		return
	}

	if event.Action == derivedAction {
		p.handleDerivedTitleParametersDidChange(event)
		return
	}

	if event.Action == compositeAction {
		return // the composite tile draws no SD-native title
	}

	// Get existing settings from actionManager to preserve threshold settings
	// Do NOT decode from event payload as it may have stale/different settings
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		// If no settings in actionManager yet, decode from payload as fallback
		settings, _, err = decodeActionSettings(event.Payload.Settings)
		if err != nil {
			log.Println("OnTitleParametersDidChange settings unmarshal", err)
		}
	}
	p.mu.RLock()
	g, ok := p.graphs[event.Context]
	p.mu.RUnlock()
	if !ok {
		log.Printf("OnTitleParametersDidChange no graph for context: %s\n", event.Context)
		return
	}
	drawTitle := !event.Payload.TitleParameters.ShowTitle
	if drawTitle {
		g.SetLabelText(0, event.Payload.Title)
		if event.Payload.TitleParameters.TitleColor != "" {
			tClr := hexToRGBA(event.Payload.TitleParameters.TitleColor)
			g.SetLabelColor(0, tClr)
		}
	} else {
		g.SetLabelText(0, "")
	}
	// Only update title-related fields, preserving threshold settings
	settings.Title = event.Payload.Title
	settings.TitleColor = event.Payload.TitleParameters.TitleColor
	settings.ShowTitleInGraph = boolPtr(drawTitle)

	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		log.Printf("OnTitleParametersDidChange SetSettings: %v\n", err)
		return
	}
	p.am.SetAction(event.Action, event.Context, &settings)
}

// OnPropertyInspectorConnected event
func (p *Plugin) OnPropertyInspectorConnected(event *streamdeck.EvSendToPlugin) {
	if event.Action == dialAction {
		p.handleDialPropertyInspectorConnected(event)
		return
	}

	if event.Action == settingsAction {
		p.handleSettingsPropertyInspectorConnected(event)
		return
	}

	if event.Action == derivedAction {
		p.handleDerivedPropertyInspectorConnected(event)
		return
	}

	if event.Action == compositeAction {
		p.handleCompositePropertyInspectorConnected(event)
		return
	}

	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		log.Println("OnPropertyInspectorConnected getSettings", err)
	}
	// Register the PI so live status pushes reach it until OnWillDisappear.
	p.mu.Lock()
	p.piContexts[event.Context] = event.Action
	p.mu.Unlock()
	p.sendStatusToPIs()
	sensors, err := p.sensorsWithTimeout(2 * time.Second)
	if err != nil {
		log.Println("OnPropertyInspectorConnected Sensors", err)
		go p.restartBridge(p.bridge)
		payload := evStatus{Error: true, Message: "AIDA64 Unavailable"}
		if err := p.sd.SendToPropertyInspector(event.Action, event.Context, payload); err != nil {
			log.Printf("OnPropertyInspectorConnected SendToPropertyInspector: %v\n", err)
		}
		settings.InErrorState = true
		if err := p.sd.SetSettings(event.Context, &settings); err != nil {
			log.Printf("OnPropertyInspectorConnected SetSettings: %v\n", err)
			return
		}
		p.am.SetAction(event.Action, event.Context, &settings)
		return
	}
	evsensors := make([]*evSendSensorsPayloadSensor, 0, len(sensors))
	for _, s := range sensors {
		evsensors = append(evsensors, sensorPayload(s.ID(), s.Name()))
	}
	payload := evSendSensorsPayload{Sensors: evsensors, Settings: &settings}
	err = p.sd.SendToPropertyInspector(event.Action, event.Context, payload)
	if err != nil {
		log.Println("OnPropertyInspectorConnected SendToPropertyInspector", err)
	}
	if err := p.sendCatalogToPropertyInspector(event.Action, event.Context, &settings, sensors); err != nil {
		log.Printf("OnPropertyInspectorConnected sendCatalogToPropertyInspector: %v\n", err)
	}
	p.mu.RLock()
	globals := make([]Threshold, len(p.globalSettings.GlobalThresholds))
	copy(globals, p.globalSettings.GlobalThresholds)
	p.mu.RUnlock()
	_ = p.sd.SendToPropertyInspector(event.Action, event.Context, map[string]interface{}{"globalThresholds": globals})
	if settings.SensorUID != "" {
		readings, rerr := p.sendReadingsToPropertyInspector(event.Action, event.Context, settings.SensorUID, &settings)
		if rerr == nil {
			if syncSettingsWithReadings(&settings, readings) {
				_ = p.sd.SetSettings(event.Context, &settings)
				p.am.SetAction(event.Action, event.Context, &settings)
			}
		}
	}
}

// OnPropertyInspectorDisappeared stops live status pushes to a PI that closed.
// Stream Deck's propertyInspectorDidDisappear and the PI's own
// propertyInspectorWillDisappear message both land here; the second is a no-op.
// The tile itself stays registered (that is OnWillDisappear's job).
func (p *Plugin) OnPropertyInspectorDisappeared(event *streamdeck.EvSendToPlugin) {
	p.mu.Lock()
	delete(p.piContexts, event.Context)
	p.mu.Unlock()
}

func (p *Plugin) sendReadingsToPropertyInspector(action, context, sensorID string, settings *actionSettings) ([]service.Reading, error) {
	rt := p.bridge
	rt.mu.RLock()
	hw := rt.hw
	rt.mu.RUnlock()
	if hw == nil {
		return nil, fmt.Errorf("AIDA64 bridge not ready")
	}
	readings, err := hw.ReadingsForSensorID(sensorID)
	if err != nil {
		log.Println("sendReadingsToPropertyInspector ReadingsForSensorID", err)
		return nil, err
	}
	evreadings := make([]*evSendReadingsPayloadReading, 0, len(readings))
	for _, r := range readings {
		evreadings = append(evreadings, &evSendReadingsPayloadReading{
			ID:     r.ID(),
			Label:  r.Label(),
			Prefix: r.Unit(),
			Unit:   r.Unit(),
			Type:   r.Type(),
		})
	}
	payload := evSendReadingsPayload{Readings: evreadings, Settings: settings}
	err = p.sd.SendToPropertyInspector(action, context, payload)
	if err != nil {
		log.Println("sendReadingsToPropertyInspector SendToPropertyInspector", err)
	}
	return readings, nil
}

// OnSendToPlugin event
func (p *Plugin) OnSendToPlugin(event *streamdeck.EvSendToPlugin) {
	var payload map[string]*json.RawMessage
	err := json.Unmarshal(*event.Payload, &payload)
	if err != nil {
		log.Println("OnSendToPlugin unmarshal", err)
	}

	if event.Action == dialAction && p.handleDialSendToPlugin(event, payload) {
		return
	}

	// Plugin-wide commands (refresh, INI, global thresholds, Settings key
	// appearance) route on payload shape.
	if isSettingsPayload(payload) {
		// Settings PI: immediate redraw after an appearance change.
		if raw, ok := payload["updateTileAppearance"]; ok {
			p.handleSettingsAppearance(event.Context, raw)
			return
		}

		// Settings PI: re-send status and the threshold library on request.
		if _, ok := payload["requestSettingsStatus"]; ok {
			p.sendStatusToPIs()
			p.sendGlobalThresholdsTo(event.Action, event.Context)
			return
		}

		// setPollInterval is the only runtime writer of AutoRefresh: "auto"
		// follows AIDA64's INI rate, a number pins the interval.
		if raw, ok := payload["setPollInterval"]; ok {
			var autoMode string
			if err := json.Unmarshal(*raw, &autoMode); err == nil && autoMode == "auto" {
				p.mu.Lock()
				p.globalSettings.AutoRefresh = nil
				p.lastCommandSet = true
				p.autoDiagnose = "auto=true from=setPollInterval(auto)"
				p.mu.Unlock()
				// Force the INI read: the 60 s watch may not have run since
				// the user toggled auto on.
				p.readIniRate(true)
				p.sendStatusToPIs()
				p.persistGlobalSettings()
				return
			}
			var intervalMs int
			if err := json.Unmarshal(*raw, &intervalMs); err == nil && intervalMs > 0 {
				f := false
				p.mu.Lock()
				p.globalSettings.AutoRefresh = &f
				p.lastCommandSet = true
				p.autoDiagnose = "auto=false from=setPollInterval(manual)"
				p.mu.Unlock()
				p.setPollInterval(intervalMs)
			}
			return
		}

		// Check for refreshIniRate (PI's Refresh button: force re-read the INI)
		if _, ok := payload["refreshIniRate"]; ok {
			p.handleIniRefresh()
			return
		}

		// Check for setIniPath (explicit AIDA64.INI path; empty = auto-detect)
		if raw, ok := payload["setIniPath"]; ok {
			var path string
			_ = json.Unmarshal(*raw, &path)
			p.handleIniPathChange(path)
			return
		}

		// Check for setIniRateKey ([Generic] rate key override; empty = default)
		if raw, ok := payload["setIniRateKey"]; ok {
			var key string
			_ = json.Unmarshal(*raw, &key)
			p.handleIniRateKeyChange(key)
			return
		}

		// Check for addGlobalThreshold
		if raw, ok := payload["addGlobalThreshold"]; ok {
			var name string
			_ = json.Unmarshal(*raw, &name)
			p.handleGlobalAddThreshold(name)
			return
		}

		// Check for deleteGlobalThreshold
		if raw, ok := payload["deleteGlobalThreshold"]; ok {
			var id string
			if err := json.Unmarshal(*raw, &id); err == nil {
				p.handleGlobalRemoveThreshold(id)
			}
			return
		}

		// Check for updateGlobalThreshold
		if raw, ok := payload["updateGlobalThreshold"]; ok {
			var upd struct {
				ID      string `json:"id"`
				Field   string `json:"field"`
				Value   string `json:"value"`
				Checked bool   `json:"checked"`
			}
			if err := json.Unmarshal(*raw, &upd); err == nil {
				p.handleGlobalThresholdUpdate(upd.ID, upd.Field, upd.Value, upd.Checked)
			}
			return
		}

	}

	if event.Action == derivedAction {
		if data, ok := payload["sdpi_collection"]; ok {
			sdpi := evSdpiCollection{}
			if err := json.Unmarshal(*data, &sdpi); err != nil {
				log.Printf("derived sdpi unmarshal: %v", err)
				return
			}
			switch sdpi.Key {
			case "derived_suppressGlobal":
				p.handleDerivedSuppressGlobal(event, &sdpi)
			case "derived_unsuppressGlobal":
				p.handleDerivedUnsuppressGlobal(event, &sdpi)
			case "derived_formula", "derived_slotCount", "derived_format", "derived_divisor",
				"derived_graphUnit", "derived_min", "derived_max",
				"derived_foregroundColor", "derived_backgroundColor", "derived_highlightColor",
				"derived_valueTextColor", "derived_titleColor", "derived_title",
				"derived_graphHeightPct", "derived_graphLineThickness", "derived_textStroke", "derived_textStrokeColor",
				"derived_updateIntervalOverrideMs", "derived_smoothingAlpha", "titleFontSize", "valueFontSize":
				p.handleDerivedGlobalField(event, &sdpi)
			case "allSlots_sensorSelect":
				p.handleDerivedAllSlotsSensor(event, &sdpi)
			default:
				slotIdx, field := parseCompositeSlotKey(sdpi.Key) // reuses the slot{N}_{field} key shape
				if slotIdx < 0 {
					log.Printf("derived unknown sdpi key: %s", sdpi.Key)
					return
				}
				switch field {
				case "sensorSelect":
					p.handleDerivedSlotSensorSelect(event, &sdpi, slotIdx)
				case "readingSelect":
					p.handleDerivedSlotReadingSelect(event, &sdpi, slotIdx)
				default:
					p.handleDerivedSlotField(event, &sdpi, slotIdx, field)
				}
			}
		}
		return
	}

	if event.Action == compositeAction {
		if data, ok := payload["sdpi_collection"]; ok {
			sdpi := evSdpiCollection{}
			if err := json.Unmarshal(*data, &sdpi); err != nil {
				log.Printf("composite sdpi unmarshal: %v", err)
				return
			}
			switch sdpi.Key {
			case "composite_mode", "composite_slotCount", "updateIntervalOverrideMs", "smoothingAlpha":
				p.handleCompositeGlobalField(event, &sdpi)
			case "composite_applyToAll":
				p.handleCompositeApplyToAll(event)
			default:
				slotIdx, field := parseCompositeSlotKey(sdpi.Key)
				if slotIdx < 0 {
					log.Printf("composite unknown sdpi key: %s", sdpi.Key)
					return
				}
				switch field {
				case "sensorSelect":
					p.handleCompositeSlotSensorSelect(event, &sdpi, slotIdx)
				case "readingSelect":
					p.handleCompositeSlotReadingSelect(event, &sdpi, slotIdx)
				case "suppressGlobal":
					p.handleCompositeSlotSuppressGlobal(event, &sdpi, slotIdx)
				case "unsuppressGlobal":
					p.handleCompositeSlotUnsuppressGlobal(event, &sdpi, slotIdx)
				case "addThreshold":
					p.handleCompositeAddThreshold(event, &sdpi, slotIdx)
				case "removeThreshold":
					p.handleCompositeRemoveThreshold(event, &sdpi, slotIdx)
				case "reorderThreshold":
					p.handleCompositeReorderThreshold(event, &sdpi, slotIdx)
				case "thresholdEnabled", "thresholdName",
					"thresholdOperator", "thresholdValue", "thresholdHysteresis", "thresholdDwellMs",
					"thresholdCooldownMs", "thresholdSticky", "thresholdText", "thresholdTextColor",
					"thresholdBackgroundColor", "thresholdForegroundColor",
					"thresholdHighlightColor", "thresholdValueTextColor":
					p.handleCompositeThresholdUpdate(event, &sdpi, slotIdx)
				default:
					p.handleCompositeSlotField(event, &sdpi, slotIdx, field)
				}
			}
		}
		return
	}

	if data, ok := payload["sdpi_collection"]; ok {
		sdpi := evSdpiCollection{}
		err = json.Unmarshal(*data, &sdpi)
		if err != nil {
			log.Println("SDPI unmarshal", err)
		}
		switch sdpi.Key {
		case "sensorSelect":
			err = p.handleSensorSelect(event, &sdpi)
			if err != nil {
				log.Println("handleSensorSelect", err)
			}
		case "readingSelect":
			err = p.handleReadingSelect(event, &sdpi)
			if err != nil {
				log.Println("handleReadingSelect", err)
			}
		case "min":
			err := p.handleSetMin(event, &sdpi)
			if err != nil {
				log.Println("handleSetMin", err)
			}
		case "max":
			err := p.handleSetMax(event, &sdpi)
			if err != nil {
				log.Println("handleSetMax", err)
			}
		case "autoRange":
			err = p.handleAutoRange(event, &sdpi)
			if err != nil {
				log.Println("handleAutoRange", err)
			}
		case "format":
			err := p.handleSetFormat(event, &sdpi)
			if err != nil {
				log.Println("handleSetFormat", err)
			}
		case "divisor":
			err := p.handleDivisor(event, &sdpi)
			if err != nil {
				log.Println("handleDivisor", err)
			}
		case "graphUnit":
			err := p.handleSetGraphUnit(event, &sdpi)
			if err != nil {
				log.Println("handleSetGraphUnit", err)
			}
		case "snoozeDurations":
			err := p.handleSnoozeDurations(event, &sdpi)
			if err != nil {
				log.Println("handleSnoozeDurations", err)
			}
		case "graphMode":
			settings, getErr := p.am.getSettings(event.Context)
			if getErr != nil {
				log.Println("graphMode getSettings", getErr)
				break
			}
			settings.GraphMode = sdpi.Value
			if err2 := p.sd.SetSettings(event.Context, &settings); err2 != nil {
				log.Println("graphMode SetSettings", err2)
				break
			}
			p.am.SetAction(event.Action, event.Context, &settings)
		case "foreground", "background", "highlight", "valuetext":
			err := p.handleColorChange(event, sdpi.Key, &sdpi)
			if err != nil {
				log.Println("handleColorChange", err)
			}
		case "titleFontSize", "valueFontSize":
			err := p.handleSetFontSize(event, sdpi.Key, &sdpi)
			if err != nil {
				log.Println("handleSetTitleFontSize", err)
			}
		case "graphHeightPct", "graphLineThickness", "textStroke", "textStrokeColor", "updateIntervalOverrideMs", "smoothingAlpha":
			err := p.handleGraphVisuals(event, &sdpi)
			if err != nil {
				log.Println("handleGraphVisuals", err)
			}
		case "warningEnabled":
			err := p.handleWarningEnabled(event, &sdpi)
			if err != nil {
				log.Println("handleWarningEnabled", err)
			}
		case "criticalEnabled":
			err := p.handleCriticalEnabled(event, &sdpi)
			if err != nil {
				log.Println("handleCriticalEnabled", err)
			}
		case "warningValue":
			err := p.handleWarningValue(event, &sdpi)
			if err != nil {
				log.Println("handleWarningValue", err)
			}
		case "criticalValue":
			err := p.handleCriticalValue(event, &sdpi)
			if err != nil {
				log.Println("handleCriticalValue", err)
			}
		case "warningOperator":
			err := p.handleWarningOperator(event, &sdpi)
			if err != nil {
				log.Println("handleWarningOperator", err)
			}
		case "criticalOperator":
			err := p.handleCriticalOperator(event, &sdpi)
			if err != nil {
				log.Println("handleCriticalOperator", err)
			}
		case "warningBackground", "warningForeground", "warningHighlight", "warningValuetext",
			"criticalBackground", "criticalForeground", "criticalHighlight", "criticalValuetext":
			err := p.handleColorChange(event, sdpi.Key, &sdpi)
			if err != nil {
				log.Println("handleColorChange (threshold)", err)
			}
		// Global threshold suppress handlers
		case "suppressGlobal":
			if err := p.handleSuppressGlobal(event, &sdpi); err != nil {
				log.Println("handleSuppressGlobal", err)
			}
		case "unsuppressGlobal":
			if err := p.handleUnsuppressGlobal(event, &sdpi); err != nil {
				log.Println("handleUnsuppressGlobal", err)
			}
		// Dynamic threshold handlers
		case "addThreshold":
			err := p.handleAddThreshold(event, &sdpi)
			if err != nil {
				log.Println("handleAddThreshold", err)
			}
		case "removeThreshold":
			err := p.handleRemoveThreshold(event, &sdpi)
			if err != nil {
				log.Println("handleRemoveThreshold", err)
			}
		case "reorderThreshold":
			err := p.handleReorderThreshold(event, &sdpi)
			if err != nil {
				log.Println("handleReorderThreshold", err)
			}
		case "thresholdEnabled", "thresholdName",
			"thresholdOperator", "thresholdValue", "thresholdHysteresis", "thresholdDwellMs",
			"thresholdCooldownMs", "thresholdSticky", "thresholdText", "thresholdTextColor",
			"thresholdBackgroundColor", "thresholdForegroundColor",
			"thresholdHighlightColor", "thresholdValueTextColor":
			err := p.handleThresholdUpdate(event, &sdpi)
			if err != nil {
				log.Println("handleThresholdUpdate", err)
			}
		default:
			log.Printf("Unknown sdpi key: %s\n", sdpi.Key)
		}
		return
	}
}

// OnDidReceiveSettings applies a Settings key's appearance when its PI saves
// it. Per-tile settings for reading/composite/derived are persisted through
// setSettings and re-read on each OnWillAppear, so they need nothing here.
func (p *Plugin) OnDidReceiveSettings(event *streamdeck.EvDidReceiveSettings) {
	if event.Action == settingsAction {
		p.handleSettingsDidReceiveSettings(event)
	}
}

// OnDidReceiveGlobalSettings handles global settings from Stream Deck
func (p *Plugin) OnDidReceiveGlobalSettings(event *streamdeck.EvDidReceiveGlobalSettings) {
	if event.Payload.Settings == nil {
		return
	}

	var incoming map[string]json.RawMessage
	if err := json.Unmarshal(*event.Payload.Settings, &incoming); err != nil {
		log.Printf("OnDidReceiveGlobalSettings unmarshal failed: %v\n", err)
		return
	}
	if incoming == nil {
		return
	}

	// The host requests global settings once at startup (OnConnected); the
	// PIs never write them. Merge the blob over the stored extras and start
	// from the current in-memory settings, so a key missing from the blob
	// keeps its value instead of resetting to the zero value. Persist only
	// when the blob changed something.
	merged := make(map[string]json.RawMessage, len(incoming))
	p.mu.Lock()
	gs := p.globalSettings
	lastCmdSet := p.lastCommandSet
	for k, v := range p.globalSettingsExtra {
		merged[k] = v
	}
	changed := false
	for k, v := range incoming {
		if existing, ok := merged[k]; !ok || string(existing) != string(v) {
			changed = true
		}
		merged[k] = v
	}
	if v, ok := merged["pollInterval"]; ok {
		_ = json.Unmarshal(v, &gs.PollInterval)
	}
	// Once a setPollInterval command has run it owns AutoRefresh, so the
	// blob's value is ignored; before that (the startup restore) it is trusted.
	if v, ok := merged["autoRefresh"]; ok {
		var autoRefresh bool
		if err := json.Unmarshal(v, &autoRefresh); err == nil {
			if lastCmdSet {
				log.Printf("autoRefresh: dropping stale global-settings value %v (command is authoritative)\n", autoRefresh)
			} else {
				gs.AutoRefresh = &autoRefresh
			}
		}
	}
	if v, ok := merged["globalThresholds"]; ok {
		_ = json.Unmarshal(v, &gs.GlobalThresholds)
	}
	if v, ok := merged["iniPath"]; ok {
		_ = json.Unmarshal(v, &gs.IniPath)
	}
	if v, ok := merged["iniRateKey"]; ok {
		_ = json.Unmarshal(v, &gs.IniRateKey)
	}
	extra := make(map[string]json.RawMessage)
	for k, v := range merged {
		if k == "pollInterval" || k == "autoRefresh" || k == "globalThresholds" || k == "iniPath" || k == "iniRateKey" {
			continue
		}
		extra[k] = v
	}
	p.mu.Unlock()

	if gs.PollInterval <= 0 {
		gs.PollInterval = int(defaultPollInterval.Milliseconds())
	}
	if gs.PollInterval < 250 {
		gs.PollInterval = 250
	}
	if gs.PollInterval > 10000 {
		gs.PollInterval = 10000
	}

	p.mu.Lock()
	intervalChanged := gs.PollInterval != p.globalSettings.PollInterval
	// In auto mode the ticker follows the INI rate, so a stored poll interval
	// must not reset it.
	autoOff := gs.AutoRefresh != nil && !*gs.AutoRefresh
	p.globalSettings = gs
	p.globalSettingsExtra = extra
	if intervalChanged {
		p.pollTimeCacheTTL = pollTimeCacheTTLForInterval(time.Duration(gs.PollInterval) * time.Millisecond)
	}
	p.mu.Unlock()

	if intervalChanged && autoOff {
		interval := time.Duration(gs.PollInterval) * time.Millisecond
		p.am.SetInterval(interval)
	}

	p.sendStatusToPIs()
	if changed {
		p.persistGlobalSettings()
	}
}
