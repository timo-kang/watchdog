# Milestones

## Objective

Build `watchdog` into a production-grade, open-source edge watchdog for robots and other sensor-dense devices. Success means:

- local modules and buses can report health through stable interfaces
- the daemon can detect stale, degraded, and failed states with explicit policy
- incidents are observable and actionable on-device
- the project is packaged, documented, and governed well enough for outside adopters

## Scope

In:

- host health collection
- local module heartbeat/report ingestion
- bus and network adapters
- incident capture and local action hooks
- packaging, testing, docs, and release process for open-source use

Out:

- hard real-time motor safety loops
- vendor-specific UI dashboards as a required component
- fleet SaaS as a prerequisite for local watchdog value
- replacing low-level drive, BMS, or MCU safety mechanisms

## Assumptions

- Linux is the primary deployment target.
- `systemd` is acceptable as the first production service manager.
- Hard safety remains below Linux user space.
- The first stable producer interface is a local Unix socket, because robot modules already run on the same host.

## Plan

1. `M0 Foundation` `in progress`
   Output:
   - Go daemon skeleton
   - normalized health/status/incident model
   - host collector
   - local module ingest socket
   - first C++ client helper
   - first supervised process input via `systemd`
   - derived component view that joins raw source health by `source_id`
   Exit criteria:
   - daemon runs continuously
   - incidents are written on degraded transitions
   - one C++ module can report health without custom per-project glue
   - one supervised service can surface liveness and restart state
   - one component can combine module and process health into a single operator-facing status

2. `M1 Local Module Health v1`
   Output:
   - protocol v1 documentation
   - per-module freshness policies
   - process identity and restart metadata
   - Unix socket auth/permissions guidance
   - SDK examples for C++ and one scripting language
   Exit criteria:
   - multiple local modules can report independently
   - stale/fail transitions are covered by tests
   - protocol and compatibility policy are documented

3. `M2 Host Runtime Coverage`
   Output:
   - CPU throttling/frequency/power support
   - memory pressure, disk pressure, and network basics
   - process liveness integration
   - richer incident snapshots
   Exit criteria:
   - host degradation causes structured incidents with enough evidence for first-line diagnosis
   - rule coverage exists for the common host failure modes on a robot compute node

4. `M3 Robot Adapters`
   Output:
   - CAN health adapter
   - EtherCAT health adapter
   - network link/jitter adapter
   - sensor freshness adapter contract
   - generic `command-json` bridge for vendor or custom probes
   First concrete targets:
   - `SocketCAN` on Linux
   - one real EtherCAT master stack such as `IgH` or `SOEM`
   Exit criteria:
   - at least one real robot platform can report bus/network/module health through the daemon
   - adapter failures are isolated and observable
   - `command-json` is documented and test-covered for both bus classes
   Current progress:
   - `SocketCAN` backend is in place
   - `IgH` CLI path is partially implemented and awaiting platform validation
   - `SOEM` backend is in place as the preferred command-driven path for C++ SOEM masters
   - `command-json` backend is in place for CAN and EtherCAT

5. `M4 Policy and Actions`
   Output:
   - debounce, hysteresis, and latching
   - configurable actions for log, alert, restart, degrade, and safe-stop requests
   - explicit severity escalation model
   Exit criteria:
   - noisy signals do not flap the platform
   - critical faults can trigger deterministic local actions
   Current progress:
   - local incident writing and transition logging are in place
   - Unix socket action requests are in place for supervisor handoff
   - action requests are durably spooled and replayed when the local receiver is unavailable
   - initial robot-oriented policy exists for notify/degrade/safe-stop recommendations

6. `M5 Open-Source Hardening`
   Output:
   - versioned config schema
   - contributor docs
   - CI, tests, and release artifacts
   - packaging for common Linux targets
   - security policy and support matrix
   Exit criteria:
   - an outside team can build, run, and contribute without private tribal knowledge

7. `M6 Enterprise Extensions`
   Output:
   - policy bundles and deployment profiles
   - audit-friendly incident export
   - optional upstream telemetry integration points
   - compatibility and upgrade guidance
   Exit criteria:
   - multi-product deployments can standardize watchdog behavior without forking the core daemon

## Dependencies

- target robot platforms with Linux/systemd access
- representative CAN/EtherCAT/network environments for adapter testing
- a safe action sink in the robot platform before any autonomous recovery or safe-stop claims

## Risks

- Scope creep into safety-controller territory:
  Keep hard safety explicitly out of scope for this daemon.
- Adapter sprawl before protocol stability:
  Freeze the local module protocol before building many producers.
- Weak open-source adoption due to private assumptions:
  document deployment, compatibility, and support expectations early.
- Overfitting to one robot:
  keep adapter interfaces narrow and platform-neutral.

## Acceptance Criteria

- A local C++ module can report health with a maintained SDK surface.
- The daemon can distinguish `ok`, `warn`, `fail`, and `stale`.
- Incidents include enough machine-readable evidence for automated handling.
- Milestones are tied to observable exit criteria, not vague aspirations.

## Recommendation

Finish `M0` by stabilizing the local module interface and adding process identity/restart metadata to the supervision path. After that, move directly to `M1` protocol documentation and a second producer example before adding bus-specific adapters.
