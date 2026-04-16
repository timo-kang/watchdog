package can

import (
	"context"
	"testing"
	"time"

	"watchdog/internal/config"
)

func TestCollectWithCommandJSONBackend(t *testing.T) {
	oldRunner := runProbeCommand
	t.Cleanup(func() {
		runProbeCommand = oldRunner
	})

	reportedAt := time.Date(2026, 4, 14, 15, 0, 0, 0, time.UTC)
	runProbeCommand = func(_ context.Context, argv []string) ([]byte, error) {
		if len(argv) != 3 || argv[0] != "/usr/local/bin/can-probe" || argv[1] != "--iface" || argv[2] != "can0" {
			t.Fatalf("unexpected probe command: %#v", argv)
		}
		return []byte(`{
			"collected_at":"2026-04-14T15:00:00Z",
			"link_up":true,
			"bitrate":1000000,
			"online_nodes":2,
			"online_nodes_known":true,
			"rx_errors":0,
			"tx_errors":1,
			"bus_off_count":0,
			"restart_count":2,
			"state":"error-active",
			"labels":{"probe":"vendor-can"},
			"metrics":{"can.vendor_heartbeat_gap_ms":12.5}
		}`), nil
	}

	collector := New(config.CANSourceConfig{
		Enabled: true,
		Backend: "command-json",
		Interfaces: []config.CANInterfaceConfig{
			{
				Name:            "can0",
				SourceID:        "drive-can",
				ExpectedBitrate: 1000000,
				RequireUp:       true,
				ProbeCommand:    []string{"/usr/local/bin/can-probe", "--iface", "can0"},
				ExpectedNodes: []config.CANNodeConfig{
					{Name: "left_drive", ID: 1},
					{Name: "right_drive", ID: 2},
				},
			},
		},
	})

	observations, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("len(observations) = %d, want 1", len(observations))
	}

	observation := observations[0]
	if !observation.CollectedAt.Equal(reportedAt) {
		t.Fatalf("collected_at = %s, want %s", observation.CollectedAt, reportedAt)
	}
	if observation.Metrics["can.online_nodes_known"] != 1 {
		t.Fatalf("can.online_nodes_known = %f, want 1", observation.Metrics["can.online_nodes_known"])
	}
	if observation.Metrics["can.vendor_heartbeat_gap_ms"] != 12.5 {
		t.Fatalf("can.vendor_heartbeat_gap_ms = %f, want 12.5", observation.Metrics["can.vendor_heartbeat_gap_ms"])
	}
	if observation.Labels["probe"] != "vendor-can" {
		t.Fatalf("probe label = %q, want vendor-can", observation.Labels["probe"])
	}
	if observation.Labels["backend"] != "command-json" {
		t.Fatalf("backend label = %q, want command-json", observation.Labels["backend"])
	}
}
