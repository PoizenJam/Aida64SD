package plugin

import (
	"github.com/poizenjam/aida64sd/internal/aida64"
	"github.com/poizenjam/aida64sd/pkg/service"
)

// Plugin implements the HardwareService contract over the Service cache. It
// is the AIDA64-specific core of the bridge process.
type Plugin struct {
	Service *Service
}

// PollTime implementation for plugin; the content-change token in Unix ns.
func (p *Plugin) PollTime() (uint64, error) {
	return p.Service.PollTime()
}

// Sensors implementation for plugin; the sensor groups are the AIDA64
// categories present in the latest snapshot. Enumeration is fully dynamic:
// whatever top-level tags the block carries each tick is what is reported,
// there is no hardcoded sensor list.
func (p *Plugin) Sensors() ([]service.Sensor, error) {
	cats := p.Service.Categories()
	sensors := make([]service.Sensor, 0, len(cats))
	for _, c := range cats {
		sensors = append(sensors, sensor{
			id:       c.ID,
			name:     c.Name,
			sourceID: service.LocalSourceID,
		})
	}
	return sensors, nil
}

// ReadingsForSensorID implementation for plugin; the sensor id is the
// category tag (temp/volt/fan/duty/curr/pwr/sys).
func (p *Plugin) ReadingsForSensorID(id string) ([]service.Reading, error) {
	readings, err := p.Service.ReadingsForCategory(id)
	if err != nil {
		return nil, err
	}
	out := make([]service.Reading, 0, len(readings))
	for _, r := range readings {
		out = append(out, reading{r})
	}
	return out, nil
}

// Sources implementation for plugin; AIDA64 has exactly one source, local.
func (p *Plugin) Sources() ([]service.Source, error) {
	pollTime, _ := p.Service.PollTime()
	return []service.Source{source{
		id:        service.LocalSourceID,
		pollTime:  pollTime,
		available: p.Service.Available(),
	}}, nil
}

type source struct {
	id        string
	pollTime  uint64
	available bool
}

func (s source) ID() string       { return s.id }
func (s source) Name() string     { return service.SourceDisplayName(s.id) }
func (s source) PollTime() uint64 { return s.pollTime }
func (s source) Available() bool  { return s.available }

type sensor struct {
	id       string
	name     string
	sourceID string
}

func (s sensor) ID() string       { return s.id }
func (s sensor) Name() string     { return s.name }
func (s sensor) SourceID() string { return s.sourceID }

// reading adapts an aida64.Reading to the service interface. Unit comes from
// the normalized type because AIDA64 has no per-reading unit field.
type reading struct {
	aida64.Reading
}

func (r reading) ID() string       { return r.Reading.ID }
func (r reading) Type() string     { return r.Reading.Type.String() }
func (r reading) TypeI() int32     { return int32(r.Reading.Type) }
func (r reading) Label() string    { return r.Reading.Label }
func (r reading) Unit() string     { return r.Reading.Unit() }
func (r reading) Value() float64   { return r.Reading.Value }
func (r reading) RawValue() string { return r.Reading.RawValue }
