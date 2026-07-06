# Retention, Durability & TTL — Design Spec

- Date: 2026-07-06
- Status: Implemented
- Area: `internal/supervisor`, `internal/incident`, `internal/adapters/module`, `internal/metrics`, new `internal/atomicwrite`, new `internal/retention`

## Problem

Four unbounded-growth / durability gaps were confirmed in code review:

1. **No retention anywhere.** The supervisor audit dir, incident dir, and shadow-FSM request dir accumulate one file per event forever. On a resource-constrained SD/eMMC robot this means watchdog will eventually alert on the storage it filled.
2. **Audit dir doubles as the dedup store.** `handlePayload` (`internal/supervisor/server.go:150-159`) decides "duplicate request" by `os.Stat` on `<AuditDir>/<RequestID>.json`. Any naive pruning of the audit dir would silently break duplicate suppression.
3. **No fsync.** Both writers (`internal/supervisor/server.go:328` `writeJSONFile`, `internal/incident/writer.go:42-63`) do temp-write + rename with no `fsync` of the file or parent directory. This is crash-safe but **not power-loss durable** — and power cuts are the canonical robot incident, so the on-device forensic evidence can be lost exactly when it matters.
4. **Module reports never evicted.** `internal/adapters/module/module.go:197-199` inserts into `c.reports[sourceID]` and never removes. Memory and Prometheus series cardinality grow without bound; any local process can invent `source_id`s.

## Goals / Non-Goals

| In scope | Out of scope (why) |
| --- | --- |
| Retention for **audit**, **incident**, **shadow-request** dirs | **rawlog manifest retention** — incidents reference manifests; pruning here creates dangling incident→segment links. Manifest/segment lifecycle belongs to the log-agent (ROADMAP M5). |
| `fsync` for **forensic** writes (incident snapshots, audit records, shadow-FSM request records) | `fsync` for reconstructable mirrors (`latest.json`, `current_state.json`, shadow `latest`) — would thrash flash for no durability gain |
| Module report **TTL eviction** (map + metrics) | `rawlog` `matchingSegments` O(dir) walk optimization — real but separate from retention |
| **Decouple dedup from retention** | arm64 baseline config (separate follow-up) |

## Design Decisions

- **Retention model: size + count cap with a minimum-keep floor** (user-selected). Each directory has a max total byte budget AND a max file count; oldest files are pruned first; the newest `MinKeep` files are always retained regardless. Robust against both slow growth and incident storms — the disk cannot be filled.
- **fsync scope: forensic writes only, no config knob** (user-selected). Incident snapshots and audit records get file + parent-directory fsync. High-churn mirrors do not.
- **Module eviction: full removal after a configurable TTL** (user-selected, default 15m). A source with no data within the TTL is dropped from the in-memory map and from the metrics surface; a later report re-adds it.
- **Retention executes in a background sweeper**, not inline on the write path. The supervisor receive loop and the watchdog poll loop must never block on pruning (core project principle: retention pressure cannot block health/action paths).

## Component Design

### 1. `internal/atomicwrite` — shared atomic write helper

Consolidates the two duplicated temp+rename implementations into one package.

```go
// WriteDurable writes data atomically and survives power loss:
//   temp write -> f.Sync() -> close -> rename -> fsync(parent dir).
// A parent-dir fsync error that indicates the filesystem does not support it
// (EINVAL/ENOTSUP) is ignored; other errors are returned.
func WriteDurable(path string, data []byte, mode os.FileMode) error

// WriteAtomic writes data atomically (temp write -> close -> rename) with no
// fsync. Crash-safe, not power-loss durable. Preserves current behavior.
func WriteAtomic(path string, data []byte, mode os.FileMode) error
```

Callers:
- `incident.Writer.MaybeWrite` → `WriteDurable`
- supervisor audit record write → `WriteDurable`
- supervisor `latest.json`, `current_state.json` mirrors → `WriteAtomic`
- shadow-FSM request write → `WriteDurable` (it is forensic evidence of what would have been requested); `latest` shadow mirror → `WriteAtomic`

The temp file is created in the **same directory** as the target so rename is atomic (same filesystem).

### 2. `internal/retention` — pure policy + sweeper

```go
type Policy struct {
    MaxBytes int64 // 0 = unlimited
    MaxFiles int   // 0 = unlimited
    MinKeep  int   // always retain the newest N matching files
}

// Prune deletes oldest matching files in dir until both MaxFiles and MaxBytes
// are satisfied, never deleting the newest MinKeep. Ordering is by filename
// descending; all retained dirs use timestamp-prefixed names
// (incident: 20060102T150405Z_<overall>.json; audit/shadow: <RequestID>.json
// where RequestID is timestamp-prefixed), so lexical order == chronological.
// Per-file remove errors are collected and returned but do not stop the sweep.
func Prune(dir string, match func(name string) bool, p Policy) (removed int, err error)
```

- `match` excludes in-progress temp files (`*.tmp`, `.incident-*.tmp`) so a write racing with a sweep is never pruned.
- `Prune` is a pure function of (filesystem state, policy) → easy to unit test with a temp dir.

**Sweeper**: a small type owning a list of `{dir, match, policy}` targets, running `Prune` on each on a ticker (`SweepInterval`, default 60s), in a goroutine bound to the daemon's context. Removed counts and errors are logged; errors never crash the daemon and are retried on the next tick. One sweeper instance in the supervisor (audit + shadow dirs), one in the watchdog daemon (incident dir).

### 3. Dedup decoupled from retention — bounded recent-ID set

Add an in-memory bounded set of recently seen request IDs to the supervisor:

```go
type recentIDs struct {
    mu    sync.Mutex
    set   map[string]struct{}
    order []string // insertion order ring for eviction
    cap   int
}
func (r *recentIDs) seen(id string) bool
func (r *recentIDs) add(id string)   // evicts oldest when len > cap
```

- **Seed on startup**: list the audit dir, sort names descending, `add` the newest up to `cap`. Reseeding after a supervisor restart reconstructs the dedup window from durable audit files.
- `handlePayload` checks `recentIDs.seen(request.RequestID)` instead of `os.Stat`; on a fresh request it processes, writes the durable audit record, then `recentIDs.add(id)`. If the audit write fails the request is not marked seen (allows a correct retry).
- Because request IDs are timestamp-monotonic, a pruned (old) ID can never legitimately recur, so retention of old audit files is irrelevant to correctness.
- Invariant: `dedup_cache_size <= audit MaxFiles` so a restart reseed always covers the full dedup window.

### 4. Module report TTL eviction

- Add `ReportTTL time.Duration` to the module `Collector` config (0 = disabled/never-evict for backward compatibility, but the default config sets 15m).
- `reportState` records a `receivedAt` timestamp when the datagram is accepted.
- On each `Collect`, entries with `now.Sub(receivedAt) > ReportTTL` are removed from `c.reports`.
- **Metrics must also drop the evicted source.** The watchdog metrics collector derives series from the current snapshot/statuses; the implementation must ensure evicted sources leave the exported series (either by rebuilding the gauge vectors from the current snapshot each publish, or by `DeleteLabelValues` for evicted `source_id`s). The chosen mechanism is verified against `internal/metrics/watchdog.go` during implementation.

## Config Additions (all optional, backward compatible)

Supervisor config:
```json
"retention": {
  "sweep_interval": "60s",
  "audit":  { "max_files": 5000, "max_bytes": "64Mi", "min_keep": 100 },
  "shadow": { "max_files": 1000, "max_bytes": "32Mi", "min_keep": 50 }
},
"dedup_cache_size": 2048
```

Watchdog config:
```json
"retention": {
  "sweep_interval": "60s",
  "incidents": { "max_files": 1000, "max_bytes": "64Mi", "min_keep": 50 }
},
"sources": { "module": { "report_ttl": "15m" } }
```

- Byte budgets accept human units (`64Mi`) or plain integers; parsing helper added if not already present.
- Omitting a block keeps today's behavior for the write paths but enables the documented defaults; a `max_*` of 0 means unlimited. `report_ttl` of 0 disables eviction.
- No fsync knob (forensic-only is fixed).

## Testing

Follow existing repo conventions (real temp dirs, real sockets, helper processes).

- `retention.Prune`: MinKeep boundary; MaxFiles-only, MaxBytes-only, and both exceeded; empty dir; match filter excludes `*.tmp`; per-file remove error is reported but sweep continues.
- `atomicwrite`: `WriteDurable` and `WriteAtomic` produce identical file contents and leave no `*.tmp` residue; target replaces existing file atomically. (fsync itself is not directly observable; the behavioral contract — content, atomic replace, no residue — is asserted.)
- Dedup: fresh request processed; immediate duplicate suppressed; after simulated restart (new `recentIDs` seeded from audit dir) a replayed request is still suppressed; a pruned old ID does not resurface as "new".
- Module TTL: a source past TTL disappears from the next snapshot and from exported metrics; a subsequent report re-adds it.
- Sweeper non-blocking: high-frequency writes concurrent with an active sweep are not blocked and never lose the file currently being written.

## Failure Modes

- Prune failure (permission/IO): logged, retried next tick, daemon keeps running.
- Parent-dir fsync unsupported by filesystem (EINVAL/ENOTSUP): ignored, write proceeds.
- Corrupt/foreign file encountered during dedup seed: skipped.
- Byte-budget config unparseable: config validation error at load (fail fast), consistent with existing config validation.

## Acceptance Criteria

- Audit, incident, and shadow dirs stay within their configured file-count and byte budgets under sustained load, while always retaining the newest `MinKeep`.
- After a simulated power loss (process kill mid-run), previously completed incident and audit records are present and parseable.
- Duplicate request suppression survives a supervisor restart with retention actively pruning old audit files.
- An idle module source is gone from both `watchdogctl status` and `/metrics` after its TTL; memory for the map does not grow with churned source IDs.
- Disabling retention/TTL (zeros) reproduces today's behavior. Nothing on the write path blocks on pruning.

## Out of Scope / Follow-ups

1. rawlog manifest/segment retention (log-agent-owned; must not orphan incident→segment links).
2. `rawlog.Linker.matchingSegments` O(dir) walk optimization.
3. arm64 baseline config for release packaging.
