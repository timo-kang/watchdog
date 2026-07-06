package retention

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func remaining(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestPruneMaxFilesKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	// Names are timestamp-prefixed; lexical order == chronological.
	writeFiles(t, dir,
		"20260101T000001Z.json", "20260101T000002Z.json",
		"20260101T000003Z.json", "20260101T000004Z.json",
	)
	removed, err := Prune(dir, matchJSON, Policy{MaxFiles: 2, MinKeep: 0})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	got := remaining(t, dir)
	if len(got) != 2 || got[0] != "20260101T000003Z.json" || got[1] != "20260101T000004Z.json" {
		t.Fatalf("remaining = %v, want the two newest", got)
	}
}

func TestPruneMinKeepOverridesBudget(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, "a1.json", "a2.json", "a3.json")
	// MaxFiles=1 would delete two, but MinKeep=3 protects all three.
	removed, err := Prune(dir, matchJSON, Policy{MaxFiles: 1, MinKeep: 3})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0 (MinKeep protects all)", removed)
	}
}

func TestPruneMaxBytes(t *testing.T) {
	dir := t.TempDir()
	// Each file is 10 bytes. Budget 25 bytes => keep at most 2 (20 bytes).
	writeFiles(t, dir, "b1.json", "b2.json", "b3.json", "b4.json")
	removed, err := Prune(dir, matchJSON, Policy{MaxBytes: 25, MinKeep: 0})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
}

func TestPruneIgnoresNonMatchingAndTemp(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, "keep.json", ".atomic-x.tmp", "notes.txt")
	removed, err := Prune(dir, matchJSON, Policy{MaxFiles: 0, MinKeep: 0})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0 (no budget set)", removed)
	}
}

func matchJSON(name string) bool {
	return filepath.Ext(name) == ".json"
}

func TestParseByteSize(t *testing.T) {
	cases := map[string]int64{"": 0, "0": 0, "1024": 1024, "64Mi": 67108864, "2Ki": 2048, "1Gi": 1073741824}
	for in, want := range cases {
		got, err := ParseByteSize(in)
		if err != nil {
			t.Fatalf("ParseByteSize(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("ParseByteSize(%q) = %d, want %d", in, got, want)
		}
	}
	if _, err := ParseByteSize("12Zz"); err == nil {
		t.Fatal("ParseByteSize(\"12Zz\") should error")
	}
}
