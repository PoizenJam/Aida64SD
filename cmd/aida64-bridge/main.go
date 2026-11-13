//go:build windows

// aida64-bridge is the Aida64SD bridge process. It reads the AIDA64
// AIDA64_SensorValues shared-memory block on a fixed 1 s ticker, caches the
// latest snapshot with a content-change freshness token, and serves the
// HardwareService contract (PollTime/Sensors/ReadingsForSensorID/Sources)
// over hashicorp/go-plugin gRPC for the Stream Deck plugin host to pull.
package main

import (
	"github.com/hashicorp/go-plugin"
	aidplugin "github.com/poizenjam/aida64sd/internal/aida64/plugin"
	"github.com/poizenjam/aida64sd/pkg/service"
)

func main() {
	svc := aidplugin.NewService()
	svc.Start()

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: service.Handshake,
		Plugins: map[string]plugin.Plugin{
			service.PluginKey: &service.Aida64ServicePlugin{
				Impl: &aidplugin.Plugin{Service: svc},
			},
		},
		// A non-nil value here enables gRPC serving for this plugin.
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
