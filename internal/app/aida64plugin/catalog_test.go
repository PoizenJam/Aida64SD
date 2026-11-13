package aida64plugin

import (
	"testing"

	service "github.com/poizenjam/aida64sd/pkg/service"
)

func TestSensorCategory(t *testing.T) {
	// In AIDA64 the sensor id is the category tag itself, so the picker group
	// is the tag (the sensor name never changes it).
	tests := []struct {
		name     string
		sensorID string
		sensor   string
		want     string
	}{
		{name: "temp", sensorID: "temp", sensor: "Temperatures", want: "temp"},
		{name: "volt", sensorID: "volt", sensor: "Voltages", want: "volt"},
		{name: "fan", sensorID: "fan", sensor: "Fan Speeds", want: "fan"},
		{name: "duty", sensorID: "duty", sensor: "Fan Duty", want: "duty"},
		{name: "curr", sensorID: "curr", sensor: "Currents", want: "curr"},
		{name: "pwr", sensorID: "pwr", sensor: "Power", want: "pwr"},
		{name: "sys", sensorID: "sys", sensor: "System", want: "sys"},
		{name: "unknown tag falls back to other", sensorID: "mystery", sensor: "X", want: "other"},
	}

	for _, tc := range tests {
		if got := sensorCategory(tc.sensorID, tc.sensor); got != tc.want {
			t.Fatalf("%s: expected %q, got %q", tc.name, tc.want, got)
		}
	}
}

func TestReadingPayloadIncludesSearchMetadata(t *testing.T) {
	reading := stubReading{
		id:    "TCPU",
		typ:   "Temp",
		label: "CPU Package",
		unit:  "C",
	}

	payload := readingPayload("temp", "Temperatures", reading)
	if payload.ID != "TCPU" {
		t.Fatalf("expected reading id TCPU, got %q", payload.ID)
	}
	if payload.SensorUID != "temp" {
		t.Fatalf("expected sensor UID to be copied, got %q", payload.SensorUID)
	}
	if payload.Source != service.LocalSourceID {
		t.Fatalf("expected local source %q, got %q", service.LocalSourceID, payload.Source)
	}
	if payload.SearchText == "" {
		t.Fatalf("expected search text to be populated")
	}
}

// stubReading implements service.Reading for the host unit tests. It mirrors
// the AIDA64 model: string id, TypeI() int32, no min/max/avg.
type stubReading struct {
	id    string
	typ   string
	label string
	unit  string
	value float64
	typeI int32
}

func (r stubReading) ID() string       { return r.id }
func (r stubReading) TypeI() int32     { return r.typeI }
func (r stubReading) Type() string     { return r.typ }
func (r stubReading) Label() string    { return r.label }
func (r stubReading) Unit() string     { return r.unit }
func (r stubReading) Value() float64   { return r.value }
func (r stubReading) RawValue() string { return "" }

// stubSensor implements service.Sensor for the host unit tests.
type stubSensor struct {
	sid  string
	name string
}

func (s stubSensor) ID() string       { return s.sid }
func (s stubSensor) Name() string     { return s.name }
func (s stubSensor) SourceID() string { return service.LocalSourceID }

// TestBuildCatalogGroupsReadingsBySensor exercises the actual catalog-building
// loop (Sensors + ReadingsForSensorID) and asserts the readings carry the
// sensorUid the sensor PI's combined picker groups on. If this returns an
// empty Readings slice, the sensor picker renders blank while the per-slot
// composite/derived pickers (a different host path) still work.
func TestBuildCatalogGroupsReadingsBySensor(t *testing.T) {
	stub := stubHardwareService{
		readingsBySensor: map[string][]service.Reading{
			"temp": {stubReading{id: "TCPU", typ: "Temp", label: "CPU Package", unit: "C", typeI: int32(service.ReadingTypeTemp)}},
			"fan":  {stubReading{id: "FCPU", typ: "Fan", label: "CPU Fan", unit: "RPM", typeI: int32(service.ReadingTypeFan)}},
		},
	}
	sensors := []service.Sensor{
		stubSensor{sid: "temp", name: "Temperatures"},
		stubSensor{sid: "fan", name: "Fans"},
	}
	p := &Plugin{
		bridge: &bridgeRuntime{hw: stub},
		am:     newActionManager(defaultPollInterval),
	}

	catalog := p.buildCatalog(stub, sensors)
	if len(catalog.Sensors) != 2 {
		t.Fatalf("sensors = %d, want 2", len(catalog.Sensors))
	}
	if len(catalog.Readings) != 2 {
		t.Fatalf("readings = %d, want 2 (an empty Readings slice is the blank sensor picker)", len(catalog.Readings))
	}
	seen := map[string]bool{}
	for _, r := range catalog.Readings {
		if r.SensorUID == "" {
			t.Fatalf("reading %q has empty SensorUID; readingsBySensorMap drops it", r.ID)
		}
		seen[r.SensorUID] = true
	}
	if !seen["temp"] || !seen["fan"] {
		t.Fatalf("expected temp and fan groups, got %v", seen)
	}
}

// TestIniStatusFieldsReported pins the INI rate state in piStatusSnapshot:
// the resolved key, the pre-clamp INI value (ms, not ns), the path used, and
// the selector list, as the PI renders them.
func TestIniStatusFieldsReported(t *testing.T) {
	p := &Plugin{
		bridge: &bridgeRuntime{},
		am:     newActionManager(defaultPollInterval),
	}
	p.iniUsedPath = `C:\Program Files (x86)\FinalWire\AIDA64 Extreme Edition\AIDA64.INI`
	p.iniRateMs = 1000
	p.iniRateKey = iniRateKeyDefault
	p.iniStatus = iniStatusOK
	p.iniNote = "default key"
	p.globalSettings.IniRateKey = "HWMonUpdateFreqSensorPage"

	_, _, _, _, ini := p.piStatusSnapshot()
	if ini.RateMs != 1000 {
		t.Fatalf("iniRateMs = %d, want 1000 (ms, not ns)", ini.RateMs)
	}
	if ini.Key != iniRateKeyDefault {
		t.Fatalf("iniRateKey = %q, want %q", ini.Key, iniRateKeyDefault)
	}
	if ini.KeySet != "HWMonUpdateFreqSensorPage" {
		t.Fatalf("iniRateKeySet = %q, want the configured override", ini.KeySet)
	}
	if ini.Path == "" {
		t.Fatal("iniPath empty, want the path used")
	}
	if ini.Status != iniStatusOK {
		t.Fatalf("iniStatus = %q, want ok", ini.Status)
	}
	if len(ini.KeyOptions) != 11 {
		t.Fatalf("iniKeyOptions = %d entries, want 11", len(ini.KeyOptions))
	}
}
