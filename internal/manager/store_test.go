package manager

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"watchdog/internal/dtc"
	"watchdog/internal/retention"
)

func TestFileEventSinkRecordsJSONL(t *testing.T) {
	dir := t.TempDir()
	sink, err := NewFileEventSink(dir, time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	defer sink.Close()

	for _, code := range []string{"P1310", "C1131"} {
		if err := sink.Record(dtc.DtcEvent{
			SchemaVersion: dtc.SchemaVersion,
			Code:          code,
			Severity:      dtc.SeverityWarn,
			Transition:    dtc.TransitionOpened,
			At:            time.Now().UTC(),
			SourceID:      "s1",
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 session file, got %d", len(entries))
	}
	f, err := os.Open(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var ev dtc.DtcEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("line not valid DtcEvent JSON: %v", err)
		}
		got = append(got, ev.Code)
	}
	if len(got) != 2 || got[0] != "P1310" || got[1] != "C1131" {
		t.Fatalf("expected two JSONL events P1310,C1131 got %v", got)
	}
}

func TestFileEventSinkPruneKeepsCurrent(t *testing.T) {
	dir := t.TempDir()
	// Pre-existing old session files (timestamp-prefixed, older than current).
	for _, n := range []string{"20260101T000001Z.jsonl", "20260101T000002Z.jsonl", "20260101T000003Z.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Current session file is newest.
	sink, err := NewFileEventSink(dir, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	defer sink.Close()

	removed, err := sink.Prune(retention.Policy{MaxFiles: 2})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	// 4 files total, MaxFiles 2, but the current file is excluded from pruning
	// and the match set is only the 3 old ones -> prune 1 to leave 2 old.
	if removed != 1 {
		t.Fatalf("expected 1 pruned, got %d", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, sink.name)); err != nil {
		t.Fatalf("current session file must survive prune: %v", err)
	}
}
