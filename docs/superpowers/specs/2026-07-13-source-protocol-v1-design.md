# Source Producer Protocol v1 — Design Spec

- Date: 2026-07-13
- Status: Approved (brainstorming), pending implementation plan
- Area: new `docs/source-protocol.md`, new `sdk/fixtures/source-protocol/v1/`, `internal/adapters/module` (export a validator), new `cmd/watchdog-report-validate`, README/CONTRIBUTING/SDK doc wiring

## Problem

Roadmap item: let third parties add a health source **without forking `internal/`**. Go's `internal/` visibility rule blocks external import, and Go's `plugin` package is too fragile for real dynamic loading — an in-process Go plugin would still require compiling into a custom binary (a fork in all but name).

But the project **already has** a language-agnostic source mechanism: the module-report Unix-datagram socket (`internal/adapters/module`), with a JSON message schema, a header-only C++ SDK, and an example fixture (`sdk/cpp/fixtures/module_report.v1.json`). Any process in any language can already be a source. What is missing is that this contract is **not frozen, not normatively documented, and not machine-verified** — so external producers must reverse-engineer it and it can silently drift.

## Goal

Formalize the existing module-report path as a frozen, documented, conformance-tested **Source Producer Protocol v1**, plus a self-test tool external producers can run against watchdog's real decoder.

## Design Decisions

- **Out-of-process socket contract, not in-process plugin** (user-selected). The plugin surface is the Unix-datagram socket + JSON message; no new importable Go API, no dynamic loading, no wire-format change. Freeze what exists.
- **Conformance is executable** (the linchpin). Fixtures are run through watchdog's *real* decoder in a Go test, so the spec cannot drift from the implementation.
- **Ship a validator binary** (user-selected). `cmd/watchdog-report-validate` lets an external producer pipe its JSON and get a pass/fail against the exact decoder — a binary, not a library, so the "no new Go API to import" rule holds.
- **Freeze actual behavior.** Where the prose intent and the decoder's real behavior diverge on an edge case, the executable conformance test encodes the decoder's behavior; a genuine bug is fixed, otherwise the doc documents what the decoder actually does. Divergences are surfaced during implementation, not papered over.

## The v1 Message (as implemented today)

One JSON object per Unix datagram to the module socket (`/run/watchdog/module.sock`; configurable). Fields (from `internal/adapters/module` `incomingReport`):

| Field | JSON | Type | Required | Semantics |
| --- | --- | --- | --- | --- |
| Source ID | `source_id` | string | **yes** | Stable component identity (e.g. `robot-1.main`, `robot-1.drive.left_hip`). Grouping key for health/latch/incident/metrics. |
| Source type | `source_type` | string | no (default `module`) | Selects rule family: `module` (loop timing e.g. `control_period_us`), `drive` (`drive.*`), `ethercat` (`ethercat.*`). |
| Severity | `severity` | string enum | see note | `ok` \| `warn` \| `fail`. Producers report these; `stale` is **watchdog-derived** from `stale_after_ms` expiry, not self-reported. |
| Reason | `reason` | string | no | Human-readable cause. |
| Observed at | `observed_at` | RFC3339 time | no | Producer timestamp; if absent/zero, watchdog uses receipt time. (Distinct from the internal monotonic receipt stamp used for TTL.) |
| Stale after | `stale_after_ms` | integer ms | no (falls back to `sources.module_reports.default_stale_after`) | Watchdog marks the source `stale` if no fresh report arrives within this window. |
| Metrics | `metrics` | object → number | no | Numeric metrics; evaluated under `rules.module` for known keys; exported to `/metrics`. |
| Labels | `labels` | object → string | no | Extra labels for status/metrics. |

The spec doc states the exact accept/reject rules; the conformance test is authoritative for edge cases.

## Compatibility Policy (frozen v1)

- Unknown JSON fields are ignored (forward-compatible; Go decoder default).
- New fields may be added only as **optional, additive**; existing field meaning is immutable.
- Any breaking change requires a new version (`v2`) and a new fixture set; v1 remains supported.
- Malformed reports are dropped and counted (never crash the health path); the doc lists which malformations are rejected.
- Watchdog is optional to the producer: a missing socket or stopped daemon must not block or spam the producer (the SDKs provide no-op/retry-throttle helpers).

## Components

### 1. `docs/source-protocol.md` (normative spec)
Transport + framing + socket path/perms; the field table above with exact required/optional and accept/reject rules; severity + staleness + debounce semantics and how each maps to supervisor actions; `source_type` metric conventions; `source_id` identity rules; the compatibility policy; malformed-report handling; and the producer-optionality guarantee. Links the C++ SDK and the fixtures.

### 2. `sdk/fixtures/source-protocol/v1/`
- `valid/` — `module_minimal.json`, `module_full.json`, `drive.json`, `ethercat.json` (at least these).
- `invalid/` — `missing_source_id.json`, `bad_severity.json`, `wrong_metric_type.json`, `malformed.json` (not JSON).
- `manifest.json` — maps each fixture to expected outcome (`accept`/`reject`) and, for rejects, the documented reason category.

### 3. Executable conformance test
A Go test that loads every fixture, runs it through watchdog's real decode/validate entry point, and asserts the manifest outcome. Placed with the module adapter or in a dedicated `conformance` test package. Failing this test is the drift alarm.

### 4. Validator: `ValidateReport` + `cmd/watchdog-report-validate`
- Export `func ValidateReport(payload []byte) error` from `internal/adapters/module`, factored out of the existing decode path so the daemon and the CLI share one implementation (no divergent second validator).
- `cmd/watchdog-report-validate` reads a JSON payload from a file arg or stdin, calls `ValidateReport`, prints a clear pass/fail message, and exits `0` (valid) / non-zero (invalid). Pure stdlib.

### 5. Docs wiring
Link `docs/source-protocol.md` from README (Contributing/scope area), CONTRIBUTING.md, and `sdk/cpp/README.md`; advance the roadmap marker for the plugin/adapter contract.

## Testing
- The conformance test (component 3) is the primary test and gates drift.
- A small test for `cmd/watchdog-report-validate` (valid fixture → exit 0; invalid fixture → non-zero, message on stderr).
- `ValidateReport` unit tests for each documented reject reason.

## Non-Goals
- No in-process Go plugin interface, no dynamic loading, no exported importable collector API.
- No change to the wire format or transport (freeze as-is).
- No new source_type behavior — this documents and pins existing behavior; new rule families are separate work.

## Acceptance Criteria
- An external developer can implement a conformant producer in any language from `docs/source-protocol.md` alone, and validate their output with `watchdog-report-validate` without reading Go code.
- Every fixture's outcome is asserted against the real decoder; changing decode behavior without updating the spec/fixtures breaks the conformance test.
- `ValidateReport` and the daemon's ingest share one validation implementation (no drift between "what validates" and "what the daemon accepts").
- v1 compatibility policy is documented; unknown fields are ignored; a documented breaking change would require v2.

## Out of Scope / Follow-ups
- A typed Go producer helper package (mirroring the C++ SDK) — deferred unless demand appears.
- Additional `source_type` rule families.
