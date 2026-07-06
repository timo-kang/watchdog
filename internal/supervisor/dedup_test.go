package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecentIDsEvictsOldest(t *testing.T) {
	r := newRecentIDs(2)
	r.add("a")
	r.add("b")
	r.add("c") // evicts "a"
	if r.seen("a") {
		t.Fatal("a should have been evicted")
	}
	if !r.seen("b") || !r.seen("c") {
		t.Fatal("b and c should be present")
	}
}

func TestSeedRecentIDsFromNewestAuditFiles(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{
		"20260101T000001Z-notify", "20260101T000002Z-degrade", "20260101T000003Z-safe_stop",
	} {
		if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Capacity 2 => only the two newest IDs are seeded.
	r := seedRecentIDs(dir, 2)
	if r.seen("20260101T000001Z-notify") {
		t.Fatal("oldest should not be seeded")
	}
	if !r.seen("20260101T000003Z-safe_stop") || !r.seen("20260101T000002Z-degrade") {
		t.Fatal("two newest should be seeded")
	}
}
