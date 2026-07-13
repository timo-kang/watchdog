# Contributing to Watchdog

Thanks for your interest in contributing. `watchdog` is a local-first health
watchdog and self-healing supervisor for C++/EtherCAT robots and sensor-dense
edge devices. This guide gets you productive without needing robot hardware.

## Scope

This repository is the **open-source local watchdog stack** (see
[Project Scope & Open-Core](README.md#project-scope--open-core) in the README).
Contributions to the local detection, incident capture, supervisor, adapters,
C++ SDK, packaging, and docs are welcome. Fleet/command-center features are out
of scope here — please open a discussion before proposing anything in that area.

Because watchdog runs next to real robots, one rule is absolute: **the generic
core never gains direct actuator, drive-enable, E-stop, or power control.**
Watchdog detects, records, and *requests* actions; the robot FSM keeps final
safety authority. PRs that cross that boundary will be declined regardless of
quality.

## Development setup

You need the Go toolchain (see `go.mod` for the version) and, for SDK work, a
C++17 compiler and CMake ≥ 3.16. No robot hardware is required.

```bash
git clone https://github.com/timo-kang/watchdog.git
cd watchdog
go build ./...
```

## The simulator is your on-ramp

You do **not** need EtherCAT hardware or a real robot to develop or verify most
changes. The Docker sim runs the full stack (watchdog + supervisor + a producer)
plus an optional Prometheus/Grafana observability stack:

```bash
docker compose -f deploy/docker/docker-compose.sim.yml up --build
# with dashboards:
docker compose -f deploy/docker/docker-compose.sim.yml --profile observability up --build
```

For a pure-Go local run without Docker, start from
`configs/watchdog.local-demo.example.json`. Fault-injection scenarios (stale
heartbeat, loop-period breach, degraded bus report, safe-stop latch) can all be
exercised against the sim — this is the expected way to develop and demonstrate
behavior when you don't have the physical bus.

## Before you open a PR

Run the same gates CI enforces, and make sure they're green:

```bash
gofmt -l $(git ls-files '*.go')   # must print nothing
go vet ./...
go test -race ./...
go build ./...
GOOS=linux GOARCH=arm64 go build ./...   # arm64 is a first-class target
```

For C++ SDK changes:

```bash
cmake -S sdk/cpp -B build/sdk-cpp -DWATCHDOG_CPP_BUILD_TESTS=ON -DWATCHDOG_CPP_BUILD_EXAMPLES=ON
cmake --build build/sdk-cpp --parallel
ctest --test-dir build/sdk-cpp --output-on-failure
```

## Conventions

- **Stdlib-first.** The daemon has exactly one external Go dependency
  (`prometheus/client_golang`). Do not add dependencies without discussing it
  first — small footprint is a feature on edge devices.
- **Header-only C++ SDK.** The SDK must stay linkable into proprietary realtime
  C++ without pulling in a Go runtime. Keep it header-only and dependency-free.
- **Follow existing patterns.** Match the surrounding code's naming, error
  wrapping (`fmt.Errorf("context: %w", err)`), and structure. IO lives at the
  edges; `rules`/`health` stay pure.
- **Tests verify real behavior.** Use `t.TempDir()`, real Unix sockets, and
  helper processes as the existing tests do — not mocks. New behavior needs a
  test; keep test output pristine.
- **Backward compatibility.** Config changes must keep existing configs loading;
  add fields with sane defaults rather than requiring them.

## Pull requests

1. Branch from `main` (e.g. `feat/…`, `fix/…`, `docs/…`).
2. Keep the change focused; write a clear description of what and why.
3. Ensure the gates above pass — CI (`.github/workflows/ci.yml`) runs them on
   every PR and must be green.
4. Sign your commits off (DCO): add `Signed-off-by: Your Name <email>` to each
   commit (`git commit -s`). By signing off you certify the
   [Developer Certificate of Origin](https://developercertificate.org/).
5. A maintainer reviews for correctness, the safety boundary, and fit with the
   project scope.

## Reporting issues

Use GitHub issues for bugs and feature requests. For anything touching the
safety/advisory boundary or a potential security issue, please describe the
scenario clearly and, for security, avoid posting exploit detail in a public
issue — contact the maintainers first.
