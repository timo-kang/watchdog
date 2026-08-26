# Watchdog

`watchdog` is becoming a **vendor-neutral, transport-agnostic fault-diagnosis (DTC) &
health-supervision middleware** for robots and sensor-dense edge devices — the reusable
framework for source-side fault reporting, per-robot aggregation with liveness, a neutral
DTC catalog, and a durable event data recorder, so a platform gets a coherent diagnostics
system without hand-rolling one.

> **Direction (pivot in progress).** watchdog was previously an on-robot *advisory
> supervisor* that decided and dispatched `safe_stop`/`degrade` actions. That carried
> authority a detector should not hold, and the concrete design has been proven natively on
> a real robot platform. watchdog is being re-aimed as the *generalized middleware* of that
> pattern. See [`docs/adr/0001-pivot-to-dtc-middleware.md`](docs/adr/0001-pivot-to-dtc-middleware.md).
> The advisory-supervisor code is preserved on the `archive/advisory-supervisor` git tag.
>
> **Design invariant:** the middleware carries *facts* (faults, liveness, evidence). It
> never owns actuation or safety authority — detection and recovery stay with the source
> and platform.

## What ships today (the health-collection foundation)

The current daemon is a read-only health-collection + evidence + metrics agent — the
foundation the middleware components (M1–M6, see Roadmap) build on:

- `watchdog`: polling daemon — collects low-rate health, derives component state, writes
  incident snapshots, exposes Prometheus metrics.
- `watchdog-report-validate`: validates a source report against the daemon's real decoder.
- `watchdog-log-agent`: standalone raw-segment producer used by the sim and bring-up.

Current source families (low-rate, fact-only):

- `host`: CPU temperature, memory, load, hottest sensor
- `module_reports`: JSON reports from local processes (incl. C++ producers) over a Unix
  datagram socket — the **source producer protocol v1** (see below)
- `systemd`: service state, main PID, restart count
- `network`: Linux link state and interface counters
- `power`: Linux `power_supply` state
- `storage`: free space, read-only state, busy percentage
- `time_sync`: `timedatectl` state, RTC drift, sync grace window

Prometheus-compatible `/metrics` is built into the daemon, so the same surface can feed
Prometheus, Grafana, or Datadog OpenMetrics collection.

> Direct EtherCAT/CAN probing and the local action supervisor were removed in the pivot
> (M0): a source that owns the hardware owns its own detection and recovery. Fieldbus and
> actuator health is reported *by the source* via the module-report protocol, not probed by
> watchdog.

## Build

```bash
go build ./...
```

No Go toolchain is needed on the target to run a released binary.

## Source Producer Protocol v1

Any process, in any language, can be a health source by sending conformant JSON reports to
the module socket — no watchdog SDK required. The normative contract, conformance fixtures,
and the `watchdog-report-validate` self-test are documented in
[`docs/source-protocol.md`](docs/source-protocol.md). The header-only C++ SDK
(`sdk/cpp/`) is one convenience implementation.

## Metrics

The daemon exposes Prometheus metrics:

```json
"metrics": { "enabled": true, "listen_address": "127.0.0.1:9108", "path": "/metrics" }
```

Use a loopback bind if Prometheus runs on the robot; bind a real interface only when a
central Prometheus is meant to scrape the robot directly. The repo includes a local
observability stack for the Docker sim under `deploy/observability/`.

## Retention & durability

Forensic writes are power-loss durable: incident snapshots are written with an `fsync` of
both the file and its parent directory, so completed evidence survives a power cut — the
canonical robot incident. A background sweeper bounds the incident directory by byte budget
and file count, always keeping the newest `min_keep`, off the health path:

```json
"retention": { "sweep_interval": "60s", "incidents": { "max_files": 1000, "max_bytes": "64Mi", "min_keep": 50 } },
"sources": { "module_reports": { "report_ttl": "15m" } }
```

Any `max_*` of `0`, or `report_ttl` of `0`, disables that limit. These durability/retention
primitives (`internal/atomicwrite`, `internal/retention`) are the intended basis for the
middleware's Event Data Recorder (M5).

## Simulation

```bash
docker compose -f deploy/docker/docker-compose.sim.yml up --build
# with dashboards:
docker compose -f deploy/docker/docker-compose.sim.yml --profile observability up --build
```

The Docker sim is the supported way to develop and verify without robot hardware.

## Repository layout

- `cmd/watchdog`: daemon entrypoint
- `cmd/watchdog-report-validate`: source-report conformance self-test
- `cmd/watchdog-log-agent`: local raw segment producer
- `internal/adapters`: low-rate collectors (host, module, systemd, network, power, storage, timesync)
- `internal/health`: normalized health model
- `internal/incident`: incident (evidence) persistence
- `internal/rawlog`: incident-to-raw-segment indexing
- `internal/rules`: severity evaluation
- `internal/metrics`: Prometheus collector and endpoint
- `internal/atomicwrite`, `internal/retention`: durable, bounded storage primitives
- `sdk/cpp`: header-only C++ producer SDK
- `configs`, `deploy`, `docs`: examples, deployment, and documentation

## Project Scope & Open-Core

Licensed under Apache-2.0. In the open core (this repo): the middleware framework — data
model, source reporter, aggregation manager, catalog + codegen, event data recorder,
transport adapters, conformance suite, reference implementation. Commercial / not here:
fleet command-center, multi-robot dashboards, cross-fleet aggregation & upload, policy
distribution.

## Roadmap

The middleware pivot is tracked as GitHub milestones **M0–M6**:

- **M0** ✅ Pivot & scope reset — remove excess-authority code, establish the charter (this change)
- **M1** Transport-agnostic fault/DTC data model
- **M2** FaultReporter source library (register/reset + liveness heartbeat)
- **M3** DiagnosticsManager (per-robot aggregation & per-instance liveness)
- **M4** DTC catalog format & codegen
- **M5** Event Data Recorder (durable, bounded — built on `atomicwrite`/`retention`)
- **M6** Transport adapters, reference integration & release

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). The Docker simulator is the supported way to develop
and verify without robot hardware. One rule is absolute: the middleware core never gains
direct actuator/E-stop/power control — it carries facts; the source and platform own
detection and recovery.
