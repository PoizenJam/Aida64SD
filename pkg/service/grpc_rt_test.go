package service

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/poizenjam/aida64sd/pkg/service/proto"
)

// mockHW3 builds the reply with the exact adapter types the bridge uses, so
// this exercises the same path as cmd/aida64-bridge + internal/aida64/plugin.
type mockHW3 struct{}

func (mockHW3) PollTime() (uint64, error) { return 1, nil }
func (mockHW3) Sensors() ([]Sensor, error) {
	return []Sensor{sensor{&proto.Sensor{ID: "temp", Name: "Temperatures", SourceID: ""}}}, nil
}
func (mockHW3) ReadingsForSensorID(id string) ([]Reading, error) {
	if id != "temp" {
		return nil, nil
	}
	return []Reading{reading{&proto.Reading{
		ID: "TCPU", Type: "Temp", TypeI: 1, Label: "CPU", Unit: "C", Value: 55,
	}}}, nil
}
func (mockHW3) Sources() ([]Source, error) {
	return []Source{source{&proto.Source{ID: "", Name: "Local", PollTime: 1, Available: true}}}, nil
}

func TestGRPCRoundTripIDs(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	proto.RegisterAidServiceServer(srv, &GRPCServer{Impl: mockHW3{}})
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	c := &GRPCClient{Client: proto.NewAidServiceClient(conn)}
	sensors, err := c.Sensors()
	if err != nil {
		t.Fatal(err)
	}
	if len(sensors) != 1 {
		t.Fatalf("sensors=%d want 1", len(sensors))
	}
	if got := sensors[0].ID(); got != "temp" {
		t.Fatalf("sensors[0].ID()=%q want %q", got, "temp")
	}
	rs, err := c.ReadingsForSensorID("temp")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 {
		t.Fatalf("readings=%d want 1", len(rs))
	}
	if got := rs[0].ID(); got != "TCPU" {
		t.Fatalf("readings[0].ID()=%q want %q", got, "TCPU")
	}
	t.Logf("round trip OK: sensor=%q reading=%q", sensors[0].ID(), rs[0].ID())
}
