package retention

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestSweeperSweepOncePrunesAllTargets(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	for _, n := range []string{"20260101T000001Z.json", "20260101T000002Z.json", "20260101T000003Z.json"} {
		_ = os.WriteFile(filepath.Join(dirA, n), []byte("xxxxx"), 0o644)
		_ = os.WriteFile(filepath.Join(dirB, n), []byte("xxxxx"), 0o644)
	}
	match := func(name string) bool { return filepath.Ext(name) == ".json" }
	s := NewSweeper(log.New(io.Discard, "", 0), 0,
		Target{Dir: dirA, Match: match, Policy: Policy{MaxFiles: 1}},
		Target{Dir: dirB, Match: match, Policy: Policy{MaxFiles: 2}},
	)
	s.SweepOnce()

	if got, _ := os.ReadDir(dirA); len(got) != 1 {
		t.Fatalf("dirA has %d files, want 1", len(got))
	}
	if got, _ := os.ReadDir(dirB); len(got) != 2 {
		t.Fatalf("dirB has %d files, want 2", len(got))
	}
}
