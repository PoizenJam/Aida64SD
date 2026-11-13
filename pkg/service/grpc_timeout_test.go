package service

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/poizenjam/aida64sd/pkg/service/proto"
)

// deadlineClient fails any call made without a deadline, standing in for a
// wedged bridge that would otherwise block forever.
type deadlineClient struct{ calls int }

func (d *deadlineClient) check(ctx context.Context) error {
	d.calls++
	if _, ok := ctx.Deadline(); !ok {
		return context.Canceled
	}
	return nil
}

func (d *deadlineClient) PollTime(ctx context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*proto.PollTimeReply, error) {
	return &proto.PollTimeReply{}, d.check(ctx)
}
func (d *deadlineClient) Sensors(ctx context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*proto.SensorsReply, error) {
	return &proto.SensorsReply{}, d.check(ctx)
}
func (d *deadlineClient) ReadingsForSensorID(ctx context.Context, _ *proto.SensorIDRequest, _ ...grpc.CallOption) (*proto.ReadingsReply, error) {
	return &proto.ReadingsReply{}, d.check(ctx)
}
func (d *deadlineClient) Sources(ctx context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*proto.SourcesReply, error) {
	return &proto.SourcesReply{}, d.check(ctx)
}

// Every bridge call must carry a deadline, so a wedged bridge returns an
// error instead of hanging the caller (the Stream Deck event goroutine).
func TestGRPCClientCallsHaveDeadline(t *testing.T) {
	d := &deadlineClient{}
	c := &GRPCClient{Client: d}
	if _, err := c.PollTime(); err != nil {
		t.Errorf("PollTime: no deadline")
	}
	if _, err := c.Sensors(); err != nil {
		t.Errorf("Sensors: no deadline")
	}
	if _, err := c.ReadingsForSensorID("temp"); err != nil {
		t.Errorf("ReadingsForSensorID: no deadline")
	}
	if _, err := c.Sources(); err != nil {
		t.Errorf("Sources: no deadline")
	}
	if d.calls != 4 {
		t.Fatalf("calls = %d, want 4", d.calls)
	}
}
