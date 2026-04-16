package supervisor

import (
	"path/filepath"
	"testing"
	"time"

	"watchdog/internal/actions"
)

func TestLoadStatusReadsStateAndLatest(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "current_state.json")
	latestPath := filepath.Join(tempDir, "latest.json")

	manager, err := LoadManager(statePath, CooldownConfig{})
	if err != nil {
		t.Fatalf("LoadManager: %v", err)
	}
	manager.state.ActiveComponents = []ComponentState{
		{
			ComponentID:  "planner",
			ActiveAction: actions.ActionDegrade,
			Latched:      true,
		},
	}
	manager.rebuild()
	if err := manager.Write(); err != nil {
		t.Fatalf("manager.Write: %v", err)
	}

	record := AuditRecord{
		Request: actions.Request{
			SchemaVersion:   1,
			RequestID:       "req-1",
			RequestedAction: actions.ActionDegrade,
		},
		Decision: ApplyResult{
			HookAction:        actions.ActionDegrade,
			ShouldExecuteHook: true,
		},
		Hook: &HookResult{
			Action:   actions.ActionDegrade,
			Executed: true,
		},
		ReceivedAt: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC),
	}
	if err := writeJSONFile(latestPath, record); err != nil {
		t.Fatalf("writeJSONFile: %v", err)
	}

	status, err := LoadStatus(statePath, latestPath)
	if err != nil {
		t.Fatalf("LoadStatus: %v", err)
	}
	if status.State.OverallAction != actions.ActionDegrade {
		t.Fatalf("OverallAction = %s", status.State.OverallAction)
	}
	if status.Latest == nil || status.Latest.Request.RequestID != "req-1" {
		t.Fatalf("Latest = %+v", status.Latest)
	}
}
