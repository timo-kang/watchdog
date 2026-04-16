# Bus Integration

## Objective

Define the minimum platform details needed to wire real CAN and EtherCAT probes into `watchdog` without guessing at robot-specific behavior.

You do not need a marketing brochure or general product sheet. What matters is operational integration detail.

## CAN

The current config model assumes:

- one or more named interfaces such as `can0`
- a backend identifier such as `socketcan`
- an expected bitrate
- optional expected nodes for counting live participants

Current implementation status:

- `socketcan` backend is implemented against Linux `iproute2`
- generic SocketCAN can see interface and controller health, but not logical node liveness by itself
- `command-json` backend is implemented for command-driven or vendor-specific probes

What is needed from the robot platform:

- Linux stack:
  whether the bus is exposed via `SocketCAN`, `CANopen`, a vendor daemon, or a custom API
- interface inventory:
  exact interface names, bitrate, and whether multiple buses exist
- topology expectations:
  expected node IDs, logical names, and whether node presence can be inferred from heartbeats or PDO traffic
- fault semantics:
  what should count as `warn` vs `fail`
  examples: bus-off, repeated restarts, missing drive heartbeat, rising rx/tx errors
- observable sources:
  sample outputs or APIs that expose health
  examples: `ip -details -statistics link show can0`, `candump` excerpts, driver logs, application heartbeat logs

Useful artifacts:

- DBC files if higher-level messages matter
- EDS files if CANopen is involved
- a short map from node IDs to modules
- one sample JSON payload if you want to use `command-json`

## EtherCAT

The current config model assumes:

- one or more named masters such as `master0`
- a backend identifier such as `igh` or `soem`
- an expected master state such as `op`
- an expected slave count

Current implementation status:

- IgH CLI backend is partially implemented via `ethercat slaves` and best-effort `ethercat master` parsing
- state/slave-count collection is in place, but the parser still needs validation against real platform output
- `soem` backend is implemented as a structured probe contract for C++ SOEM masters
- `command-json` backend is implemented for vendor CLIs, middleware, or custom bridge processes

What is needed from the robot platform:

- master stack:
  `IgH`, `SOEM`, vendor runtime, or another control layer
- master inventory:
  how many masters exist and what they are called
- topology expectations:
  expected slave count, slave names or positions, and any critical slaves that should be tracked separately
- fault semantics:
  what should count as `warn` vs `fail`
  examples: `SAFEOP`, working counter drop, slave missing, DC sync drift, link down
- observable sources:
  sample outputs or APIs that expose health
  examples: `ethercat master`, `ethercat slaves -v`, SOEM diagnostics, master logs, application diagnostics

Useful artifacts:

- ESI XML files
- slave inventory by position/alias
- expected cycle time and tolerance
- expected WKC and acceptable degradation band
- one sample JSON payload if you want to use `command-json`
- one sample SOEM snapshot if you want to use `backend: "soem"`

## What To Hand Over

For each real robot platform, the fastest useful handoff is:

1. bus/backend type for CAN and EtherCAT
2. sample command output or API payloads from a healthy system
3. sample output from one failing case
4. expected nodes/slaves and criticality
5. the action policy for each failure mode

With that, the next implementation step is straightforward:

- extend the existing `SocketCAN` backend with node-level visibility or add a higher-level CAN backend
- validate and harden the IgH backend or add another real EtherCAT backend under `internal/adapters/ethercat`
- map platform-specific failures into the existing `component` model

## Command-JSON Contract

`command-json` is the fastest path when the platform already has a reliable health command or library wrapper.

Rules:

- configure `probe_command` as an argv array; the daemon does not use a shell
- command must write exactly one JSON object to stdout
- command should exit non-zero on probe failure
- standardized health fields belong at the top level
- vendor-specific fields should go in `labels` and `metrics`

Minimal CAN payload:

```json
{
  "link_up": true,
  "bitrate": 1000000,
  "online_nodes": 2,
  "online_nodes_known": true,
  "state": "error-active"
}
```

Minimal EtherCAT payload:

```json
{
  "link_known": true,
  "link_up": true,
  "master_state": "op",
  "slaves_seen": 12
}
```

SOEM-oriented payload:

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
    {"position": 1, "name": "imu", "state": "op"},
    {"position": 7, "name": "knee_left", "state": "safeop"},
    {"position": 12, "name": "lidar_sync", "state": "op", "lost": true}
  ]
}
```

When you use `backend: "soem"`, the daemon derives lost/non-operational slave counts and carries the faulted slave positions/names into incidents.
