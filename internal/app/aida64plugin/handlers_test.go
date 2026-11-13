package aida64plugin

import (
	"image/color"
	"testing"

	"github.com/poizenjam/aida64sd/pkg/graph"
	service "github.com/poizenjam/aida64sd/pkg/service"
)

func TestApplyReadingSettingsUsesBridge(t *testing.T) {
	const (
		sensorUID = "temp"
		readingID = "TCPU"
	)

	p := &Plugin{
		bridge: &bridgeRuntime{
			hw: stubHardwareService{
				readingsBySensor: map[string][]service.Reading{
					sensorUID: {
						stubReading{id: readingID, typ: "Temp", label: "CPU Package", unit: "C", typeI: int32(service.ReadingTypeTemp)},
					},
				},
			},
		},
		graphs: map[string]*graph.Graph{
			"ctx": graph.NewGraph(
				72, 72, 0, 100,
				&color.RGBA{255, 255, 255, 255},
				&color.RGBA{0, 0, 0, 255},
				&color.RGBA{255, 0, 0, 255},
			),
		},
	}

	settings := &actionSettings{
		SensorUID: sensorUID,
		ReadingID: readingID,
	}

	if err := p.applyReadingSettings("ctx", settings); err != nil {
		t.Fatalf("applyReadingSettings() error = %v", err)
	}
	if settings.ReadingLabel != "CPU Package" {
		t.Fatalf("ReadingLabel = %q, want %q", settings.ReadingLabel, "CPU Package")
	}
	if !settings.IsValid {
		t.Fatalf("IsValid = false, want true")
	}
	// Unit-based default scale for temps is 0..100 (Tjmax 95); auto-range grows it live.
	if settings.Min != 0 || settings.Max != 100 {
		t.Fatalf("Min/Max = %d/%d, want 0/100", settings.Min, settings.Max)
	}
}

type stubHardwareService struct {
	readingsBySensor map[string][]service.Reading
	sources          []service.Source
	pollTime         uint64 // value PollTime returns; 0 => 1 (the historical default)
	pollErr          error  // error PollTime returns; nil => no error
	available        *bool  // Sources()[0].Available(); nil => true (default)
	sourcesErr       error  // error Sources returns; nil => no error
}

func (s stubHardwareService) PollTime() (uint64, error) {
	if s.pollErr != nil {
		return 0, s.pollErr
	}
	if s.pollTime != 0 {
		return s.pollTime, nil
	}
	return 1, nil
}

func (s stubHardwareService) Sensors() ([]service.Sensor, error) {
	return nil, nil
}

func (s stubHardwareService) ReadingsForSensorID(id string) ([]service.Reading, error) {
	return s.readingsBySensor[id], nil
}

func (s stubHardwareService) Sources() ([]service.Source, error) {
	if s.sourcesErr != nil {
		return nil, s.sourcesErr
	}
	if s.sources != nil {
		return s.sources, nil
	}
	avail := true
	if s.available != nil {
		avail = *s.available
	}
	return []service.Source{stubSource{id: service.LocalSourceID, pollTime: 1, available: avail}}, nil
}

type stubSource struct {
	id        string
	pollTime  uint64
	available bool
}

func (s stubSource) ID() string       { return s.id }
func (s stubSource) Name() string     { return service.SourceDisplayName(s.id) }
func (s stubSource) PollTime() uint64 { return s.pollTime }
func (s stubSource) Available() bool  { return s.available }
