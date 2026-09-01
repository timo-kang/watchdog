# DiagnosticsManager

The per-robot aggregator of the fault-diagnosis middleware (`internal/manager`). It consumes
source `FaultReport`s (the M1 model, produced by the M2 reporter), judges liveness, aggregates
active faults into a `DiagnosticsReport`, produces operator notifications, and persists DTC
transition events. It carries **facts only** — it never decides or performs a recovery action.

## Ingestion & discovery

Reports enter through `Ingest(report, receivedAt)` — the transport-agnostic seam. A discovery
adapter (a socket receiver, a shared-memory scanner, …; transports land in M6) simply calls
`Ingest` for each report it observes. `Ingest` validates the report, records it per
`(source_id, instance)`, and emits `opened`/`closed` DTC events for faults that changed since
that source's previous report.

## Per-instance liveness

`FaultReport` doubles as a heartbeat: `Evaluate(now)` marks a source **not alive** when its
last report is older than the report's advertised `deadline_ms` (a dead source cannot report
its own death). A stale source's own faults are dropped — a hung source can't be trusted for
detail — and replaced by a single reserved `source.hung` (`SourceHungCode`, FATAL) fault.
Liveness transitions (hung / recovered) are emitted as DTC events. Liveness is judged **per
instance**, so one hung instance does not mask its healthy siblings.

## Aggregation & notifications

`Evaluate` returns a `DiagnosticsReport` — every source's liveness plus the union of active
faults (deterministically ordered). `Notifications(report, now)` groups active faults by code
and emits at most `NoticeCap` notifications per code (0 = unlimited), highest-severity first,
so a fault affecting many instances does not flood the operator.

## Durable event store

`FileEventSink` persists every DTC event as a durable JSONL session log: one JSON object per
line, each append `fsync`ed so events survive a power cut. `Prune` bounds the rolling history
of old session files via the shared `internal/retention` policy (size + count), never removing
the currently-open session. This is the generalized form of raisin's `dtc_events.csv` /
`dtc_history.csv`.
