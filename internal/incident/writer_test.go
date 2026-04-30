package incident

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"watchdog/internal/health"
)

func TestMaybeWriteSuppressesEquivalentFailWhenTransitionsOnly(t *testing.T) {
	dir := t.TempDir()
	writer := New(dir, true)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	first := health.Snapshot{
		CollectedAt: now,
		Overall:     health.SeverityFail,
		Components: []health.ComponentStatus{{
			ComponentID: "system-clock",
			Severity:    health.SeverityFail,
			Sources: []health.ComponentSource{{
				SourceType: "time_sync",
				Severity:   health.SeverityFail,
				Reason:     "clock is not synchronized",
			}},
		}},
	}
	path, err := writer.MaybeWrite(nil, first)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	if path == "" {
		t.Fatal("expected first incident path")
	}

	second := first
	second.CollectedAt = now.Add(2 * time.Second)
	second.Components[0].Reason = "time_sync fail: clock is not synchronized for 1200s >= grace 600s"
	path, err = writer.MaybeWrite(&first, second)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if path != "" {
		t.Fatalf("expected no second incident path, got %q", path)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
}

func TestMaybeWriteUsesAtomicTempRename(t *testing.T) {
	dir := t.TempDir()
	writer := New(dir, true)
	snapshot := health.Snapshot{
		CollectedAt: time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		Overall:     health.SeverityWarn,
		Components: []health.ComponentStatus{{
			ComponentID: "host.local",
			Severity:    health.SeverityWarn,
			Sources: []health.ComponentSource{{
				SourceType: "host",
				Severity:   health.SeverityWarn,
				Reason:     "cpu temp 86.0C >= warn 85.0C",
			}},
		}},
	}
	path, err := writer.MaybeWrite(nil, snapshot)
	if err != nil {
		t.Fatalf("write incident: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read incident: %v", err)
	}
	var decoded health.Snapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal incident: %v", err)
	}
	if decoded.Overall != health.SeverityWarn {
		t.Fatalf("overall = %s, want warn", decoded.Overall)
	}

	matches, err := filepath.Glob(filepath.Join(dir, ".incident-*.tmp"))
	if err != nil {
		t.Fatalf("glob temp incidents: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no temp incidents, found %v", matches)
	}
}
