package service

import (
	"context"

	"google.golang.org/grpc"

	"github.com/hashicorp/go-plugin"

	"github.com/poizenjam/aida64sd/pkg/service/proto"
)

// Handshake is the shared handshake between plugin host and bridge. The magic
// cookie values are Aida64SD-specific; the host and bridge must agree on them.
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "AIDA64_PLUGIN",
	MagicCookieValue: "aida64sd",
}

// PluginKey is the key under which the bridge dispenses the service.
const PluginKey = "aida64plugin"

// PluginMap is the map of plugins we can dispense.
var PluginMap = map[string]plugin.Plugin{
	PluginKey: &Aida64ServicePlugin{},
}

// HardwareService is the interface exposed over gRPC. The plugin (Stream Deck
// host process) is the client; the bridge is the server. All calls are unary:
// the bridge caches the latest snapshot and clients pull from it.
type HardwareService interface {
	// PollTime returns the time (Unix ns) the shared-memory content last
	// changed, or 0 if it has never been read successfully.
	PollTime() (uint64, error)
	// Sensors lists the sensor groups (AIDA64 categories) present in the
	// latest snapshot.
	Sensors() ([]Sensor, error)
	// ReadingsForSensorID returns the readings of one sensor group (category).
	ReadingsForSensorID(id string) ([]Reading, error)
	// Sources lists the data sources; AIDA64 has exactly one, the local machine.
	Sources() ([]Source, error)
}

// ReadingType is the normalized value/unit type of a reading, shared by the
// plugin host (tiles, thresholds, formatting) and the bridge. It mirrors the
// HWiNFO plugin's ReadingType set so the two backends render identically; the
// values are also the bridge proto's TypeI enum.
type ReadingType int

const (
	// ReadingTypeNone no type
	ReadingTypeNone ReadingType = iota
	// ReadingTypeTemp temperature in celsius
	ReadingTypeTemp
	// ReadingTypeVolt voltage
	ReadingTypeVolt
	// ReadingTypeFan RPM
	ReadingTypeFan
	// ReadingTypeCurrent amps
	ReadingTypeCurrent
	// ReadingTypePower watts
	ReadingTypePower
	// ReadingTypeClock MHz
	ReadingTypeClock
	// ReadingTypeUsage percentage or load
	ReadingTypeUsage
	// ReadingTypeOther other
	ReadingTypeOther
)

// String returns the canonical short name ("Temp", "Volt", ...) used in
// settings JSON and the PI's reading-type filters.
func (t ReadingType) String() string {
	return [...]string{"None", "Temp", "Volt", "Fan", "Current", "Power", "Clock", "Usage", "Other"}[t]
}

// Sensor is one sensor group. For AIDA64 the group is a category tag
// (temp/volt/fan/duty/curr/pwr/sys) and its readings are the tags inside it.
type Sensor interface {
	ID() string
	Name() string
	SourceID() string
}

// Source is a machine whose sensors the bridge exposes. AIDA64 is
// local-machine only, so the bridge always reports a single local source.
type Source interface {
	ID() string
	Name() string
	PollTime() uint64
	Available() bool
}

// Reading is one parsed sensor value. AIDA64 has no per-reading unit field
// and no min/max/avg history, so the Reading interface is slimmer than the
// HWiNFO reference: type/unit are derived from the category tag (plus the
// label-% rule) and RawValue preserves what AIDA64 wrote for non-numeric
// values.
type Reading interface {
	ID() string
	Type() string
	TypeI() int32
	Label() string
	Unit() string
	Value() float64
	RawValue() string
}

// Aida64ServicePlugin implements plugin.GRPCPlugin so the service can be
// served and consumed through hashicorp/go-plugin.
type Aida64ServicePlugin struct {
	plugin.Plugin
	Impl HardwareService
}

// GRPCServer constructor.
func (p *Aida64ServicePlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterAidServiceServer(s, &GRPCServer{Impl: p.Impl})
	return nil
}

// GRPCClient constructor.
func (p *Aida64ServicePlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &GRPCClient{Client: proto.NewAidServiceClient(c)}, nil
}

// sensor adapts a proto.Sensor to the Sensor interface.
type sensor struct {
	*proto.Sensor
}

func (s sensor) ID() string       { return s.Sensor.GetID() }
func (s sensor) Name() string     { return s.Sensor.GetName() }
func (s sensor) SourceID() string { return s.Sensor.GetSourceID() }

// source adapts a proto.Source to the Source interface.
type source struct {
	*proto.Source
}

func (s source) ID() string       { return s.Source.GetID() }
func (s source) Name() string     { return s.Source.GetName() }
func (s source) PollTime() uint64 { return s.Source.GetPollTime() }
func (s source) Available() bool  { return s.Source.GetAvailable() }

// reading adapts a proto.Reading to the Reading interface.
type reading struct {
	*proto.Reading
}

func (r reading) ID() string       { return r.Reading.GetID() }
func (r reading) Type() string     { return r.Reading.GetType() }
func (r reading) TypeI() int32     { return r.Reading.GetTypeI() }
func (r reading) Label() string    { return r.Reading.GetLabel() }
func (r reading) Unit() string     { return r.Reading.GetUnit() }
func (r reading) Value() float64   { return r.Reading.GetValue() }
func (r reading) RawValue() string { return r.Reading.GetRawValue() }
