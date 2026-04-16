package ethercat

import (
	"context"
	"testing"
	"time"

	"watchdog/internal/config"
)

func TestCollectBuildsObservation(t *testing.T) {
	collector := New(config.EtherCATSourceConfig{
		Enabled: true,
		Backend: "igh",
		Masters: []config.EtherCATMasterConfig{
			{
				Name:           "master0",
				SourceID:       "actuators",
				ExpectedState:  "op",
				ExpectedSlaves: 12,
				RequireLink:    true,
			},
		},
	})
	collector.probe = func(context.Context, string, config.EtherCATMasterConfig) (MasterStatus, error) {
		return MasterStatus{
			CollectedAt:            time.Now(),
			LinkUp:                 true,
			MasterState:            "op",
			SlavesSeen:             12,
			WorkingCounter:         120,
			WorkingCounterExpected: 120,
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
	if observation.SourceType != "ethercat" {
		t.Fatalf("source_type = %q, want ethercat", observation.SourceType)
	}
	if observation.Metrics["ethercat.expected_slaves"] != 12 {
		t.Fatalf("ethercat.expected_slaves = %f, want 12", observation.Metrics["ethercat.expected_slaves"])
	}
	if observation.Labels["master_state"] != "op" {
		t.Fatalf("master_state = %q, want op", observation.Labels["master_state"])
	}
}
