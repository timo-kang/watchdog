# ADR-0001: Pivot from advisory supervisor to a vendor-neutral DTC middleware

- Status: Accepted
- Date: 2026-08-26

## Context

`watchdog` began as a local-first, on-robot **advisory supervisor**: it collected
health, derived component state, and *decided* advisory actions (`notify` /
`degrade` / `safe_stop` / `resolve`), which a local `watchdog-supervisor` latched
and dispatched to hooks. It also probed EtherCAT/CAN directly.

Two things made that framing wrong:

1. **It held authority it should not.** Deciding `safe_stop`, encoding
   severity→action policy, running a latching supervisor, and executing hooks is
   FSM/safety-layer authority. A detector must not own it. The middleware must
   carry *facts* (faults, liveness, evidence), and let the source/platform own
   detection and recovery where the hardware and context already live.

2. **The target platform built the right thing natively.** The robot platform now
   has a first-party fault-diagnosis (DTC) system: source-side detection in the
   packages that own the hardware (`registerDTC`/`resetDTC`), a per-robot
   diagnostics manager, liveness via a heartbeat (a hung detector self-reports),
   an event data recorder for the window around a fault, and an operator GUI.
   That covers watchdog's detection/aggregation/forensics/GUI mission with the
   correct authority model, so watchdog-as-an-on-robot-component is redundant.

## Decision

Re-aim `watchdog` from an on-robot advisory supervisor to a **vendor-neutral,
transport-agnostic fault-diagnosis (DTC) & health-supervision middleware** — the
generalized, reusable framework of what a platform-specific diagnostics system
proves in the concrete: a source-side FaultReporter (register/reset + liveness
heartbeat), a per-robot DiagnosticsManager, a neutral DTC catalog with codegen,
and a durable Event Data Recorder. Any robotics team can adopt it instead of
hand-rolling their own.

Design invariant: **the middleware never owns actuation or safety authority.** It
carries facts; detection and recovery stay with the source/platform.

## Consequences

- Removed the excess-authority code as the first step (this ADR's companion
  change): `internal/supervisor`, `internal/actions`, the supervisor metrics
  collector, `cmd/watchdog-supervisor`, `cmd/watchdogctl`, and direct EtherCAT/CAN
  probing (`internal/adapters/{ethercat,can}`). Preserved on the
  `archive/advisory-supervisor` git tag.
- Kept the reusable foundation: health model, low-rate collectors (host, systemd,
  network, power, storage, time-sync, module reports), incident writer, Prometheus
  metrics, the module-report **source protocol v1**, and the durability/retention
  primitives (`internal/atomicwrite`, `internal/retention`) — the last two map
  directly onto the future Event Data Recorder.
- The daemon is now a read-only health-collection + evidence + metrics agent with
  no action authority. New middleware components (data model, FaultReporter,
  DiagnosticsManager, catalog, EDR, transports) are tracked as milestones M1–M6.
- Sunk cost from the previous framing is accepted; the reusable residue is the
  durability/retention code and the protocol/conformance discipline.

## Alternatives considered

- **Keep the supervisor, connect its `safe_stop` to the platform FSM.** Rejected:
  it validates an architecture where the detector keeps decision authority it
  should not have, and duplicates the platform's native diagnostics.
- **Delete the repo.** Rejected: the *pattern* (source-side DTC reporting,
  per-robot aggregation with liveness, catalog + codegen, durable EDR) is reusable
  and today every team re-implements it; a clean vendor-neutral middleware is the
  defensible direction.
