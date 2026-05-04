# Watchdog

`watchdog` is a local-first health watchdog for robots and other sensor-dense edge devices.

It is meant to run on the robot, not in the cloud. It polls local health sources, writes incident snapshots, and sends structured action requests to a local supervisor. Remote dashboards and fleet systems can be added later, but they are not part of the safety-critical path.

## What Ships Today

Current binaries:

- `watchdog`: polling daemon
- `watchdog-supervisor`: local action receiver and latch
- `watchdogctl`: operator status tool

Current built-in source families:

- `host`: CPU temperature, memory, load, hottest sensor
- `module_reports`: JSON heartbeats from local modules, including C++ producers
- `systemd`: service state, main PID, restart count
- `network`: Linux link state and interface counters
- `power`: Linux `power_supply` state
- `storage`: free space, read-only state, busy percentage
- `time_sync`: `timedatectl` state, RTC drift, sync grace window
- `can`: SocketCAN and command-based probes
- `ethercat`: SOEM, partial IgH, and command-based probes

Current local action policy:

- `warn` -> `notify`
- `fail` or `stale` -> `degrade`
- EtherCAT lost slave or required link down -> `safe_stop`
- recovery -> `resolve`

Prometheus-compatible `/metrics` endpoints are now built into both `watchdog` and `watchdog-supervisor`, so the same surface can feed Prometheus, Grafana, or Datadog OpenMetrics collection.

## Install From Release

The published Linux target today is Ubuntu 24.04 x86_64.

Download the latest release asset:

- `watchdog-v<version>-ubuntu24-amd64.tar.gz`
- `watchdog-v<version>-ubuntu24-amd64.tar.gz.sha256`

Install it on the robot:

```bash
tar -xzf watchdog-v<version>-ubuntu24-amd64.tar.gz
cd watchdog-v<version>-ubuntu24-amd64

sudo install -d /etc/watchdog
sudo install -m 0755 bin/watchdog /usr/local/bin/watchdog
sudo install -m 0755 bin/watchdog-supervisor /usr/local/bin/watchdog-supervisor
sudo install -m 0755 bin/watchdogctl /usr/local/bin/watchdogctl
sudo install -m 0644 configs/watchdog.json /etc/watchdog/watchdog.json
sudo install -m 0644 configs/watchdog-supervisor.json /etc/watchdog/watchdog-supervisor.json
sudo install -m 0644 systemd/watchdog.service /etc/systemd/system/watchdog.service
sudo install -m 0644 systemd/watchdog-supervisor.service /etc/systemd/system/watchdog-supervisor.service
sudo systemctl daemon-reload
sudo systemctl enable --now watchdog-supervisor watchdog
```

The robot does not need the Go toolchain installed.

## Build From Source

```bash
go build ./...
```

Build release-style binaries locally:

```bash
mkdir -p dist/linux-amd64
go build -o dist/linux-amd64/watchdog ./cmd/watchdog
go build -o dist/linux-amd64/watchdog-supervisor ./cmd/watchdog-supervisor
go build -o dist/linux-amd64/watchdogctl ./cmd/watchdogctl
```

## First Config To Use

For a real Ubuntu 24.04 x86_64 node, start from:

- `configs/watchdog.ubuntu24-amd64.json`
- `configs/watchdog-supervisor.ubuntu24-amd64.json`

That baseline is intentionally conservative:

- enabled by default: `host`, `storage`, `time_sync`, module ingest, supervisor actions
- disabled until you fill real platform values: `systemd`, `network`, `power`, `can`, `ethercat`

For a fuller robot bring-up, use:

- `configs/watchdog.robot-baseline.example.json`

Edit these first:

- `systemd.units`
- `network.interfaces`
- `power.supplies`
- `can.interfaces`
- `ethercat.masters`
- `sources.time_sync.require_synchronized`
- `sources.time_sync.sync_grace_period`

## Runtime Layout

Config:

- `/etc/watchdog/watchdog.json`
- `/etc/watchdog/watchdog-supervisor.json`

Runtime sockets:

- `/run/watchdog/module.sock`
- `/run/watchdog/supervisor.sock`

Persistent state and incidents:

- `/var/lib/watchdog/incidents/`
- `/var/lib/watchdog/actions/`
- `/var/lib/watchdog/supervisor/current_state.json`
- `/var/lib/watchdog/supervisor/latest.json`
- `/var/lib/watchdog/supervisor/requests/`

Service logs:

- `journalctl -u watchdog`
- `journalctl -u watchdog-supervisor`

Metrics endpoints:

- `watchdog`: `127.0.0.1:9108/metrics`
- `watchdog-supervisor`: `127.0.0.1:9109/metrics`

If Grafana is running on a different machine, these loopback binds are not reachable from it. In that case, use the `remote-metrics` example configs and scrape the robot's real IP or hostname instead.

## How To Inspect It

Live logs:

```bash
sudo journalctl -u watchdog -f
sudo journalctl -u watchdog-supervisor -f
```

Operator view:

```bash
watchdogctl status -config /etc/watchdog/watchdog-supervisor.json
watchdogctl status -config /etc/watchdog/watchdog-supervisor.json -verbose
watchdogctl status -config /etc/watchdog/watchdog-supervisor.json -json -verbose
```

Important files:

```bash
sudo jq . /var/lib/watchdog/supervisor/current_state.json
sudo jq . /var/lib/watchdog/supervisor/latest.json
sudo ls -lt /var/lib/watchdog/incidents
```

## Prometheus and Grafana

Both processes can expose Prometheus-compatible metrics:

```json
"metrics": {
  "enabled": true,
  "listen_address": "127.0.0.1:9108",
  "path": "/metrics"
}
```

Use loopback if Prometheus runs on the robot. Use a real interface bind such as `0.0.0.0:9108` only when a central Prometheus server is meant to scrape the robot directly.

The repository includes a local observability stack for the Docker sim:

- `deploy/observability/prometheus/prometheus.docker-sim.yml`
- `deploy/observability/grafana/provisioning/...`
- `deploy/observability/grafana/dashboards/watchdog-overview.json`

Run it with the simulator:

```bash
docker compose -f deploy/docker/docker-compose.sim.yml --profile observability up --build
```

Then open:

- Prometheus: `http://localhost:9091`
- Grafana: `http://localhost:3300`
  - login: `admin`
  - password: `admin`

The provisioned Grafana dashboard is `Watchdog Overview`.

### Central Prometheus Scraping Real Robots

For a monitoring server or laptop that scrapes one or more robots directly:

1. On each robot, make the metrics endpoints reachable.
   Start from:
   - `configs/watchdog.ubuntu24-amd64.remote-metrics.example.json`
   - `configs/watchdog-supervisor.ubuntu24-amd64.remote-metrics.example.json`

2. Install those as:
   - `/etc/watchdog/watchdog.json`
   - `/etc/watchdog/watchdog-supervisor.json`

3. Restart the services:

```bash
sudo systemctl restart watchdog-supervisor watchdog
```

4. From the monitoring server, verify basic reachability before opening Grafana:

```bash
curl http://ROBOT_IP:9108/metrics
curl http://ROBOT_IP:9109/metrics
```

If Prometheus runs in Docker on the same Linux machine as the robot processes, use `host.docker.internal:9108` and `host.docker.internal:9109` in the Prometheus target list. The provided `docker-compose.server.yml` already maps `host.docker.internal` to the host gateway.

5. Run the central observability stack:

```bash
cd deploy/observability
$EDITOR prometheus/prometheus.robot-server.example.yml
docker compose -f docker-compose.server.yml up -d
```

Then open:

- Prometheus: `http://SERVER_IP:9091`
- Grafana: `http://SERVER_IP:3300`

If you see "no data" in Grafana, the first checks should be:

```bash
curl http://ROBOT_IP:9108/metrics
curl http://ROBOT_IP:9109/metrics
curl http://SERVER_IP:9091/api/v1/targets
```

The common failure cases are:

- Prometheus is still scraping the Docker sim targets instead of the robot IPs
- robot metrics are still bound to `127.0.0.1`
- firewall or network policy blocks `9108` or `9109`
- Prometheus target entries do not match the robot address

## Time Sync Behavior

`time_sync` now has a configurable grace window before an unsynchronized clock becomes a hard failure.

Config:

```json
"time_sync": {
  "enabled": true,
  "source_id": "system-clock",
  "require_synchronized": true,
  "warn_on_local_rtc": true,
  "sync_grace_period": "10m"
}
```

Behavior:

- during the grace window: `warn`
- after the grace window expires: `fail`
- incident snapshots are written on state transitions, not every repeated poll

Use this intentionally:

- if the robot must eventually sync, keep `require_synchronized=true` and tune `sync_grace_period`
- if the robot is expected to run without synchronized time, set `require_synchronized=false`

## Module Heartbeats

Local modules send one JSON datagram per heartbeat to `module.sock`.

Minimal example payload:

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

C++ helper code is in:

- `sdk/cpp/include/watchdog/client.hpp`
- `sdk/cpp/examples/send_heartbeat.cpp`

SOEM helper code is in:

- `sdk/cpp/include/watchdog/ethercat_probe.hpp`
- `sdk/cpp/examples/emit_soem_probe.cpp`

## Simulation

Local demo config:

- `configs/watchdog.local-demo.example.json`

Docker simulation stack:

- `deploy/docker/docker-compose.sim.yml`

Run the Docker sim:

```bash
docker compose -f deploy/docker/docker-compose.sim.yml up --build
```

Inspect it:

```bash
docker compose -f deploy/docker/docker-compose.sim.yml logs -f watchdog watchdog-supervisor planner-sim
docker compose -f deploy/docker/docker-compose.sim.yml exec watchdog-supervisor /usr/local/bin/watchdogctl status -config /configs/watchdog-supervisor.docker-sim.json
```

## Repository Layout

- `cmd/watchdog`: daemon entrypoint
- `cmd/watchdog-supervisor`: local receiver and hook dispatcher
- `cmd/watchdogctl`: status CLI
- `cmd/watchdog-sim-module`: simulation producer
- `internal/adapters`: collectors
- `internal/actions`: action request building and delivery
- `internal/config`: config loading and validation
- `internal/health`: normalized health model
- `internal/incident`: incident persistence
- `internal/rules`: severity evaluation
- `internal/supervisor`: local supervisor state and hook execution
- `deploy/systemd`: unit files
- `deploy/docker`: simulation stack
- `configs`: example configs
- `docs`: roadmap and interface notes

## More Docs

- `docs/milestones.md`: project milestones
- `docs/bus-integration.md`: CAN and EtherCAT integration handoff
- `docs/action-interface.md`: watchdog to supervisor contract
- `docs/observability.md`: metrics, Prometheus, Grafana, and dashboard notes

## Current Boundaries

This is a local watchdog stack, not yet a full robot control-plane product.

What it already does well:

- local health polling
- component-level state derivation
- incident snapshot writing
- supervisor latching and audit
- C++ heartbeat integration
- baseline host, storage, time, network, power, CAN, and EtherCAT inputs

What still belongs outside this repo:

- hard real-time actuator safety
- final robot FSM and autonomy policy
- fleet dashboards and remote command center
- vendor-specific telemetry for every module on the robot
