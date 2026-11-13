package service

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/poizenjam/aida64sd/pkg/service/proto"
)

// GRPCClient implements HardwareService over a gRPC connection to the bridge.
type GRPCClient struct {
	Client proto.AidServiceClient
}

// callTimeout bounds every bridge call. The bridge answers from an in-memory
// snapshot in microseconds, so hitting this means it is wedged. Without a
// deadline a wedged bridge hung the caller forever, and when the caller was
// the Stream Deck event goroutine the whole plugin froze; with it the caller
// gets an error and the normal unavailable/restart paths take over.
const callTimeout = 2 * time.Second

func callContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), callTimeout)
}

// PollTime rpc call.
func (c *GRPCClient) PollTime() (uint64, error) {
	ctx, cancel := callContext()
	defer cancel()
	resp, err := c.Client.PollTime(ctx, &emptypb.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.GetPollTime(), nil
}

// Sensors rpc call.
func (c *GRPCClient) Sensors() ([]Sensor, error) {
	ctx, cancel := callContext()
	defer cancel()
	resp, err := c.Client.Sensors(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	sensors := make([]Sensor, 0, len(resp.GetSensors()))
	for _, s := range resp.GetSensors() {
		sensors = append(sensors, sensor{s})
	}
	return sensors, nil
}

// ReadingsForSensorID rpc call.
func (c *GRPCClient) ReadingsForSensorID(id string) ([]Reading, error) {
	ctx, cancel := callContext()
	defer cancel()
	resp, err := c.Client.ReadingsForSensorID(ctx, &proto.SensorIDRequest{Id: id})
	if err != nil {
		return nil, err
	}

	readings := make([]Reading, 0, len(resp.GetReadings()))
	for _, r := range resp.GetReadings() {
		readings = append(readings, reading{r})
	}
	return readings, nil
}

// Sources rpc call.
func (c *GRPCClient) Sources() ([]Source, error) {
	ctx, cancel := callContext()
	defer cancel()
	resp, err := c.Client.Sources(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	sources := make([]Source, 0, len(resp.GetSources()))
	for _, s := range resp.GetSources() {
		sources = append(sources, source{s})
	}
	return sources, nil
}

// GRPCServer serves HardwareService over gRPC; Impl is the concrete
// implementation (the bridge's plugin.Plugin).
type GRPCServer struct {
	Impl HardwareService
	proto.UnimplementedAidServiceServer
}

// PollTime gRPC wrapper.
func (s *GRPCServer) PollTime(ctx context.Context, _ *emptypb.Empty) (*proto.PollTimeReply, error) {
	v, err := s.Impl.PollTime()
	return &proto.PollTimeReply{PollTime: v}, err
}

// Sensors gRPC wrapper.
func (s *GRPCServer) Sensors(ctx context.Context, _ *emptypb.Empty) (*proto.SensorsReply, error) {
	sensors, err := s.Impl.Sensors()
	if err != nil {
		return nil, err
	}
	out := &proto.SensorsReply{}
	for _, sensor := range sensors {
		out.Sensors = append(out.Sensors, &proto.Sensor{
			ID:       sensor.ID(),
			Name:     sensor.Name(),
			SourceID: sensor.SourceID(),
		})
	}
	return out, nil
}

// Sources gRPC wrapper.
func (s *GRPCServer) Sources(ctx context.Context, _ *emptypb.Empty) (*proto.SourcesReply, error) {
	sources, err := s.Impl.Sources()
	if err != nil {
		return nil, err
	}
	out := &proto.SourcesReply{}
	for _, src := range sources {
		out.Sources = append(out.Sources, &proto.Source{
			ID:        src.ID(),
			Name:      src.Name(),
			PollTime:  src.PollTime(),
			Available: src.Available(),
		})
	}
	return out, nil
}

// ReadingsForSensorID gRPC wrapper.
func (s *GRPCServer) ReadingsForSensorID(ctx context.Context, req *proto.SensorIDRequest) (*proto.ReadingsReply, error) {
	readings, err := s.Impl.ReadingsForSensorID(req.GetId())
	if err != nil {
		return nil, err
	}
	out := &proto.ReadingsReply{}
	for _, reading := range readings {
		out.Readings = append(out.Readings, &proto.Reading{
			ID:       reading.ID(),
			Type:     reading.Type(),
			TypeI:    reading.TypeI(),
			Label:    reading.Label(),
			Unit:     reading.Unit(),
			Value:    reading.Value(),
			RawValue: reading.RawValue(),
		})
	}
	return out, nil
}
