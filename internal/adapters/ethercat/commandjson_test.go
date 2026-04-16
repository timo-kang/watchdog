package ethercat

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

	reportedAt := time.Date(2026, 4, 14, 15, 1, 0, 0, time.UTC)
	runProbeCommand = func(_ context.Context, argv []string) ([]byte, error) {
		if len(argv) != 3 || argv[0] != "/usr/local/bin/ethercat-probe" || argv[1] != "--master" || argv[2] != "master0" {
			t.Fatalf("unexpected probe command: %#v", argv)
		}
		return []byte(`{
			"collected_at":"2026-04-14T15:01:00Z",
			"link_known":true,
			"link_up":true,
			"master_state":"op",
			"slaves_seen":12,
			"slave_errors":0,
			"working_counter":120,
			"working_counter_expected":120,
			"labels":{"probe":"vendor-ethercat"},
			"metrics":{"ethercat.dc_drift_us":4.25}
		}`), nil
	}

	collector := New(config.EtherCATSourceConfig{
		Enabled: true,
		Backend: "command-json",
		Masters: []config.EtherCATMasterConfig{
			{
				Name:           "master0",
				SourceID:       "actuators",
				ExpectedState:  "op",
				ExpectedSlaves: 12,
				RequireLink:    true,
				ProbeCommand:   []string{"/usr/local/bin/ethercat-probe", "--master", "master0"},
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
	if observation.Metrics["ethercat.dc_drift_us"] != 4.25 {
		t.Fatalf("ethercat.dc_drift_us = %f, want 4.25", observation.Metrics["ethercat.dc_drift_us"])
	}
	if observation.Metrics["ethercat.link_known"] != 1 {
		t.Fatalf("ethercat.link_known = %f, want 1", observation.Metrics["ethercat.link_known"])
	}
	if observation.Labels["probe"] != "vendor-ethercat" {
		t.Fatalf("probe label = %q, want vendor-ethercat", observation.Labels["probe"])
	}
	if observation.Labels["backend"] != "command-json" {
		t.Fatalf("backend label = %q, want command-json", observation.Labels["backend"])
	}
}
