package aida64plugin

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	service "github.com/poizenjam/aida64sd/pkg/service"
)

// sensorCategory returns the picker group for a sensor. In AIDA64 the sensor
// id is already the category tag (temp/volt/fan/duty/curr/pwr/sys), and the
// readings inside it are the tags, so the group is just the tag lowercased.
// Anything unrecognized falls back to "other" so an unexpected tag never
// disappears from the picker.
func sensorCategory(sensorID, _ string) string {
	tag := strings.ToLower(strings.TrimSpace(sensorID))
	switch tag {
	case "temp", "volt", "fan", "duty", "curr", "pwr", "sys":
		return tag
	default:
		return "other"
	}
}

func searchText(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, " ")
}

func sensorPayload(sensorID, sensorName string) *evSendSensorsPayloadSensor {
	category := sensorCategory(sensorID, sensorName)
	return &evSendSensorsPayloadSensor{
		UID:        sensorID,
		Name:       sensorName,
		Category:   category,
		SearchText: searchText(sensorName, category, sensorID, service.LocalSourceName),
		Source:     service.LocalSourceID,
	}
}

func readingPayload(sensorID, sensorName string, reading service.Reading) *evSendReadingsPayloadReading {
	category := sensorCategory(sensorID, sensorName)
	return &evSendReadingsPayloadReading{
		ID:         reading.ID(),
		Label:      reading.Label(),
		Prefix:     reading.Unit(),
		Unit:       reading.Unit(),
		Type:       reading.Type(),
		SensorUID:  sensorID,
		SensorName: sensorName,
		Category:   category,
		SearchText: searchText(sensorName, category, reading.Type(), reading.Label(), reading.Unit(), service.LocalSourceName),
		Source:     service.LocalSourceID,
	}
}

func (p *Plugin) sendCatalogToPropertyInspector(action, context string, settings *actionSettings, sensors []service.Sensor) error {
	rt := p.bridge
	rt.mu.RLock()
	hw := rt.hw
	rt.mu.RUnlock()
	if hw == nil {
		return fmt.Errorf("AIDA64 bridge not ready")
	}

	catalog := p.buildCatalog(hw, sensors)
	payload := evSendCatalogPayload{Catalog: catalog, Settings: settings}
	log.Printf("catalog push %s/%s: sensors=%d readings=%d (PI sensor picker source)\n", action, context, len(sensors), len(catalog.Readings))
	return p.sd.SendToPropertyInspector(action, context, payload)
}

// buildCatalog assembles the picker payload from a bridge snapshot: one
// sensor row per category plus every reading, keyed so the PI can group
// readings by sensorUid. Split out from the push so the grouping (the thing
// the sensor picker renders) is directly testable against a stub bridge.
func (p *Plugin) buildCatalog(hw service.HardwareService, sensors []service.Sensor) *evSendCatalogPayloadCatalog {
	catalog := &evSendCatalogPayloadCatalog{
		Sensors:  make([]*evSendSensorsPayloadSensor, 0, len(sensors)),
		Readings: make([]*evSendReadingsPayloadReading, 0),
	}

	catalog.Sources = p.sourcesPayload()
	cstatus, cinterval, csensors, cauto, cinirefresh := p.piStatusSnapshot()
	catalog.ConnectionStatus = cstatus
	catalog.PollInterval = cinterval
	catalog.SensorCount = csensors
	catalog.AutoRefresh = cauto
	catalog.IniPath = cinirefresh.Path
	catalog.IniPathSet = cinirefresh.PathSet
	catalog.IniRateMs = cinirefresh.RateMs
	catalog.IniRateAppliedMs = cinirefresh.AppliedMs
	catalog.IniStatus = cinirefresh.Status
	catalog.IniRateKey = cinirefresh.Key
	catalog.IniRateKeySet = cinirefresh.KeySet
	catalog.IniNote = cinirefresh.Note
	catalog.IniKeyOptions = cinirefresh.KeyOptions
	catalog.HostVersion = hostVersion

	for _, sensor := range sensors {
		sensorID := sensor.ID()
		sensorName := sensor.Name()
		catalog.Sensors = append(catalog.Sensors, sensorPayload(sensorID, sensorName))

		readings, err := hw.ReadingsForSensorID(sensorID)
		if err != nil {
			log.Printf("buildCatalog ReadingsForSensorID sensor=%s: %v\n", sensorID, err)
			continue
		}

		for _, reading := range readings {
			catalog.Readings = append(catalog.Readings, readingPayload(sensorID, sensorName, reading))
		}
	}

	return catalog
}

// sourcesPayload lists the bridge's sensor sources for the Property
// Inspectors' Source dropdown; nil (dropdown hidden) when unavailable.
func (p *Plugin) sourcesPayload() []evSendCatalogSource {
	rt := p.bridge
	rt.mu.RLock()
	hw := rt.hw
	rt.mu.RUnlock()
	if hw == nil {
		return nil
	}
	sources, err := hw.Sources()
	if err != nil {
		return nil
	}
	out := make([]evSendCatalogSource, 0, len(sources))
	for _, src := range sources {
		out = append(out, evSendCatalogSource{ID: src.ID(), Name: src.Name()})
	}
	return out
}

// persistGlobalSettings sends the full global-settings blob (poll interval,
// INI path/key, thresholds, plus fields owned by other PIs)
// so a partial save from one PI never clobbers another's fields.
func (p *Plugin) persistGlobalSettings() error {
	if p.sd == nil {
		return nil // unit tests exercise the state changes without an SDK connection
	}
	p.mu.RLock()
	gs := p.globalSettings
	extra := make(map[string]json.RawMessage, len(p.globalSettingsExtra)+3)
	for k, v := range p.globalSettingsExtra {
		extra[k] = v
	}
	if raw, err := json.Marshal(gs.PollInterval); err == nil {
		extra["pollInterval"] = raw
	}
	// autoRefresh, iniPath and iniRateKey are always written. They
	// used to be omitted at their default (auto on = nil, "" = auto-detect /
	// default key), so the stored blob only ever recorded the non-default
	// state: whether turning auto back on or clearing an override survived a
	// Stream Deck restart depended on the SDK store replacing rather than
	// merging the blob. Writing them explicitly makes every persist complete.
	autoOn := gs.AutoRefresh == nil || *gs.AutoRefresh
	if raw, err := json.Marshal(autoOn); err == nil {
		extra["autoRefresh"] = raw
	}
	if raw, err := json.Marshal(gs.IniPath); err == nil {
		extra["iniPath"] = raw
	}
	if raw, err := json.Marshal(gs.IniRateKey); err == nil {
		extra["iniRateKey"] = raw
	}
	if gs.GlobalThresholds != nil {
		if raw, err := json.Marshal(gs.GlobalThresholds); err == nil {
			extra["globalThresholds"] = raw
		}
	}
	p.mu.RUnlock()
	return p.sd.SetGlobalSettings(extra)
}
