# Action Interface

## Objective

Define the production-facing boundary between the local watchdog and the local robot supervisor.

This interface is intentionally separate from:

- high-rate telemetry logging
- module-private APIs
- cloud or fleet services

The watchdog is a detector and recommender. The supervisor is the actuator of policy.

## Delivery Model

Current local delivery path:

1. watchdog detects a transition
2. watchdog writes an incident JSON snapshot locally
3. watchdog logs the transition locally
4. watchdog sends an action request to a Unix datagram socket
5. if the socket is unavailable, watchdog spools the request to disk
6. when the socket becomes available again, watchdog replays spooled requests oldest-first

This gives the local robot a robust path even when the receiver restarts.

## Why Unix Datagram

The current default transport is Unix datagram because it is:

- local-only
- simple to consume from C++, Go, or Python
- independent from HTTP/gRPC stacks
- easy to supervise with `systemd`

The receiver does not need any SDK from this repository. It only needs to:

- bind the configured socket path
- read one JSON object per datagram
- decide what to do with the request

This socket is local to the robot. It is not intended as a remote control surface.

## Schema

Current schema version: `1`

Example:

```json
{
  "schema_version": 1,
  "request_id": "20260415T082100.000000000Z-transition-safe_stop-actuators",
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

Fields:

- `schema_version`: contract version for the payload shape
- `request_id`: stable per-request identifier for dedupe or audit
- `event`: `transition` or `resolved`
- `timestamp`: watchdog snapshot time
- `hostname`: local host identity
- `overall`: current overall watchdog severity
- `previous_overall`: previous overall severity when available
- `requested_action`: `notify`, `degrade`, `safe_stop`, or `resolve`
- `reason`: operator-facing summary
- `incident_path`: local snapshot evidence path when written
- `components`: affected components for transition events
- `resolved_components`: affected components for resolve events
- `errors`: watchdog collector errors seen during the snapshot

## Semantics

Important: `requested_action` is advisory from watchdog policy, not direct authority.

The receiver is expected to:

- validate the request
- apply platform-specific policy
- decide whether to ignore, log, degrade, safe-stop, restart, or escalate

The watchdog must not directly command motors or fieldbuses through this interface.

## Current Built-In Policy

Current default mapping:

- host/process/module/CAN `warn` -> `notify`
- host/process/module/CAN `fail` or `stale` -> `degrade`
- EtherCAT lost slave -> `safe_stop`
- EtherCAT required link down -> `safe_stop`
- other non-OK EtherCAT -> `degrade`
- return to healthy -> `resolve`

This policy is intentionally conservative and local-first.

## Spool Behavior

When the action socket is unavailable:

- the watchdog writes the request to `actions.unix_socket.spool_dir`
- requests are stored as one JSON file per request
- replay is oldest-first by filename order
- replay is bounded by `actions.unix_socket.replay_batch_size` per tick

This means:

- requests are not silently lost on receiver restart
- request ordering is preserved across short outages
- the spool directory becomes part of operational state and should live on persistent storage

## Receiver Expectations

Minimum receiver behavior:

- bind the configured socket path before the watchdog starts sending
- tolerate duplicate `request_id` values across restart/replay scenarios
- treat requests as edge-triggered advisories, not full world state
- use `incident_path` for deeper diagnosis when needed

Recommended receiver behavior:

- dedupe by `request_id`
- journal every handled request
- explicitly separate `degrade` and `safe_stop` handlers
- keep hardware safety below Linux and outside this path

## Supervisor State

The local receiver should maintain a persistent state view, not just per-request logs.

Minimum local state:

- `latest.json`: last handled request plus hook result
- `current_state.json`: current active component actions and last-hook timestamps

Recommended semantics:

- `notify` can be treated as active until a matching `resolve`
- `degrade` and `safe_stop` should be latched
- lower-severity follow-up requests must not silently downgrade a latched higher action
- only an explicit `resolve` request should clear a latched component action
- same-action reminder hooks should be rate-limited by cooldown windows

That means:

- `safe_stop` stays active for a component until a `resolve` clears it
- a later `degrade` for that same component updates audit history but does not replace the active `safe_stop`

## Initial Deployment Without FSM

You do not need a full robot FSM before adopting this interface.

A minimal first receiver can:

- accept watchdog requests on the local Unix socket
- write audit records locally
- keep a latest-state file for operators or tooling
- keep a current-state file with latched component actions
- invoke one hook command per action kind

This repository now includes that companion daemon as `cmd/watchdog-supervisor`.

That gives you a clean stepping stone:

- first: watchdog -> local supervisor -> hook/script/service
- later: replace the hook target with a real robot supervisor or FSM

The interface stays stable while the robot-side policy implementation grows up around it.

## Resource Cost

The local receiver path is intentionally cheap:

- Unix datagram delivery stays inside the same machine
- requests are emitted only on health transitions or resolves, not every poll tick
- spool replay is bounded
- audit writes are one JSON file per handled request

In practice, this should be negligible compared with perception, planning, logging, or fieldbus workloads.

## Local vs Command Center

If you later build an independent watchdog/report/command-center server, keep the boundaries clear:

- local watchdog -> local supervisor: safety-relevant, low-latency, local-only
- local supervisor/watchdog -> command center server: reporting, audit, fleet visibility
- command center server -> robot: optional, explicit, heavily constrained control path

Recommendation:

- do not make the remote server the first consumer of watchdog actions
- do not require network reachability for local safety behavior
- treat the command center as observability and fleet policy infrastructure first

That keeps the robot safe when disconnected and keeps remote control out of the implicit watchdog path.
