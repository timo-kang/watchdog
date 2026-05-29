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
				Slaves: []config.EtherCATSlaveConfig{
					{Position: 2, Name: "left_hip", Criticality: "critical", ExpectedState: "op"},
					{Position: 12, Name: "diagnostic_io", Criticality: "optional", ExpectedState: "op"},
				},
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
			Slaves: []SlaveStatus{
				{Position: 2, Name: "left_hip", State: "safeop"},
				{Position: 12, Name: "diagnostic_io", State: "op", Lost: true},
			},
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
	if observation.Metrics["ethercat.criticality_known"] != 1 {
		t.Fatalf("ethercat.criticality_known = %f, want 1", observation.Metrics["ethercat.criticality_known"])
	}
	if observation.Metrics["ethercat.critical_slaves_not_op"] != 1 {
		t.Fatalf("ethercat.critical_slaves_not_op = %f, want 1", observation.Metrics["ethercat.critical_slaves_not_op"])
	}
	if observation.Metrics["ethercat.optional_slaves_lost"] != 1 {
		t.Fatalf("ethercat.optional_slaves_lost = %f, want 1", observation.Metrics["ethercat.optional_slaves_lost"])
	}
	if observation.Labels["faulted_critical_slave_positions"] != "2" {
		t.Fatalf("faulted_critical_slave_positions = %q, want 2", observation.Labels["faulted_critical_slave_positions"])
	}
	if observation.Labels["faulted_optional_slave_positions"] != "12" {
		t.Fatalf("faulted_optional_slave_positions = %q, want 12", observation.Labels["faulted_optional_slave_positions"])
	}
}

func TestCollectKeepsAggregateOnlyEtherCATBackwardCompatible(t *testing.T) {
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
			LinkKnown:              true,
			LinkUp:                 true,
			MasterState:            "op",
			SlavesSeen:             12,
			WorkingCounter:         120,
			WorkingCounterExpected: 120,
			Slaves: []SlaveStatus{
				{Position: 12, Name: "lidar", State: "op", Lost: true},
			},
			AdditionalMetrics: map[string]float64{
				"ethercat.slaves_lost": 1,
			},
		}, nil
	}

	observations, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	observation := observations[0]
	if observation.Metrics["ethercat.slaves_lost"] != 1 {
		t.Fatalf("ethercat.slaves_lost = %f, want 1", observation.Metrics["ethercat.slaves_lost"])
	}
	if observation.Metrics["ethercat.criticality_known"] != 0 {
		t.Fatalf("ethercat.criticality_known = %f, want absent/0", observation.Metrics["ethercat.criticality_known"])
	}
}
