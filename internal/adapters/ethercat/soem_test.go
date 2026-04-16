package ethercat

import (
	"context"
	"testing"

	"watchdog/internal/config"
)

func TestCollectWithSOEMBackend(t *testing.T) {
	oldRunner := runProbeCommand
	t.Cleanup(func() {
		runProbeCommand = oldRunner
	})

	runProbeCommand = func(_ context.Context, argv []string) ([]byte, error) {
		if len(argv) != 3 || argv[0] != "/usr/local/bin/watchdog-soem-probe" || argv[1] != "--master" || argv[2] != "master0" {
			t.Fatalf("unexpected probe command: %#v", argv)
		}
		return []byte(`{
			"interface":"enp3s0",
			"link_known":true,
			"link_up":true,
			"working_counter":118,
			"working_counter_expected":120,
			"slaves":[
				{"position":1,"name":"imu","state":"op"},
				{"position":2,"name":"left_hip","state":"safeop"},
				{"position":3,"name":"right_hip","state":"op"},
				{"position":4,"name":"torso","state":"op"},
				{"position":5,"name":"arm_left","state":"op"},
				{"position":6,"name":"arm_right","state":"op"},
				{"position":7,"name":"gripper_left","state":"op"},
				{"position":8,"name":"gripper_right","state":"op"},
				{"position":9,"name":"ankle_left","state":"op"},
				{"position":10,"name":"ankle_right","state":"op"},
				{"position":11,"name":"camera","state":"op"},
				{"position":12,"name":"lidar","state":"op","lost":true}
			]
		}`), nil
	}

	collector := New(config.EtherCATSourceConfig{
		Enabled: true,
		Backend: "soem",
		Masters: []config.EtherCATMasterConfig{
			{
				Name:           "master0",
				SourceID:       "actuators",
				ExpectedState:  "op",
				ExpectedSlaves: 12,
				RequireLink:    true,
				ProbeCommand:   []string{"/usr/local/bin/watchdog-soem-probe", "--master", "master0"},
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
	if observation.Labels["backend"] != "soem" {
		t.Fatalf("backend = %q, want soem", observation.Labels["backend"])
	}
	if observation.Labels["ethercat.stack"] != "soem" {
		t.Fatalf("ethercat.stack = %q, want soem", observation.Labels["ethercat.stack"])
	}
	if observation.Labels["faulted_slave_positions"] != "2,12" {
		t.Fatalf("faulted_slave_positions = %q, want 2,12", observation.Labels["faulted_slave_positions"])
	}
	if observation.Metrics["ethercat.slaves_seen"] != 12 {
		t.Fatalf("ethercat.slaves_seen = %f, want 12", observation.Metrics["ethercat.slaves_seen"])
	}
	if observation.Metrics["ethercat.slaves_lost"] != 1 {
		t.Fatalf("ethercat.slaves_lost = %f, want 1", observation.Metrics["ethercat.slaves_lost"])
	}
	if observation.Metrics["ethercat.slaves_not_op"] != 1 {
		t.Fatalf("ethercat.slaves_not_op = %f, want 1", observation.Metrics["ethercat.slaves_not_op"])
	}
	if observation.Metrics["ethercat.slave_errors"] != 2 {
		t.Fatalf("ethercat.slave_errors = %f, want 2", observation.Metrics["ethercat.slave_errors"])
	}
	if observation.Labels["master_state"] != "safeop" {
		t.Fatalf("master_state = %q, want safeop", observation.Labels["master_state"])
	}
}
