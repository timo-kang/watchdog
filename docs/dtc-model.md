# Fault-Diagnosis (DTC) Data Model v1

The transport-agnostic data model for the watchdog middleware. JSON is the canonical
serialization; any transport (Unix datagram, ROS2, shared memory) carries these shapes.
The model carries **facts only** — it never encodes or executes an action. The Go binding
is `internal/dtc`; conformance fixtures are `sdk/fixtures/dtc/v1/`, driven through the real
`FaultReport.Validate` by `internal/dtc` tests so this document cannot drift from the code.

Severity levels: `INFO`, `WARN`, `FATAL`.

## FaultReport — the source publication (and heartbeat)

A source publishes its currently-active faults at a steady cadence and immediately on any
transition. **The report doubles as the source's liveness heartbeat:** a consumer that does
not receive a report within `deadline_ms` treats the source as hung — itself a fault. A dead
source cannot report its own death, which is precisely why liveness is inferred from the
absence of its heartbeat.

| Field | JSON | Type | Required | Meaning |
| --- | --- | --- | --- | --- |
| Schema version | `schema_version` | int | **yes** | Must equal `1`. |
| Source ID | `source_id` | string | **yes** | Stable source identity (e.g. `robot-1.raidrive`). |
| Instance | `instance` | string | no | Discriminator when a source runs as multiple instances. |
| Sequence | `sequence` | uint | yes | Monotonic per `(source_id, instance)`. |
| Published at | `published_at` | RFC3339 | yes | Publication timestamp. |
| Deadline | `deadline_ms` | int ms | yes (`0` = liveness disabled) | Hung if no report arrives within this window. |
| Active | `active` | array of Fault | yes (may be empty) | Currently-active faults. |

Acceptance rules (pinned by the conformance suite): reject non-JSON, `schema_version != 1`,
empty `source_id`, negative `deadline_ms`, or any active Fault that fails Fault validation;
accept otherwise.

## Fault — one active DTC

| Field | JSON | Type | Required | Meaning |
| --- | --- | --- | --- | --- |
| Code | `code` | string | **yes** | Catalog code, e.g. `P1310`. |
| Severity | `severity` | string | **yes** | `INFO` \| `WARN` \| `FATAL`. |
| Since | `since` | RFC3339 | yes | When it became active. |
| Units | `units` | array of FaultUnit | no | Affected parts. |
| Detail | `detail` | string | no | Human-readable detail. |

**FaultUnit**: `part` (string, required — stable part identity, e.g. `joint.left_hip`) and
optional `instance`.

## Aggregation, events, and surfaces

- **DiagnosticsReport** — the manager's per-robot aggregate: `robot_id`, `generated_at`,
  `sources` (`SourceLiveness`: `source_id`, `instance`, `last_seen_at`, `alive`), and the
  aggregated `active` faults.
- **DtcEvent** — a fault transition for the event log / recorder: `code`, `severity`,
  `transition` (`opened` | `closed`), `at`, `source_id`, `units`.
- **DiagnosticsEpisode** — the DTC events of one session: `episode_id`, `started_at`,
  `ended_at`, `events`.
- **Notification** — an operator-facing message: `code`, `severity`, `title`, `message`,
  `at`.

## Services

- **GetDiagnosticsHistory** — request: optional `since`/`until`/`codes`/`limit`; response:
  `events` (`[]DtcEvent`).
- **RunSelfCheck** — request: optional `scope`; response: `ran_at` + `faults` (`[]Fault`).

## Versioning & compatibility

- **Frozen v1.** The fields and acceptance rules above are stable.
- Unknown fields are **ignored** (forward-compatible), so a v1 consumer tolerates a producer
  that adds fields.
- New fields may be added only as **optional and additive**; an existing field's meaning
  never changes.
- A change that breaks existing producers requires **v2** (bump `schema_version`) with its
  own fixture set; v1 remains supported.

## Conformance

`sdk/fixtures/dtc/v1/{valid,invalid}/` with `manifest.json` records each case's expected
outcome (and reject reason). `internal/dtc` runs every fixture through `FaultReport.Validate`,
so changing acceptance behavior without updating the spec/fixtures breaks the test.
