package rules

import (
	"testing"
	"time"

	"watchdog/internal/config"
	"watchdog/internal/health"
)

func TestEvaluateModuleUsesReportedSeverity(t *testing.T) {
	evaluator := New(config.RulesConfig{})
	observation := health.Observation{
		SourceID:         "planner",
		SourceType:       "module",
		CollectedAt:      time.Now(),
		ReportedSeverity: health.SeverityWarn,
		ReportedReason:   "deadline overrun",
		StaleAfter:       2 * time.Second,
	}

	status := evaluator.Evaluate(observation)
	if status.Severity != health.SeverityWarn {
		t.Fatalf("severity = %s, want %s", status.Severity, health.SeverityWarn)
	}
	if status.Reason != "deadline overrun" {
		t.Fatalf("reason = %q, want deadline overrun", status.Reason)
	}
}

func TestEvaluateModuleMarksStale(t *testing.T) {
	evaluator := New(config.RulesConfig{})
	observation := health.Observation{
		SourceID:         "localization",
		SourceType:       "module",
		CollectedAt:      time.Now().Add(-3 * time.Second),
		ReportedSeverity: health.SeverityFail,
		ReportedReason:   "tracking lost",
		StaleAfter:       500 * time.Millisecond,
	}

	status := evaluator.Evaluate(observation)
	if status.Severity != health.SeverityStale {
		t.Fatalf("severity = %s, want %s", status.Severity, health.SeverityStale)
	}
	if status.Reason == "" {
		t.Fatal("expected stale reason")
	}
	if got := status.Metrics["age.s"]; got <= 0 {
		t.Fatalf("age.s = %f, want > 0", got)
	}
}

func TestEvaluateProcessWarnsOnRestarts(t *testing.T) {
	evaluator := New(config.RulesConfig{
		Process: config.ProcessRules{
			RestartWarn: 1,
			RestartFail: 3,
		},
	})
	observation := health.Observation{
		SourceID:    "planner",
		SourceType:  "process",
		CollectedAt: time.Now(),
		Metrics: map[string]float64{
			"process.main_pid":         4242,
			"process.restarts":         2,
			"process.require_main_pid": 1,
		},
		Labels: map[string]string{
			"load_state":   "loaded",
			"active_state": "active",
			"sub_state":    "running",
		},
	}

	status := evaluator.Evaluate(observation)
	if status.Severity != health.SeverityWarn {
		t.Fatalf("severity = %s, want %s", status.Severity, health.SeverityWarn)
	}
	if status.Reason == "" {
		t.Fatal("expected reason")
	}
}

func TestEvaluateProcessFailsWhenInactive(t *testing.T) {
	evaluator := New(config.RulesConfig{
		Process: config.ProcessRules{
			RestartWarn: 1,
			RestartFail: 3,
		},
	})
	observation := health.Observation{
		SourceID:    "planner",
		SourceType:  "process",
		CollectedAt: time.Now(),
		Metrics: map[string]float64{
			"process.main_pid":         0,
			"process.restarts":         0,
			"process.require_main_pid": 1,
		},
		Labels: map[string]string{
			"load_state":   "loaded",
			"active_state": "failed",
			"sub_state":    "failed",
		},
	}

	status := evaluator.Evaluate(observation)
	if status.Severity != health.SeverityFail {
		t.Fatalf("severity = %s, want %s", status.Severity, health.SeverityFail)
	}
	if status.Reason == "" {
		t.Fatal("expected reason")
	}
}

func TestEvaluateCANFailsOnBusOff(t *testing.T) {
	evaluator := New(config.RulesConfig{
		CAN: config.CANRules{
			MissingNodesWarn: 1,
			MissingNodesFail: 2,
			RestartWarn:      1,
			RestartFail:      3,
		},
	})
	observation := health.Observation{
		SourceID:    "drive-can",
		SourceType:  "can",
		CollectedAt: time.Now(),
		Metrics: map[string]float64{
			"can.link_up":          1,
			"can.require_up":       1,
			"can.bitrate":          1000000,
			"can.expected_bitrate": 1000000,
			"can.online_nodes":     2,
			"can.expected_nodes":   2,
			"can.bus_off_count":    1,
		},
	}

	status := evaluator.Evaluate(observation)
	if status.Severity != health.SeverityFail {
		t.Fatalf("severity = %s, want %s", status.Severity, health.SeverityFail)
	}
}

func TestEvaluateEtherCATWarnsOnSafeOp(t *testing.T) {
	evaluator := New(config.RulesConfig{
		EtherCAT: config.EtherCATRules{
			MissingSlavesWarn: 1,
			MissingSlavesFail: 2,
			WKCWarnRatio:      0.95,
			WKCFailRatio:      0.80,
		},
	})
	observation := health.Observation{
		SourceID:    "actuators",
		SourceType:  "ethercat",
		CollectedAt: time.Now(),
		Metrics: map[string]float64{
			"ethercat.link_up":              1,
			"ethercat.link_known":           1,
			"ethercat.require_link":         1,
			"ethercat.slaves_seen":          11,
			"ethercat.expected_slaves":      12,
			"ethercat.working_counter":      118,
			"ethercat.working_counter_goal": 120,
		},
		Labels: map[string]string{
			"master_state":   "safeop",
			"expected_state": "op",
		},
	}

	status := evaluator.Evaluate(observation)
	if status.Severity != health.SeverityWarn {
		t.Fatalf("severity = %s, want %s", status.Severity, health.SeverityWarn)
	}
	if status.Metrics["ethercat.wkc_ratio"] == 0 {
		t.Fatal("expected ethercat.wkc_ratio to be set")
	}
}

func TestEvaluateEtherCATDoesNotFailUnknownLink(t *testing.T) {
	evaluator := New(config.RulesConfig{
		EtherCAT: config.EtherCATRules{
			MissingSlavesWarn: 1,
			MissingSlavesFail: 2,
			WKCWarnRatio:      0.95,
			WKCFailRatio:      0.80,
		},
	})
	observation := health.Observation{
		SourceID:    "actuators",
		SourceType:  "ethercat",
		CollectedAt: time.Now(),
		Metrics: map[string]float64{
			"ethercat.link_known":           0,
			"ethercat.link_up":              0,
			"ethercat.require_link":         1,
			"ethercat.slaves_seen":          12,
			"ethercat.expected_slaves":      12,
			"ethercat.working_counter":      0,
			"ethercat.working_counter_goal": 0,
		},
		Labels: map[string]string{
			"master_state":   "op",
			"expected_state": "op",
		},
	}

	status := evaluator.Evaluate(observation)
	if status.Severity != health.SeverityOK {
		t.Fatalf("severity = %s, want %s", status.Severity, health.SeverityOK)
	}
}

func TestEvaluateEtherCATFailsOnLostSlave(t *testing.T) {
	evaluator := New(config.RulesConfig{
		EtherCAT: config.EtherCATRules{
			MissingSlavesWarn: 1,
			MissingSlavesFail: 2,
			WKCWarnRatio:      0.95,
			WKCFailRatio:      0.80,
		},
	})
	observation := health.Observation{
		SourceID:    "actuators",
		SourceType:  "ethercat",
		CollectedAt: time.Now(),
		Metrics: map[string]float64{
			"ethercat.link_known":           1,
			"ethercat.link_up":              1,
			"ethercat.require_link":         1,
			"ethercat.slaves_seen":          12,
			"ethercat.expected_slaves":      12,
			"ethercat.slaves_lost":          1,
			"ethercat.working_counter":      120,
			"ethercat.working_counter_goal": 120,
		},
		Labels: map[string]string{
			"master_state":   "op",
			"expected_state": "op",
		},
	}

	status := evaluator.Evaluate(observation)
	if status.Severity != health.SeverityFail {
		t.Fatalf("severity = %s, want %s", status.Severity, health.SeverityFail)
	}
}

func TestEvaluateEtherCATWarnsOnNonOperationalSlave(t *testing.T) {
	evaluator := New(config.RulesConfig{
		EtherCAT: config.EtherCATRules{
			MissingSlavesWarn: 1,
			MissingSlavesFail: 2,
			WKCWarnRatio:      0.95,
			WKCFailRatio:      0.80,
		},
	})
	observation := health.Observation{
		SourceID:    "actuators",
		SourceType:  "ethercat",
		CollectedAt: time.Now(),
		Metrics: map[string]float64{
			"ethercat.link_known":           1,
			"ethercat.link_up":              1,
			"ethercat.require_link":         1,
			"ethercat.slaves_seen":          12,
			"ethercat.expected_slaves":      12,
			"ethercat.slaves_not_op":        1,
			"ethercat.working_counter":      120,
			"ethercat.working_counter_goal": 120,
		},
		Labels: map[string]string{
			"master_state":   "op",
			"expected_state": "op",
		},
	}

	status := evaluator.Evaluate(observation)
	if status.Severity != health.SeverityWarn {
		t.Fatalf("severity = %s, want %s", status.Severity, health.SeverityWarn)
	}
}

func TestEvaluateCANDoesNotInferMissingNodesWhenUnknown(t *testing.T) {
	evaluator := New(config.RulesConfig{
		CAN: config.CANRules{
			MissingNodesWarn: 1,
			MissingNodesFail: 2,
			RestartWarn:      1,
			RestartFail:      3,
		},
	})
	observation := health.Observation{
		SourceID:    "drive-can",
		SourceType:  "can",
		CollectedAt: time.Now(),
		Metrics: map[string]float64{
			"can.link_up":            1,
			"can.require_up":         1,
			"can.expected_bitrate":   1000000,
			"can.bitrate":            1000000,
			"can.expected_nodes":     4,
			"can.online_nodes":       0,
			"can.online_nodes_known": 0,
		},
	}

	status := evaluator.Evaluate(observation)
	if status.Severity != health.SeverityOK {
		t.Fatalf("severity = %s, want %s", status.Severity, health.SeverityOK)
	}
}
