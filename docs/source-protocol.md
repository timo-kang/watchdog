# Source Producer Protocol v1

This is the normative contract for producing health reports that `watchdog`
ingests. Any process, in any language, can be a health source by sending
conformant messages — no watchdog SDK or Go dependency is required. The
header-only C++ SDK (`sdk/cpp/`) is one convenience implementation of this
protocol, not a requirement.

Executable examples live in `sdk/fixtures/source-protocol/v1/`. To check a
payload against the exact validator the daemon uses:

```bash
watchdog-report-validate my-report.json      # or: cat my-report.json | watchdog-report-validate
```

## Transport & framing

- One JSON object per **Unix datagram**, sent to the module socket.
- Default socket path: `/run/watchdog/module.sock` (configurable via
  `sources.module_reports.socket_path`).
- There is no reply. The producer must not block waiting on watchdog, and must
  tolerate the socket being absent (watchdog stopped or not installed) without
  erroring or spamming — reporting is best-effort and optional.

## Message schema

| Field | JSON key | Type | Required | Meaning |
| --- | --- | --- | --- | --- |
| Source ID | `source_id` | string | **yes** | Stable identity of the reporting component. Grouping key for health state, action latching, incident history, and metric labels. Keep it stable per component (e.g. `robot-1.main`, `robot-1.drive.left_hip`, `robot-1.ethercat`). |
| Source type | `source_type` | string | no → `module` | Selects the rule family used to evaluate the report. Known: `module`, `drive`, `ethercat`. |
| Severity | `severity` | string | **yes** | One of `ok`, `warn`, `fail`. See the note on `stale` below. |
| Reason | `reason` | string | no | Human-readable cause, shown in status and incidents. |
| Observed at | `observed_at` | string (RFC 3339) | no → receipt time | Producer's timestamp for the observation. If absent or zero, watchdog uses the time it received the datagram. |
| Stale after | `stale_after_ms` | integer (ms) | no → `sources.module_reports.default_stale_after` | If no fresh report for this source arrives within this window, watchdog marks the source `stale`. |
| Metrics | `metrics` | object (string → number) | no | Numeric metrics. Known keys are evaluated under `rules.module`; all are exported to `/metrics`. |
| Labels | `labels` | object (string → string) | no | Extra labels attached to status and metrics. |

### `severity` and `stale`

`severity` is required; an empty or unknown value is rejected. Producers report
`ok`, `warn`, or `fail`. `stale` is **watchdog-derived** from `stale_after_ms`
expiry and should not be self-reported — it means "no fresh report arrived in
time," which only watchdog can determine. (For backward compatibility the
decoder currently also accepts a literal `stale`, but producing it is
discouraged and reserved; do not rely on it.)

## Acceptance rules (normative)

A report is **rejected** (dropped by the daemon, non-zero from the validator)
if any of these hold; otherwise it is **accepted**. These rules are pinned by
the conformance fixtures and test.

- The payload is not a single valid JSON object → reject.
- `source_id` is missing or empty → reject.
- `severity` is missing, empty, or not one of `ok`/`warn`/`fail`/`stale` → reject.
- A field is present with the wrong JSON type (e.g. a `metrics` value that is
  not a number) → reject.

Unknown fields are ignored (see Compatibility). A rejected report never affects
the health or action path; malformed reports are dropped and counted.

## Severity → action semantics

Watchdog evaluates reports and derives component health; the local supervisor
maps health transitions to advisory actions. In the default policy: `warn` →
`notify`; `fail`/`stale` → `degrade`; drive/EtherCAT critical faults → a
`safe_stop` request. Threshold-based promotion (e.g. `control_period_us` over a
configured limit) and debounce/hysteresis (consecutive-poll counts) are applied
by watchdog, not the producer. See the README for the current policy table.

## `source_type` conventions

- `module` — general modules; loop-timing metrics such as `control_period_us`.
- `drive` — actuator drives; `drive.*` metrics (e.g. `drive.motor_temp_c`,
  `drive.current_a`, `drive.fault_code`).
- `ethercat` — fieldbus health; `ethercat.*` metrics (e.g. `ethercat.wkc_ratio`).

Known metric keys under these types are evaluated by watchdog's built-in rules
(`rules.module`); unknown metrics are still exported.

## Compatibility policy

- **Frozen v1.** The fields and acceptance rules above are stable.
- **Unknown fields are ignored**, so a v1 consumer tolerates a producer that
  adds fields — forward compatible.
- New fields may be added only as **optional and additive**; the meaning of an
  existing field never changes.
- A change that would break existing producers requires a new protocol version
  (`v2`) with its own fixture set; v1 remains supported.
- Watchdog is optional to producers: a missing socket or stopped daemon must
  not block or spam the producer. The provided SDKs offer no-op/disable and
  retry-throttle helpers for this.

## Conformance

- Fixtures: `sdk/fixtures/source-protocol/v1/{valid,invalid}/` with
  `manifest.json` recording the expected outcome (and reject reason) per case.
- These are driven through watchdog's real validator in
  `internal/adapters/module` (`TestSourceProtocolV1Conformance`), so this
  document and the implementation cannot silently diverge.
- `watchdog-report-validate` exposes that same validator as a binary for
  producers to self-test.
