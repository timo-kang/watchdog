# Watchdog

`watchdog` is the start of a production-oriented edge watchdog daemon for robots and other sensor-dense edge devices.

This repository is intentionally separate from CI/build-server profiling scripts. The goal here is a long-running service with explicit health state, incident snapshots, and clear interfaces for future adapters such as:

- host thermal/power/memory/load
- process heartbeats from C++ modules
- CAN bus health
- EtherCAT master/slave state
- network/link health
- battery and power-supply state
- storage pressure and IO pressure
- time synchronization and RTC drift
- sensor freshness and module deadlines

## Current Scope

The first scaffold provides:

- a Go daemon entrypoint
- a companion local supervisor entrypoint
- JSON config loading with defaults
- a host adapter that samples:
  - load average
  - memory availability
  - hottest sensor
  - hottest CPU-side sensor
- a rule evaluator with warn/fail thresholds
- a Unix datagram ingest socket for module reports from C++ or other local processes
- a `systemd` adapter for supervised service state and restart counts
- a generic Linux network adapter backed by `/sys/class/net` and `/proc/net/dev`
- a generic Linux power-supply adapter backed by `/sys/class/power_supply`
- a generic Linux storage adapter backed by `statfs`, `/proc/self/mountinfo`, and `/proc/diskstats`
- a generic Linux time-sync adapter backed by `timedatectl show`
- stale detection for missing module heartbeats
- transition logging
- incident snapshot writing on degraded transitions without repeated identical fail storms
- optional Unix-datagram action requests to a local supervisor
- a sample `systemd` unit

This is not yet the full robot watchdog. It is the base control-plane service that later adapters can plug into.

For first real-robot trials, the intended mandatory baseline is:

- host runtime health
- network health
- power or BMS health
- storage pressure
- time synchronization
- module heartbeats
- supervised process state

That gives you infrastructure coverage before adding robot-specific device adapters for gimbals, manipulators, radios, AI accelerators, sensor blocks, and other internal subsystems.

## Repository Layout

- `cmd/watchdog`: binary entrypoint
- `cmd/watchdog-supervisor`: local action receiver and hook dispatcher
- `cmd/watchdogctl`: operator-facing local status command
- `internal/app`: main polling loop
- `internal/adapters`: collector interfaces and concrete adapters
- `internal/actions`: transition logging hooks
- `internal/config`: config loading and validation
- `internal/health`: normalized observation/status/snapshot model
- `internal/incident`: incident snapshot persistence
- `internal/rules`: severity evaluation
- `internal/supervisor`: local receiver, audit log, and hook execution
- `configs`: example config
- `docs`: roadmap and integration notes
- `deploy/systemd`: deployment unit files
- `sdk/cpp`: first C++ producer helper

## Build

```bash
go build ./...
```

For robot deployment, the Go toolchain is only needed on the build machine. The robot does not need `go` installed. Build the binaries locally or in CI, then copy the resulting executables, configs, and service units to the node.

Example local build artifacts:

```bash
mkdir -p dist/linux-amd64
go build -o dist/linux-amd64/watchdog ./cmd/watchdog
go build -o dist/linux-amd64/watchdog-supervisor ./cmd/watchdog-supervisor
go build -o dist/linux-amd64/watchdogctl ./cmd/watchdogctl
```

Typical files to copy to the robot:

- `watchdog`
- `watchdog-supervisor`
- `watchdogctl`
- the config JSON you want to run
- the matching `systemd` unit files under `deploy/systemd/`

If the robot architecture differs from your build machine, cross-compile by setting `GOOS` and `GOARCH` on the build machine before running `go build`.

## Ubuntu 24.04 x86_64 Deploy

The repository now includes a safe production deploy baseline for Ubuntu 24.04 x86_64:

- `configs/watchdog.ubuntu24-amd64.json`
- `configs/watchdog-supervisor.ubuntu24-amd64.json`

Those files use production-style absolute paths:

- `/usr/local/bin/watchdog`
- `/usr/local/bin/watchdog-supervisor`
- `/usr/local/bin/watchdogctl`
- `/etc/watchdog/watchdog.json`
- `/etc/watchdog/watchdog-supervisor.json`
- `/run/watchdog/*.sock`
- `/var/lib/watchdog/...`

The Ubuntu 24.04 config is intentionally safe for first boot:

- enabled by default: `host`, `storage`, `time_sync`, module ingest, supervisor actions
- disabled until you edit real platform names: `systemd`, `network`, `power`, `can`, `ethercat`

That means you can start monitoring immediately on a generic Ubuntu 24.04 x86_64 node without guessing interface or battery names, then turn on the remaining adapters once the robot-specific values are known.

Install example:

```bash
sudo install -d /etc/watchdog
sudo install -m 0755 dist/linux-amd64/watchdog /usr/local/bin/watchdog
sudo install -m 0755 dist/linux-amd64/watchdog-supervisor /usr/local/bin/watchdog-supervisor
sudo install -m 0755 dist/linux-amd64/watchdogctl /usr/local/bin/watchdogctl
sudo install -m 0644 configs/watchdog.ubuntu24-amd64.json /etc/watchdog/watchdog.json
sudo install -m 0644 configs/watchdog-supervisor.ubuntu24-amd64.json /etc/watchdog/watchdog-supervisor.json
sudo install -m 0644 deploy/systemd/watchdog.service /etc/systemd/system/watchdog.service
sudo install -m 0644 deploy/systemd/watchdog-supervisor.service /etc/systemd/system/watchdog-supervisor.service
sudo systemctl daemon-reload
sudo systemctl enable --now watchdog-supervisor watchdog
```

To inspect monitoring after startup:

```bash
sudo journalctl -u watchdog -f
sudo journalctl -u watchdog-supervisor -f
sudo /usr/local/bin/watchdogctl status -config /etc/watchdog/watchdog-supervisor.json
```

On-device outputs land here:

- incidents: `/var/lib/watchdog/incidents/`
- action spool: `/var/lib/watchdog/actions/`
- supervisor state: `/var/lib/watchdog/supervisor/current_state.json`
- latest supervisor record: `/var/lib/watchdog/supervisor/latest.json`
- supervisor audit history: `/var/lib/watchdog/supervisor/requests/`

For a fuller robot bring-up, start from `configs/watchdog.robot-baseline.example.json` and merge in your real:

- `systemd` unit names
- network interface names and minimum speeds
- power-supply or battery names
- CAN interfaces and bitrates
- SOEM probe command path


## Run

```bash
go run ./cmd/watchdog -config ./configs/watchdog.example.json
```

With the example config, the daemon also opens a local Unix datagram socket at `./var/run/watchdog/module.sock`.

The companion local supervisor can run separately:

```bash
go run ./cmd/watchdog-supervisor -config ./configs/watchdog-supervisor.example.json
```

Local status can be inspected with:

```bash
go run ./cmd/watchdogctl status -config ./configs/watchdog-supervisor.example.json
```

For a first robot-oriented baseline, start from `configs/watchdog.robot-baseline.example.json` and adapt the interface names, power-supply names, service names, and bus probes to your platform before deployment.

Release artifacts publish a `linux-amd64` bundle with the binaries, Ubuntu 24.04 x86_64 configs, and `systemd` unit files ready to copy onto the target machine.

## Roadmap

The project milestones live in [`docs/milestones.md`](docs/milestones.md). Treat that file as the contract for what counts as `M0`, `M1`, and later production/open-source readiness.

The bus integration handoff checklist lives in [`docs/bus-integration.md`](docs/bus-integration.md).

The watchdog-to-supervisor contract lives in [`docs/action-interface.md`](docs/action-interface.md).

## Mandatory Infrastructure Adapters

The first non-bus robot baseline should turn on these source families:

- `network`: link state, interface speed, RX/TX counters, error deltas, and drop deltas
- `power`: battery or PSU presence, online state, capacity, temperature, and supply health when the kernel exports them
- `storage`: filesystem free space, read-only state, inode pressure, and disk busy percentage when device stats are available
- `time_sync`: `timedatectl`-based NTP state, RTC drift, `LocalRTC` misconfiguration, and a configurable synchronization grace window

Those adapters are intentionally Linux-generic. They are meant to get you through the first robot bring-up safely. After real-robot testing, add deeper robot-specific adapters where the failures actually show up, for example:

- BMS or power-rail probes beyond generic `power_supply`
- modem or multi-link network module probes beyond interface counters
- sensor freshness and packet-age producers
- manipulator, gimbal, or AI compute module health producers
- vendor-specific storage or accelerator telemetry

## Module Health Ingest

Local modules report health by sending one JSON datagram per heartbeat:

```json
{
  "source_id": "planner",
  "severity": "warn",
  "reason": "deadline miss",
  "stale_after_ms": 1500,
  "metrics": {
    "deadline_miss_ms": 18.5
  },
  "labels": {
    "process": "planner_main"
  }
}
```

Fields:

- `source_id`: stable module identifier
- `severity`: `ok`, `warn`, `fail`, or `stale`
- `reason`: operator-facing summary for the current state
- `stale_after_ms`: optional per-module freshness deadline; otherwise the daemon uses config default
- `observed_at`: optional RFC3339 timestamp if the producer wants to set event time explicitly
- `metrics` and `labels`: optional structured context carried into incidents and logs

Reusable C++ client helper:

```cpp
#include "watchdog/client.hpp"

watchdog::Client client("./var/run/watchdog/module.sock");
watchdog::Report report;
report.source_id = "planner";
report.severity = watchdog::Severity::kOk;
report.reason = "healthy";
report.stale_after_ms = 1500;

std::string error;
if (!client.Send(report, &error)) {
  // handle error
}
```

Buildable example:

```bash
g++ -std=c++17 -I./sdk/cpp/include ./sdk/cpp/examples/send_heartbeat.cpp -o /tmp/watchdog-send-example
/tmp/watchdog-send-example ./var/run/watchdog/module.sock
```

This is intentionally narrow: local heartbeat transport first, richer adapters next.

## Local Demo

For a clean simulated-node demo, use `configs/watchdog.local-demo.example.json`. That config:

- disables host health sampling to avoid unrelated machine noise
- enables module heartbeat ingest
- enables watchdog-to-supervisor action delivery

Run it in three terminals:

```bash
# terminal 1
go run ./cmd/watchdog-supervisor -config ./configs/watchdog-supervisor.example.json

# terminal 2
go run ./cmd/watchdog -config ./configs/watchdog.local-demo.example.json

# terminal 3
g++ -std=c++17 -I./sdk/cpp/include ./sdk/cpp/examples/send_heartbeat.cpp -o /tmp/watchdog-send-example
/tmp/watchdog-send-example ./var/run/watchdog/module.sock
go run ./cmd/watchdogctl status -config ./configs/watchdog-supervisor.example.json
```

Important:

- the example sender sends one heartbeat only
- after `stale_after_ms`, that component will become `stale` and then `degrade`
- a real simulated robot node should keep sending heartbeats periodically

With a real module or simulator process, point its local watchdog client at `./var/run/watchdog/module.sock` and emit one JSON heartbeat per control/report cycle or at a bounded health interval.

## Docker Sim

For a quick end-to-end simulation without installing the binaries onto a robot, use the Docker compose stack in `deploy/docker/docker-compose.sim.yml`.

What it runs:

- `watchdog-supervisor`
- `watchdog`
- `planner-sim`, which sends 5 `warn` heartbeats and then exits

That makes the stack demonstrate the real sequence:

1. module reports `warn`
2. watchdog stays fresh while heartbeats arrive
3. simulator exits
4. watchdog marks the module `stale`
5. supervisor latches `degrade`

Run it:

```bash
docker compose -f deploy/docker/docker-compose.sim.yml up --build
```

Inspect it from another terminal:

```bash
docker compose -f deploy/docker/docker-compose.sim.yml logs -f watchdog watchdog-supervisor planner-sim
docker compose -f deploy/docker/docker-compose.sim.yml exec watchdog-supervisor /usr/local/bin/watchdogctl status -config /configs/watchdog-supervisor.docker-sim.json
```

Expected `watchdogctl` outcome after the simulator stops and `stale_after` expires:

- `Overall: degrade`
- active component `planner`
- state `stale -> degrade [latched]`

Tear it down and remove volumes:

```bash
docker compose -f deploy/docker/docker-compose.sim.yml down -v
```

The sim configs are:

- `configs/watchdog.docker-sim.json`
- `configs/watchdog-supervisor.docker-sim.json`

Those intentionally disable host, storage, time-sync, and bus adapters so the simulation focuses on the module-to-watchdog-to-supervisor flow first.


## Process Supervision

The first process supervision path is a `systemd` collector. It polls configured units and emits `process` observations with:

- `active_state` / `sub_state`
- `load_state`
- `ExecMainPID`
- `NRestarts`
- `InvocationID`

Example config:

```json
"systemd": {
  "enabled": true,
  "units": [
    {
      "name": "planner.service",
      "source_id": "planner",
      "require_main_pid": true
    }
  ]
}
```

There is also a runnable sample config at `configs/watchdog.systemd.example.json`.

If `source_id` matches the module heartbeat `source_id`, the snapshot will show both the module-level health report and the supervised service state for the same logical component.

## Component View

Snapshots and incidents now contain both:

- raw `statuses`: one row per collected source
- derived `components`: one row per logical component grouped by `source_id`

That means `planner` can carry:

- a `module` status from heartbeat freshness and self-reported health
- a `process` status from `systemd`
- one derived `component` severity for operator-facing handling

The component view is the primary surface for actions and incident review. Raw statuses remain in the snapshot for debugging and root-cause analysis.

## Action Reporting

Current reporting path:

- always log transitions through `journald`/stdout
- always write incident JSON on degraded transitions
- optionally emit a structured action request to a local supervisor over a Unix datagram socket

The action sink is configured under:

```json
"actions": {
  "unix_socket": {
    "enabled": true,
    "socket_path": "./var/run/watchdog/supervisor.sock",
    "send_resolved": true,
    "spool_dir": "./var/spool/watchdog/actions",
    "replay_batch_size": 64
  }
}
```

The daemon does not need a C++ receiver contract from this repo. Any local supervisor that can bind a Unix datagram socket and decode JSON can consume the requests.

If the supervisor socket is down, the watchdog now spools action requests to disk and replays them oldest-first when the socket returns.

This transport is local-only. The intended path is:

- watchdog process on the robot
- Unix datagram socket on the same robot
- local supervisor process on the same robot

It is not a remote socket and it does not depend on the network.

Current built-in policy:

- `warn` from host/process/module/CAN: `notify`
- `fail` or `stale` from host/process/module/CAN: `degrade`
- EtherCAT lost slave or required link down: `safe_stop`
- other non-OK EtherCAT states: `degrade`
- recovery back to healthy: `resolve`

Action request payload:

```json
{
  "schema_version": 1,
  "event": "transition",
  "timestamp": "2026-04-15T08:21:00Z",
  "hostname": "robot-1",
  "overall": "fail",
  "previous_overall": "warn",
  "requested_action": "safe_stop",
  "reason": "safe_stop requested for actuators: ethercat fail: lost slave 12",
  "incident_path": "/var/lib/watchdog/incidents/20260415T082100Z_fail.json",
  "components": [
    {
      "component_id": "actuators",
      "severity": "fail",
      "requested_action": "safe_stop",
      "reason": "ethercat fail: lost slave 12",
      "source_types": ["ethercat"]
    }
  ]
}
```

This is intentionally a request, not direct motor control. The robot supervisor remains the place that decides whether to degrade autonomy, safe-stop locomotion, restart a service, or just log.

### Local Supervisor

This repo now includes a minimal companion receiver: `watchdog-supervisor`.

Its job is intentionally narrow:

- bind the local action socket
- validate and dedupe requests by `request_id`
- write per-request audit JSON
- keep a `latest.json` view of the most recent request
- keep a persistent `current_state.json` view of active component actions
- latch `degrade` and `safe_stop` until an explicit `resolve` request clears them
- suppress same-action reminder hooks inside configurable cooldown windows
- optionally run one executable hook for `notify`, `degrade`, `safe_stop`, or `resolve`

This is enough to start integrating with a robot platform even if you do not have a full supervisor FSM yet. In the first deployment, the hook can call a small script or existing service that logs, raises a local alert, or requests a degraded mode from the platform.

Example supervisor config:

```json
{
  "socket_path": "./var/run/watchdog/supervisor.sock",
  "audit_dir": "./var/lib/watchdog/supervisor/requests",
  "latest_path": "./var/lib/watchdog/supervisor/latest.json",
  "state_path": "./var/lib/watchdog/supervisor/current_state.json",
  "hook_timeout": "5s",
  "cooldowns": {
    "notify": "30s",
    "degrade": "15s",
    "safe_stop": "5s",
    "resolve": "5s"
  },
  "hooks": {
    "notify": [],
    "degrade": [],
    "safe_stop": [],
    "resolve": []
  }
}
```

`current_state.json` is the operator-facing local truth for what the supervisor currently believes is active. It keeps the highest latched action per component, so a component that reached `safe_stop` will stay there until a `resolve` event clears it. Lower-severity follow-up requests update audit history but do not silently downgrade the active state.

Example systemd unit:

- `deploy/systemd/watchdog.service`
- `deploy/systemd/watchdog-supervisor.service`

Resource cost is intentionally low. The socket only carries transition events, not high-rate telemetry, so the steady-state cost is a small amount of local IPC, periodic polling in the watchdog, and bounded file writes for incidents/audit.

For the full delivery contract and the recommended boundary to any future command-center server, see `docs/action-interface.md`.

## Bus Adapters

The repository now has explicit source schemas and placeholder collectors for:

- CAN
- EtherCAT

Current status:

- CAN: real Linux `SocketCAN` probe using `ip -details -statistics link show`
- EtherCAT: partial IgH CLI backend for slave/state collection, pending validation on real platform output
- EtherCAT: first-class `soem` backend that consumes a structured probe from the C++ master process
- CAN and EtherCAT: generic `command-json` backend for vendor tools, custom daemons, or bridge scripts

The config model now supports:

```json
"can": {
  "enabled": false,
  "backend": "socketcan",
  "interfaces": [
    {
      "name": "can0",
      "source_id": "drive-can",
      "expected_bitrate": 1000000,
      "require_up": true
    }
  ]
},
"ethercat": {
  "enabled": false,
  "backend": "igh",
  "masters": [
    {
      "name": "master0",
      "source_id": "actuators",
      "expected_state": "op",
      "expected_slaves": 12,
      "require_link": true
    }
  ]
}
```

For custom or vendor-specific probes, each interface or master can also use `backend: "command-json"` with an explicit argv array under `probe_command`. A second example lives in `configs/watchdog.command.example.json`.

If your robot uses `SOEM`, prefer `backend: "soem"` with a `probe_command` that prints one SOEM status JSON object. A ready-to-fill config lives in `configs/watchdog.soem.example.json`, and a small C++ encoder helper lives in `sdk/cpp/include/watchdog/ethercat_probe.hpp`.

The command must print one JSON object to stdout and exit `0`. The daemon does not invoke a shell.

CAN command contract:

```json
{
  "collected_at": "2026-04-14T15:00:00Z",
  "link_up": true,
  "bitrate": 1000000,
  "online_nodes": 2,
  "online_nodes_known": true,
  "rx_errors": 0,
  "tx_errors": 1,
  "bus_off_count": 0,
  "restart_count": 2,
  "state": "error-active",
  "labels": {
    "probe": "vendor-can"
  },
  "metrics": {
    "can.vendor_heartbeat_gap_ms": 12.5
  }
}
```

EtherCAT command contract:

```json
{
  "collected_at": "2026-04-14T15:01:00Z",
  "link_known": true,
  "link_up": true,
  "master_state": "op",
  "slaves_seen": 12,
  "slave_errors": 0,
  "working_counter": 120,
  "working_counter_expected": 120,
  "labels": {
    "probe": "vendor-ethercat"
  },
  "metrics": {
    "ethercat.dc_drift_us": 4.25
  }
}
```

SOEM backend contract:

```json
{
  "interface": "enp3s0",
  "link_known": true,
  "link_up": true,
  "master_state": "op",
  "slaves_seen": 12,
  "working_counter": 120,
  "working_counter_expected": 120,
  "slaves": [
    {
      "position": 1,
      "name": "imu",
      "state": "op"
    },
    {
      "position": 7,
      "name": "knee_left",
      "state": "safeop"
    },
    {
      "position": 12,
      "name": "lidar_sync",
      "state": "op",
      "lost": true
    }
  ]
}
```

The daemon derives:

- `ethercat.slaves_lost`
- `ethercat.slaves_not_op`
- `ethercat.slaves_faulted`
- `faulted_slave_positions` / `faulted_slave_names`

The bundled C++ example at `sdk/cpp/examples/emit_soem_probe.cpp` shows the output shape, and `watchdog/ethercat_probe.hpp` includes `SOEMStateToString(...)` plus `AddSOEMSlave(...)` so your platform code can map `ec_slave[]` data without hand-building JSON.

For a tighter integration path, `watchdog/ethercat_probe.hpp` also provides `SOEMMasterSnapshot`, `SOEMSlaveSnapshot`, `BuildProbeReportFromSOEM(...)`, and `ValidateProbeReport(...)`. That is the intended SDK surface for a real `/usr/local/bin/watchdog-soem-probe` binary in your robot platform.

Use top-level fields for the standardized metrics the rule engine already understands. Use `labels` and `metrics` for extra context that should flow into snapshots and incidents.

What is still needed for real adapters is not a generic spec sheet, but platform evidence:

- stack/backend type
- expected topology
- healthy and failing command output or API payloads
- fault-to-action policy

That handoff is described in `docs/bus-integration.md`.

## Design Direction

The intended production shape is:

1. hardware/device watchdogs below Linux for hard safety
2. this daemon for host-level health aggregation and policy
3. adapters for robot-specific buses, modules, and networks
4. explicit safe-stop/degrade actions integrated with the robot platform

The daemon is deliberately written in Go so deployment to robots is a single compiled binary rather than a language runtime rollout. C++ modules do not need to link against Go or embed a Go runtime; they only write heartbeat datagrams to the local socket.
