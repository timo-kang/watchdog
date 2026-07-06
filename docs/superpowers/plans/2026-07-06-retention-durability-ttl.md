# Retention, Durability & TTL Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bound on-device storage/memory growth and make forensic writes power-loss durable, without ever blocking the watchdog health/action paths.

**Architecture:** Two new pure packages — `internal/atomicwrite` (durable vs atomic file writes) and `internal/retention` (Prune policy + background sweeper) — are built first and independently. Integration then adopts durable writes in the incident + supervisor writers, decouples supervisor dedup from the audit dir via an in-memory bounded set, adds module-report TTL eviction, and wires sweepers + config for both daemons.

**Tech Stack:** Go 1.22 (stdlib only + existing `prometheus/client_golang`), Unix datagram sockets, `os`/`syscall` for fsync, table-driven tests with `t.TempDir()`.

## Global Constraints

- Pure Go, no new external dependencies (only stdlib; `prometheus/client_golang` already present). Copy verbatim.
- Backward compatible: a zero/omitted retention budget (`max_files:0`, `max_bytes:0`) or `report_ttl:0` disables that limit and reproduces today's behavior.
- Retention runs in a background sweeper only — never inline on the supervisor receive loop or watchdog poll loop.
- fsync scope is forensic-only (incident snapshots, audit records, shadow-FSM request records). Mirrors (`latest.json`, `current_state.json`, shadow `latest`) use atomic-only writes. No fsync config knob.
- All timestamped filenames are lexically chronological; retention orders by filename, not mtime.
- Invariant: `dedup_cache_size <= audit.max_files` so a restart reseed covers the full dedup window.
- gofmt-clean; `go vet ./...`, `go test -race ./...` green before each commit.
- Spec of record: `docs/superpowers/specs/2026-07-06-retention-durability-ttl-design.md`.

---

## Execution Topology

- **Phase 1 (parallel-safe, new files only):** Task 1 in one worktree; Tasks 2→3 in a second worktree (same package). No shared files with each other or with `main`.
- **Phase 2 (sequential, shared files):** Tasks 4–10 modify `internal/supervisor/server.go`, the two config packages, `internal/adapters/module/module.go`, and `internal/app/app.go`. Run these in order on a single branch after Phase 1 merges.

---

## Task 1: `internal/atomicwrite` package

**Files:**
- Create: `internal/atomicwrite/atomicwrite.go`
- Test: `internal/atomicwrite/atomicwrite_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func WriteDurable(path string, data []byte, mode os.FileMode) error`
  - `func WriteAtomic(path string, data []byte, mode os.FileMode) error`

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/atomicwrite/ -run TestWrite -v`
Expected: FAIL — `undefined: WriteDurable` / `undefined: WriteAtomic`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package atomicwrite provides atomic file writes with an optional
// power-loss-durable variant. Both write to a temp file in the target
// directory and rename into place, so a crash never leaves a partially
// written target. WriteDurable additionally fsyncs the file and its parent
// directory so the data survives power loss, at the cost of extra flash I/O.
package atomicwrite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func WriteDurable(path string, data []byte, mode os.FileMode) error {
	return write(path, data, mode, true)
}

func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	return write(path, data, mode, false)
}

func write(path string, data []byte, mode os.FileMode, durable bool) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".atomic-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if durable {
		if err := tmp.Sync(); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("fsync temp file: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	cleanup = false

	if durable {
		if err := fsyncDir(dir); err != nil {
			return fmt.Errorf("fsync dir: %w", err)
		}
	}
	return nil
}

// fsyncDir fsyncs a directory so a rename is durable. Filesystems that do not
// support directory fsync report EINVAL/ENOTSUP; those are treated as success.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
			return nil
		}
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race ./internal/atomicwrite/ -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add internal/atomicwrite/
git commit -m "feat: add atomicwrite package with durable and atomic writes"
```

---

## Task 2: `internal/retention` — Policy, Prune, ParseByteSize

**Files:**
- Create: `internal/retention/retention.go`
- Test: `internal/retention/retention_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Policy struct { MaxBytes int64; MaxFiles int; MinKeep int }`
  - `func Prune(dir string, match func(name string) bool, p Policy) (removed int, err error)`
  - `func ParseByteSize(s string) (int64, error)` — accepts plain integers and Ki/Mi/Gi suffixes (`"64Mi"` → 67108864); `""` → 0.

- [ ] **Step 1: Write the failing test**

```go
package retention

import (
	"fmt"
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
	_ = fmt.Sprint
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/retention/ -v`
Expected: FAIL — `undefined: Prune` / `undefined: Policy` / `undefined: ParseByteSize`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package retention bounds a directory's file count and byte size, always
// keeping the newest MinKeep files. It is a pure function of filesystem state
// plus policy; the sweeper (sweeper.go) drives it on a ticker.
package retention

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type Policy struct {
	MaxBytes int64 // 0 = unlimited
	MaxFiles int   // 0 = unlimited
	MinKeep  int   // always retain the newest N matching files
}

func (p Policy) enabled() bool { return p.MaxBytes > 0 || p.MaxFiles > 0 }

type fileInfo struct {
	name string
	size int64
}

// Prune deletes oldest matching files until both MaxFiles and MaxBytes are
// satisfied, never deleting the newest MinKeep. Files are ordered by name
// (ascending == oldest-first) because all retained dirs use timestamp-prefixed
// names. Per-file remove errors are joined and returned; the sweep continues.
func Prune(dir string, match func(name string) bool, p Policy) (int, error) {
	if !p.enabled() {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	files := make([]fileInfo, 0, len(entries))
	var totalBytes int64
	for _, e := range entries {
		if e.IsDir() || !match(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{name: e.Name(), size: info.Size()})
		totalBytes += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name }) // oldest first

	totalFiles := len(files)
	removed := 0
	var errs []error
	for i := 0; i < len(files) && totalFiles > p.MinKeep; i++ {
		overCount := p.MaxFiles > 0 && totalFiles > p.MaxFiles
		overBytes := p.MaxBytes > 0 && totalBytes > p.MaxBytes
		if !overCount && !overBytes {
			break
		}
		if err := os.Remove(dir + "/" + files[i].name); err != nil {
			errs = append(errs, err)
			continue
		}
		totalFiles--
		totalBytes -= files[i].size
		removed++
	}
	if len(errs) > 0 {
		return removed, fmt.Errorf("prune %s: %d errors, first: %w", dir, len(errs), errs[0])
	}
	return removed, nil
}

// ParseByteSize parses "1024", "64Mi", "2Ki", "1Gi". Empty string == 0.
func ParseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "Ki"):
		mult, s = 1024, strings.TrimSuffix(s, "Ki")
	case strings.HasSuffix(s, "Mi"):
		mult, s = 1024*1024, strings.TrimSuffix(s, "Mi")
	case strings.HasSuffix(s, "Gi"):
		mult, s = 1024*1024*1024, strings.TrimSuffix(s, "Gi")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse byte size %q: %w", s, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("byte size must not be negative: %q", s)
	}
	return n * mult, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race ./internal/retention/ -run 'TestPrune|TestParseByteSize' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/retention/retention.go internal/retention/retention_test.go
git commit -m "feat: add retention Prune policy and byte-size parser"
```

---

## Task 3: `internal/retention` — background Sweeper

**Files:**
- Create: `internal/retention/sweeper.go`
- Test: `internal/retention/sweeper_test.go`

**Interfaces:**
- Consumes: `Prune`, `Policy` (Task 2).
- Produces:
  - `type Target struct { Dir string; Match func(name string) bool; Policy Policy }`
  - `type Sweeper struct { ... }`
  - `func NewSweeper(logger *log.Logger, interval time.Duration, targets ...Target) *Sweeper`
  - `func (s *Sweeper) Run(ctx context.Context)` — blocks until ctx is done; prune once immediately, then every interval.
  - `func (s *Sweeper) SweepOnce()` — one pass over all targets (used by tests and by Run).

- [ ] **Step 1: Write the failing test**

```go
package retention

import (
	"log"
	"io"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/retention/ -run TestSweeper -v`
Expected: FAIL — `undefined: NewSweeper`.

- [ ] **Step 3: Write minimal implementation**

```go
package retention

import (
	"context"
	"log"
	"time"
)

type Target struct {
	Dir    string
	Match  func(name string) bool
	Policy Policy
}

type Sweeper struct {
	logger   *log.Logger
	interval time.Duration
	targets  []Target
}

func NewSweeper(logger *log.Logger, interval time.Duration, targets ...Target) *Sweeper {
	return &Sweeper{logger: logger, interval: interval, targets: targets}
}

// SweepOnce prunes every target once. Per-target errors are logged, never fatal.
func (s *Sweeper) SweepOnce() {
	for _, t := range s.targets {
		removed, err := Prune(t.Dir, t.Match, t.Policy)
		if err != nil && s.logger != nil {
			s.logger.Printf("retention: prune %s error: %v", t.Dir, err)
		}
		if removed > 0 && s.logger != nil {
			s.logger.Printf("retention: pruned %d files from %s", removed, t.Dir)
		}
	}
}

// Run prunes immediately, then every interval, until ctx is cancelled.
// If interval <= 0, it prunes once and returns (test/one-shot mode).
func (s *Sweeper) Run(ctx context.Context) {
	s.SweepOnce()
	if s.interval <= 0 {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SweepOnce()
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race ./internal/retention/ -v`
Expected: PASS (all retention tests).

- [ ] **Step 5: Commit**

```bash
git add internal/retention/sweeper.go internal/retention/sweeper_test.go
git commit -m "feat: add retention background sweeper"
```

---

## Task 4: Adopt durable writes in the incident writer

**Files:**
- Modify: `internal/incident/writer.go:37-64` (replace the temp-create/write/chmod/close/rename block)
- Test: `internal/incident/writer_test.go` (existing tests must still pass; add no new behavior)

**Interfaces:**
- Consumes: `atomicwrite.WriteDurable` (Task 1).

- [ ] **Step 1: Replace the manual write with WriteDurable**

In `internal/incident/writer.go`, add import `"watchdog/internal/atomicwrite"` and replace lines 37-64 (from `data, err := json.MarshalIndent...` through the final `return path, nil`) with:

```go
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal incident: %w", err)
	}
	if err := atomicwrite.WriteDurable(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write incident: %w", err)
	}
	return path, nil
```

Remove the now-unused `"os"` import only if no other reference remains (the `os.MkdirAll` on line 30 still uses it — keep `"os"`).

- [ ] **Step 2: Run the existing incident tests**

Run: `go test -race ./internal/incident/ -v`
Expected: PASS (existing coverage: writes on transition, skips OK, filename format). If a test asserted on temp-file internals it must be updated to assert on the final file only.

- [ ] **Step 3: Verify no temp residue regression**

Run: `go test -race ./internal/incident/ -run . -count=1`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/incident/writer.go internal/incident/writer_test.go
git commit -m "refactor: incident writer uses durable atomic write"
```

---

## Task 5: Adopt atomicwrite in supervisor server + shadow_fsm

**Files:**
- Modify: `internal/supervisor/server.go:182-189` (audit + latest), `:328-341` (delete `writeJSONFile`)
- Modify: `internal/supervisor/shadow_fsm.go:55-69` (request + latest)
- Test: `internal/supervisor/server_test.go` (existing must pass)

**Interfaces:**
- Consumes: `atomicwrite.WriteDurable`, `atomicwrite.WriteAtomic` (Task 1).

- [ ] **Step 1: Add a JSON helper that marshals then delegates**

In `internal/supervisor/server.go`, add import `"watchdog/internal/atomicwrite"` and replace the `writeJSONFile` function (lines 328-341) with two helpers:

```go
func writeJSONDurable(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json file: %w", err)
	}
	return atomicwrite.WriteDurable(path, data, 0o644)
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json file: %w", err)
	}
	return atomicwrite.WriteAtomic(path, data, 0o644)
}
```

- [ ] **Step 2: Route each writer to the right durability**

In `handlePayload` (around lines 182-189): the audit record is forensic → `writeJSONDurable(recordPath, record)`; the `latest.json` mirror is reconstructable → `writeJSONAtomic(s.cfg.LatestPath, record)`.

In `internal/supervisor/shadow_fsm.go` (lines 55-69): the per-request file → `writeJSONDurable(path, robotRequest)`; the shadow `LatestPath` mirror → `writeJSONAtomic(...)`.

Search the package for any other `writeJSONFile(` callers (e.g. `state.go` `current_state.json`) and route them to `writeJSONAtomic` (mirror/state file, reconstructable).

- [ ] **Step 3: Run the supervisor tests**

Run: `go test -race ./internal/supervisor/ -v`
Expected: PASS — including `TestServerProcessesAndDedupesRequests` and `TestServerWritesShadowFSMRequestWithoutCommandHook`.

- [ ] **Step 4: Commit**

```bash
git add internal/supervisor/server.go internal/supervisor/shadow_fsm.go internal/supervisor/state.go
git commit -m "refactor: supervisor writes use durable vs atomic per forensic value"
```

---

## Task 6: Decouple supervisor dedup from the audit dir

**Files:**
- Create: `internal/supervisor/dedup.go`
- Test: `internal/supervisor/dedup_test.go`
- Modify: `internal/supervisor/server.go` (struct field, `NewServer`, `Run` seed, `handlePayload` check)

**Interfaces:**
- Consumes: nothing new.
- Produces (package-internal):
  - `type recentIDs struct { ... }`
  - `func newRecentIDs(capacity int) *recentIDs`
  - `func (r *recentIDs) seen(id string) bool`
  - `func (r *recentIDs) add(id string)`
  - `func seedRecentIDs(auditDir string, capacity int) *recentIDs` — reads newest audit filenames, strips `.json`, adds up to capacity.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/supervisor/ -run 'TestRecentIDs|TestSeedRecentIDs' -v`
Expected: FAIL — `undefined: newRecentIDs` / `undefined: seedRecentIDs`.

- [ ] **Step 3: Write the implementation**

Create `internal/supervisor/dedup.go`:

```go
package supervisor

import (
	"os"
	"sort"
	"strings"
	"sync"
)

type recentIDs struct {
	mu    sync.Mutex
	set   map[string]struct{}
	order []string
	cap   int
}

func newRecentIDs(capacity int) *recentIDs {
	if capacity < 1 {
		capacity = 1
	}
	return &recentIDs{set: make(map[string]struct{}, capacity), cap: capacity}
}

func (r *recentIDs) seen(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.set[id]
	return ok
}

func (r *recentIDs) add(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.set[id]; ok {
		return
	}
	r.set[id] = struct{}{}
	r.order = append(r.order, id)
	for len(r.order) > r.cap {
		oldest := r.order[0]
		r.order = r.order[1:]
		delete(r.set, oldest)
	}
}

// seedRecentIDs loads the newest audit request IDs (filename without .json) so
// duplicate suppression survives a restart even as retention prunes old files.
func seedRecentIDs(auditDir string, capacity int) *recentIDs {
	r := newRecentIDs(capacity)
	entries, err := os.ReadDir(auditDir)
	if err != nil {
		return r
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names) // oldest first; timestamp-prefixed
	// Seed the newest `capacity` in chronological order so eviction order matches.
	if len(names) > capacity {
		names = names[len(names)-capacity:]
	}
	for _, n := range names {
		r.add(strings.TrimSuffix(n, ".json"))
	}
	return r
}
```

- [ ] **Step 4: Wire it into the Server**

In `internal/supervisor/server.go`:
- Add field to `Server`: `dedup *recentIDs`.
- In `Run`, after `os.MkdirAll(s.cfg.AuditDir, ...)` (line 62-64) and before the receive loop, seed it: `s.dedup = seedRecentIDs(s.cfg.AuditDir, s.cfg.DedupCacheSize)`. (Task 7 adds `DedupCacheSize`; until then use a literal `2048`.)
- In `handlePayload`, replace the `os.Stat(recordPath)` duplicate check (lines 150-159) with:

```go
	recordPath := filepath.Join(s.cfg.AuditDir, request.RequestID+".json")
	if s.dedup.seen(request.RequestID) {
		if s.observer != nil {
			s.observer.ObserveRequest(request, "duplicate")
		}
		s.logger.Printf("duplicate request_id=%s action=%s", request.RequestID, request.RequestedAction)
		return nil
	}
```

- After a successful audit write (after line 184 `writeJSONDurable(recordPath, record)` returns nil), record it: `s.dedup.add(request.RequestID)`.

- [ ] **Step 5: Run tests**

Run: `go test -race ./internal/supervisor/ -v`
Expected: PASS — the existing `TestServerProcessesAndDedupesRequests` still passes (dedup now memory-backed), plus the two new dedup tests.

- [ ] **Step 6: Commit**

```bash
git add internal/supervisor/dedup.go internal/supervisor/dedup_test.go internal/supervisor/server.go
git commit -m "feat: decouple supervisor dedup from audit dir via bounded recent-id set"
```

---

## Task 7: Supervisor retention config + sweeper wiring

**Files:**
- Modify: `internal/supervisor/config.go` (add `Retention`, `DedupCacheSize`, `SweepInterval`)
- Modify: `internal/supervisor/server.go` (`Run` starts the sweeper goroutine; use `s.cfg.DedupCacheSize`)
- Test: `internal/supervisor/config_test.go`

**Interfaces:**
- Consumes: `retention.Policy`, `retention.Sweeper`, `retention.NewSweeper`, `retention.ParseByteSize` (Tasks 2–3).

- [ ] **Step 1: Write the failing config test**

```go
func TestLoadConfigDefaultsRetention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "supervisor.json")
	if err := os.WriteFile(path, []byte(`{"socket_path":"/x.sock","audit_dir":"/a","state_path":"/s.json"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DedupCacheSize != 2048 {
		t.Fatalf("DedupCacheSize = %d, want 2048", cfg.DedupCacheSize)
	}
	if cfg.Retention.Audit.MaxFiles != 5000 || cfg.Retention.Audit.MinKeep != 100 {
		t.Fatalf("audit retention defaults wrong: %+v", cfg.Retention.Audit)
	}
	if cfg.Retention.SweepInterval != time.Minute {
		t.Fatalf("SweepInterval = %v, want 1m", cfg.Retention.SweepInterval)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/supervisor/ -run TestLoadConfigDefaultsRetention -v`
Expected: FAIL — `cfg.Retention undefined` / `cfg.DedupCacheSize undefined`.

- [ ] **Step 3: Extend the config types + loader**

In `internal/supervisor/config.go`:
- Add to `Config`: `Retention RetentionConfig` and `DedupCacheSize int`.
- Add types:

```go
type RetentionConfig struct {
	SweepInterval time.Duration
	Audit         retention.Policy
	Shadow        retention.Policy
}

type fileRetentionConfig struct {
	SweepInterval string          `json:"sweep_interval"`
	Audit         filePolicyConfig `json:"audit"`
	Shadow        filePolicyConfig `json:"shadow"`
}

type filePolicyConfig struct {
	MaxFiles int    `json:"max_files"`
	MaxBytes string `json:"max_bytes"`
	MinKeep  int    `json:"min_keep"`
}
```

- Add `Retention fileRetentionConfig json:"retention"` and `DedupCacheSize int json:"dedup_cache_size"` to `fileConfig`, and set defaults in the `raw := fileConfig{...}` literal:

```go
		DedupCacheSize: 2048,
		Retention: fileRetentionConfig{
			SweepInterval: "60s",
			Audit:         filePolicyConfig{MaxFiles: 5000, MaxBytes: "64Mi", MinKeep: 100},
			Shadow:        filePolicyConfig{MaxFiles: 1000, MaxBytes: "32Mi", MinKeep: 50},
		},
```

- After cooldown parsing, parse retention (add import `"watchdog/internal/retention"`):

```go
	retentionCfg, err := parseRetention(raw.Retention)
	if err != nil {
		return Config{}, err
	}
	dedupSize := raw.DedupCacheSize
	if dedupSize <= 0 {
		dedupSize = 2048
	}
```

- Add the parser and include `Retention: retentionCfg, DedupCacheSize: dedupSize` in the returned `Config{...}`:

```go
func parseRetention(raw fileRetentionConfig) (RetentionConfig, error) {
	interval, err := time.ParseDuration(nonEmpty(raw.SweepInterval, "60s"))
	if err != nil {
		return RetentionConfig{}, fmt.Errorf("parse retention.sweep_interval: %w", err)
	}
	audit, err := parsePolicy(raw.Audit)
	if err != nil {
		return RetentionConfig{}, fmt.Errorf("retention.audit: %w", err)
	}
	shadow, err := parsePolicy(raw.Shadow)
	if err != nil {
		return RetentionConfig{}, fmt.Errorf("retention.shadow: %w", err)
	}
	return RetentionConfig{SweepInterval: interval, Audit: audit, Shadow: shadow}, nil
}

func parsePolicy(raw filePolicyConfig) (retention.Policy, error) {
	maxBytes, err := retention.ParseByteSize(raw.MaxBytes)
	if err != nil {
		return retention.Policy{}, err
	}
	return retention.Policy{MaxBytes: maxBytes, MaxFiles: raw.MaxFiles, MinKeep: raw.MinKeep}, nil
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
```

Add `"strings"` to imports.

- [ ] **Step 4: Start the sweeper in Run**

In `internal/supervisor/server.go` `Run`, replace the literal `2048` from Task 6 with `s.cfg.DedupCacheSize`, and after the state is loaded (after line 94) start the sweeper bound to ctx:

```go
	matchJSON := func(name string) bool { return strings.HasSuffix(name, ".json") }
	targets := []retention.Target{{Dir: s.cfg.AuditDir, Match: matchJSON, Policy: s.cfg.Retention.Audit}}
	if s.cfg.ShadowFSM.Enabled && s.cfg.ShadowFSM.RequestDir != "" {
		targets = append(targets, retention.Target{Dir: s.cfg.ShadowFSM.RequestDir, Match: matchJSON, Policy: s.cfg.Retention.Shadow})
	}
	sweeper := retention.NewSweeper(s.logger, s.cfg.Retention.SweepInterval, targets...)
	go sweeper.Run(ctx)
```

Add imports `"strings"` and `"watchdog/internal/retention"` to server.go.

- [ ] **Step 5: Run tests**

Run: `go test -race ./internal/supervisor/ -v`
Expected: PASS (config defaults test + all prior).

- [ ] **Step 6: Commit**

```bash
git add internal/supervisor/config.go internal/supervisor/config_test.go internal/supervisor/server.go
git commit -m "feat: supervisor retention config and background sweeper"
```

---

## Task 8: Module report TTL eviction

**Files:**
- Modify: `internal/adapters/module/module.go` (add `receivedAt` to `reportState`, evict in `Collect`)
- Modify: `internal/config/config.go` (add `ReportTTL` to `ModuleReportSourceConfig` + `report_ttl` parse)
- Test: `internal/adapters/module/module_test.go`

**Interfaces:**
- Consumes: `config.ModuleReportSourceConfig.ReportTTL` (this task adds it).

- [ ] **Step 1: Write the failing test**

```go
func TestCollectEvictsStaleSources(t *testing.T) {
	c := New(config.ModuleReportSourceConfig{ReportTTL: 50 * time.Millisecond, DefaultStaleAfter: time.Second})
	c.mu.Lock()
	c.reports["fresh"] = reportState{sourceID: "fresh", receivedAt: time.Now()}
	c.reports["stale"] = reportState{sourceID: "stale", receivedAt: time.Now().Add(-time.Second)}
	c.mu.Unlock()

	if _, err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	c.mu.RLock()
	_, hasFresh := c.reports["fresh"]
	_, hasStale := c.reports["stale"]
	c.mu.RUnlock()
	if !hasFresh {
		t.Fatal("fresh source should be retained")
	}
	if hasStale {
		t.Fatal("stale source should be evicted")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapters/module/ -run TestCollectEvictsStaleSources -v`
Expected: FAIL — `unknown field ReportTTL` / `unknown field receivedAt`.

- [ ] **Step 3: Implement**

In `internal/config/config.go`: add `ReportTTL time.Duration` to `ModuleReportSourceConfig` (after `DefaultStaleAfter`); add `ReportTTL string json:"report_ttl"` to the module reports fileConfig struct; default `"15m"` in the defaults literal; parse near the existing `moduleStaleAfter` block:

```go
	moduleReportTTL, err := time.ParseDuration(nonEmptyOr(raw.Sources.ModuleReports.ReportTTL, "15m"))
	if err != nil {
		return Config{}, fmt.Errorf("parse sources.module_reports.report_ttl: %w", err)
	}
	if moduleReportTTL < 0 {
		return Config{}, fmt.Errorf("sources.module_reports.report_ttl must not be negative")
	}
```

and set `ReportTTL: moduleReportTTL` in the constructed `ModuleReportSourceConfig{...}`. (Reuse or add a small `nonEmptyOr(v, fallback string) string` helper if the package lacks one.)

In `internal/adapters/module/module.go`:
- Add `receivedAt time.Time` to `reportState` (line 36-45).
- In `readLoop` where the report is stored (`c.reports[report.sourceID] = report`, line 198), set `report.receivedAt = time.Now()` before storing. In `decodeReport` return path, leave `receivedAt` zero (it's stamped at store time in the read loop).
- At the top of `Collect` (before the RLock snapshot copy near line 122), evict stale entries:

```go
	if c.cfg.ReportTTL > 0 {
		cutoff := time.Now().Add(-c.cfg.ReportTTL)
		c.mu.Lock()
		for id, st := range c.reports {
			if st.receivedAt.Before(cutoff) {
				delete(c.reports, id)
			}
		}
		c.mu.Unlock()
	}
```

Note: because `internal/metrics/watchdog.go:223` rebuilds all `watchdog_status_*` series from the current snapshot each scrape, an evicted source disappears from `/metrics` automatically — no `DeleteLabelValues` needed.

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/adapters/module/ ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/module/module.go internal/adapters/module/module_test.go internal/config/config.go
git commit -m "feat: evict stale module report sources after configurable TTL"
```

---

## Task 9: Watchdog incident retention config + sweeper wiring

**Files:**
- Modify: `internal/config/config.go` (add incident `Retention` config)
- Modify: `internal/app/app.go` (construct + run the incident-dir sweeper for the daemon's lifetime)
- Test: `internal/config/config_test.go`, `internal/app/app_test.go`

**Interfaces:**
- Consumes: `retention.Policy`, `retention.NewSweeper`, `retention.Target`, `retention.ParseByteSize`.

- [ ] **Step 1: Write the failing config test**

```go
func TestLoadConfigDefaultsIncidentRetention(t *testing.T) {
	// Build a minimal valid watchdog config file in a temp dir, load it,
	// and assert the incident retention defaults.
	// (Mirror the existing minimal-config test in this file for the boilerplate.)
	cfg := mustLoadMinimalConfig(t) // existing test helper or inline per this file's pattern
	if cfg.Retention.Incidents.MaxFiles != 1000 || cfg.Retention.Incidents.MinKeep != 50 {
		t.Fatalf("incident retention defaults wrong: %+v", cfg.Retention.Incidents)
	}
	if cfg.Retention.SweepInterval != time.Minute {
		t.Fatalf("SweepInterval = %v, want 1m", cfg.Retention.SweepInterval)
	}
}
```

If no `mustLoadMinimalConfig` helper exists, inline the minimal valid JSON the existing `config_test.go` already uses for its happy-path load test.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoadConfigDefaultsIncidentRetention -v`
Expected: FAIL — `cfg.Retention undefined`.

- [ ] **Step 3: Implement config**

In `internal/config/config.go`: add a `Retention WatchdogRetentionConfig` field to the watchdog `Config`, with:

```go
type WatchdogRetentionConfig struct {
	SweepInterval time.Duration
	Incidents     retention.Policy
}
```

Add the corresponding `fileConfig` block `retention.incidents{max_files,max_bytes,min_keep}` + `sweep_interval`, defaults `MaxFiles:1000, MaxBytes:"64Mi", MinKeep:50, SweepInterval:"60s"`, and a `parseWatchdogRetention` function mirroring `parseRetention`/`parsePolicy` from Task 7 (using `retention.ParseByteSize`). Import `"watchdog/internal/retention"`.

- [ ] **Step 4: Wire the sweeper in the daemon**

In `internal/app/app.go`, where the daemon's long-lived goroutines start (same lifetime/context as the poll loop), construct and run the sweeper for the incident dir:

```go
	incidentSweeper := retention.NewSweeper(logger, cfg.Retention.SweepInterval, retention.Target{
		Dir:    cfg.IncidentDir,
		Match:  func(name string) bool { return strings.HasSuffix(name, ".json") },
		Policy: cfg.Retention.Incidents,
	})
	go incidentSweeper.Run(ctx)
```

Use the app's existing logger and context variables (match their names in `app.go`). Add imports `"strings"` and `"watchdog/internal/retention"`.

- [ ] **Step 5: Run tests**

Run: `go test -race ./internal/config/ ./internal/app/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/app/app.go internal/app/app_test.go
git commit -m "feat: watchdog incident retention config and sweeper"
```

---

## Task 10: Config examples, docs, and full verification

**Files:**
- Modify: `configs/watchdog-supervisor.example.json`, `configs/watchdog.example.json` (document the new blocks)
- Modify: `README.md` (retention/durability/TTL note), `docs/observability.md` if it lists persisted dirs
- Modify: the design/plan status lines to "implemented"

- [ ] **Step 1: Add the new config blocks to the example files**

Add to `configs/watchdog-supervisor.example.json`:

```json
"dedup_cache_size": 2048,
"retention": {
  "sweep_interval": "60s",
  "audit":  { "max_files": 5000, "max_bytes": "64Mi", "min_keep": 100 },
  "shadow": { "max_files": 1000, "max_bytes": "32Mi", "min_keep": 50 }
}
```

Add to `configs/watchdog.example.json`:

```json
"retention": {
  "sweep_interval": "60s",
  "incidents": { "max_files": 1000, "max_bytes": "64Mi", "min_keep": 50 }
},
"sources": { "module_reports": { "report_ttl": "15m" } }
```

(Merge the `sources.module_reports` key into the existing `sources` block rather than duplicating it.)

- [ ] **Step 2: Document behavior in README**

Add a short "Retention & durability" subsection near the runtime-layout section: forensic writes (incidents, audit, shadow requests) are power-loss durable; audit/incident/shadow dirs are bounded by size+count with a min-keep floor pruned by a background sweeper; idle module sources are evicted after `report_ttl`. Note that `max_*: 0` or `report_ttl: 0` disables a limit.

- [ ] **Step 3: Validate example configs load**

Run:
```bash
go vet ./...
gofmt -l $(git ls-files '*.go')   # expect empty
go test -race ./...               # expect all packages ok
go build ./...
GOOS=linux GOARCH=arm64 go build ./...
```
Expected: vet clean, gofmt empty, all tests pass, both builds succeed.

- [ ] **Step 4: End-to-end durability + retention smoke check**

Drive the supervisor with the docker sim or a local run, send > `max_files` audit requests, and confirm the audit dir stabilizes at the cap while keeping the newest `min_keep`; kill -9 the process mid-run and confirm the newest incident/audit JSON files are still complete and parseable (`jq . <file>`).

- [ ] **Step 5: Commit**

```bash
git add configs/ README.md docs/
git commit -m "docs: document retention, durability, and module TTL; add config examples"
```

---

## Self-Review

- **Spec coverage:** retention model (Tasks 2,3,7,9) ✓; forensic fsync (Tasks 1,4,5) ✓; mirror atomic-only (Task 5) ✓; dedup decoupling + reseed invariant (Tasks 6,7) ✓; module TTL + metric removal (Task 8, metric removal verified via snapshot rebuild) ✓; config + backward-compat defaults (Tasks 7,8,9) ✓; sweeper non-blocking (Task 3 design, Tasks 7,9 wiring) ✓; rawlog retention out-of-scope (documented, not implemented) ✓.
- **Placeholder scan:** no TBD/TODO; every code step carries real code. Task 9 Step 1 references an existing-test helper *or* inlining — explicit, not vague.
- **Type consistency:** `retention.Policy{MaxBytes,MaxFiles,MinKeep}`, `retention.Target{Dir,Match,Policy}`, `retention.NewSweeper(logger,interval,targets...)`, `atomicwrite.WriteDurable/WriteAtomic(path,data,mode)`, `recentIDs.seen/add`, `seedRecentIDs(dir,cap)`, `ReportTTL`/`receivedAt` — names match across all tasks.
