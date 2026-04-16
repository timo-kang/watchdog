package supervisor

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"watchdog/internal/actions"
	"watchdog/internal/health"
)

func TestManagerLatchesEscalationsUntilResolve(t *testing.T) {
	manager, err := LoadManager(filepath.Join(t.TempDir(), "current_state.json"), CooldownConfig{})
	if err != nil {
		t.Fatalf("LoadManager: %v", err)
	}

	firstAt := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	result, err := manager.Apply(actions.Request{
		SchemaVersion:   1,
		RequestID:       "req-1",
		Event:           actions.EventTransition,
		Timestamp:       firstAt,
		Hostname:        "robot-1",
		Overall:         health.SeverityFail,
		RequestedAction: actions.ActionDegrade,
		Components: []actions.ComponentRequest{
			{
				ComponentID:     "actuators",
				Severity:        health.SeverityFail,
				RequestedAction: actions.ActionDegrade,
				Reason:          "slave 12 not operational",
				SourceTypes:     []string{"ethercat"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Apply first transition: %v", err)
	}
	if !result.ShouldExecuteHook || result.HookAction != actions.ActionDegrade {
		t.Fatalf("first hook = %+v, want degrade hook execution", result)
	}

	secondAt := firstAt.Add(2 * time.Second)
	result, err = manager.Apply(actions.Request{
		SchemaVersion:   1,
		RequestID:       "req-2",
		Event:           actions.EventTransition,
		Timestamp:       secondAt,
		Hostname:        "robot-1",
		Overall:         health.SeverityFail,
		RequestedAction: actions.ActionSafeStop,
		Components: []actions.ComponentRequest{
			{
				ComponentID:     "actuators",
				Severity:        health.SeverityFail,
				RequestedAction: actions.ActionSafeStop,
				Reason:          "slave 12 lost",
				SourceTypes:     []string{"ethercat"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Apply escalation: %v", err)
	}
	if !result.ShouldExecuteHook || result.HookAction != actions.ActionSafeStop {
		t.Fatalf("escalation hook = %+v, want safe_stop hook execution", result)
	}
	if got := result.State.ActiveComponents[0].ActiveAction; got != actions.ActionSafeStop {
		t.Fatalf("active action = %s, want safe_stop", got)
	}

	thirdAt := secondAt.Add(2 * time.Second)
	result, err = manager.Apply(actions.Request{
		SchemaVersion:   1,
		RequestID:       "req-3",
		Event:           actions.EventTransition,
		Timestamp:       thirdAt,
		Hostname:        "robot-1",
		Overall:         health.SeverityFail,
		RequestedAction: actions.ActionDegrade,
		Components: []actions.ComponentRequest{
			{
				ComponentID:     "actuators",
				Severity:        health.SeverityWarn,
				RequestedAction: actions.ActionDegrade,
				Reason:          "state improved but not healthy",
				SourceTypes:     []string{"ethercat"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Apply downgraded transition: %v", err)
	}
	if result.ShouldExecuteHook {
		t.Fatalf("downgraded request unexpectedly executed hook: %+v", result)
	}
	if !strings.Contains(result.SuppressionReason, "latched higher action") {
		t.Fatalf("suppression = %q, want latched-higher-action reason", result.SuppressionReason)
	}
	if got := result.State.ActiveComponents[0].ActiveAction; got != actions.ActionSafeStop {
		t.Fatalf("active action after downgrade = %s, want safe_stop", got)
	}

	resolveAt := thirdAt.Add(2 * time.Second)
	result, err = manager.Apply(actions.Request{
		SchemaVersion:   1,
		RequestID:       "req-4",
		Event:           actions.EventResolved,
		Timestamp:       resolveAt,
		Hostname:        "robot-1",
		Overall:         health.SeverityOK,
		RequestedAction: actions.ActionResolve,
		Resolved:        []string{"actuators"},
	})
	if err != nil {
		t.Fatalf("Apply resolve: %v", err)
	}
	if !result.ShouldExecuteHook || result.HookAction != actions.ActionResolve {
		t.Fatalf("resolve hook = %+v, want resolve hook execution", result)
	}
	if len(result.State.ActiveComponents) != 0 || result.State.OverallAction != actions.ActionNone {
		t.Fatalf("state after resolve = %+v, want empty/none", result.State)
	}
}

func TestManagerSuppressesRepeatedSameActionWithinCooldown(t *testing.T) {
	manager, err := LoadManager(filepath.Join(t.TempDir(), "current_state.json"), CooldownConfig{
		Degrade: time.Hour,
	})
	if err != nil {
		t.Fatalf("LoadManager: %v", err)
	}

	firstAt := time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC)
	first, err := manager.Apply(actions.Request{
		SchemaVersion:   1,
		RequestID:       "req-1",
		Event:           actions.EventTransition,
		Timestamp:       firstAt,
		Hostname:        "robot-1",
		Overall:         health.SeverityFail,
		RequestedAction: actions.ActionDegrade,
		Components: []actions.ComponentRequest{
			{
				ComponentID:     "planner",
				Severity:        health.SeverityFail,
				RequestedAction: actions.ActionDegrade,
				Reason:          "missed deadline",
				SourceTypes:     []string{"module"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Apply first request: %v", err)
	}
	if !first.ShouldExecuteHook {
		t.Fatalf("first request should execute hook: %+v", first)
	}

	second, err := manager.Apply(actions.Request{
		SchemaVersion:   1,
		RequestID:       "req-2",
		Event:           actions.EventTransition,
		Timestamp:       firstAt.Add(5 * time.Second),
		Hostname:        "robot-1",
		Overall:         health.SeverityFail,
		RequestedAction: actions.ActionDegrade,
		Components: []actions.ComponentRequest{
			{
				ComponentID:     "planner",
				Severity:        health.SeverityFail,
				RequestedAction: actions.ActionDegrade,
				Reason:          "missed deadline again",
				SourceTypes:     []string{"module"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Apply second request: %v", err)
	}
	if second.ShouldExecuteHook {
		t.Fatalf("second request unexpectedly executed hook: %+v", second)
	}
	if !strings.Contains(second.SuppressionReason, "cooldown") {
		t.Fatalf("suppression = %q, want cooldown reason", second.SuppressionReason)
	}
	if got := second.State.OverallAction; got != actions.ActionDegrade {
		t.Fatalf("overall action = %s, want degrade", got)
	}
}
