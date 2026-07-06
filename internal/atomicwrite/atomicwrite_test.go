package atomicwrite

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteDurableWritesContentAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")

	if err := WriteDurable(path, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatalf("WriteDurable: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Fatalf("content = %q, want %q", got, `{"a":1}`)
	}

	// No temp residue left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want 1 (temp residue?)", len(entries))
	}
}

func TestWriteAtomicReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mirror.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want 1", len(entries))
	}
}
