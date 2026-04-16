package can

import (
	"context"
	"testing"
	"time"

	"watchdog/internal/config"
)

func TestCollectBuildsObservation(t *testing.T) {
	collector := New(config.CANSourceConfig{
		Enabled: true,
		Backend: "socketcan",
		Interfaces: []config.CANInterfaceConfig{
			{
				Name:            "can0",
				SourceID:        "drive-can",
				ExpectedBitrate: 1000000,
				RequireUp:       true,
				ExpectedNodes: []config.CANNodeConfig{
					{Name: "left_drive", ID: 1},
					{Name: "right_drive", ID: 2},
				},
			},
		},
	})
	collector.probe = func(context.Context, string, config.CANInterfaceConfig) (InterfaceStatus, error) {
		return InterfaceStatus{
			CollectedAt:  time.Now(),
			LinkUp:       true,
			Bitrate:      1000000,
			OnlineNodes:  2,
			RXErrors:     0,
			TXErrors:     1,
			BusOffCount:  0,
			RestartCount: 1,
			State:        "error-active",
		}, nil
	}

	observations, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("len(observations) = %d, want 1", len(observations))
	}

	observation := observations[0]
	if observation.SourceType != "can" {
		t.Fatalf("source_type = %q, want can", observation.SourceType)
	}
	if observation.Metrics["can.online_nodes"] != 2 {
		t.Fatalf("can.online_nodes = %f, want 2", observation.Metrics["can.online_nodes"])
	}
	if observation.Labels["bus_state"] != "error-active" {
		t.Fatalf("bus_state = %q, want error-active", observation.Labels["bus_state"])
	}
}
