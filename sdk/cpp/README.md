# Watchdog C++ SDK

Header-only C++ helpers for reporting robot module health to the local watchdog
daemon and for writing raw log segment manifests.

## Protocol v1

Module producers send one JSON datagram to the configured Unix datagram socket.
The stable v1 fields are:

- `source_id`: required stable component identity, for example `robot-1.main`
- `source_type`: optional evaluator type, defaults to `module`
- `severity`: `ok`, `warn`, `fail`, or `stale`
- `reason`: operator-facing reason string
- `stale_after_ms`: heartbeat freshness deadline
- `observed_at`: optional RFC3339 timestamp supplied by the producer
- `metrics`: string-to-number map with finite values only
- `labels`: string-to-string map for grouping and operator context

Use `source_type: "ethercat"` with `ethercat.*` metrics when a C++ robot process
is reporting fieldbus health through the module socket.

## Raw Log Segments

Use `watchdog::rawlog::SegmentWriter` when a robot process should write raw
sensor or low-level diagnostic segments directly. It creates a segment file under
`segments/YYYY-MM-DD/` and a manifest v1 JSON file under `manifests/`.

```cpp
#include "watchdog/raw_log.hpp"

watchdog::rawlog::SegmentWriterOptions options;
options.segment_dir = "/var/lib/watchdog/logs/segments";
options.manifest_dir = "/var/lib/watchdog/logs/manifests";
options.source_id = "imu.front";
options.data_type = "imu";

watchdog::rawlog::SegmentWriter writer(options);
std::string error;
if (writer.Open(&error)) {
  writer.WriteLine("{\"seq\":1,\"ax\":0.01}", &error);
  watchdog::rawlog::SegmentManifest manifest;
  writer.Close(&manifest, &error);
}
```

The writer is independent of the watchdog daemon. If watchdog is not installed
or the module socket is absent, raw segment writing still works.

Conformance fixtures:

- `fixtures/module_report.v1.json`
- `fixtures/action_request.v1.json`

## CMake

Build and test the SDK standalone:

```sh
cmake -S sdk/cpp -B /tmp/watchdog-cpp-build
cmake --build /tmp/watchdog-cpp-build
ctest --test-dir /tmp/watchdog-cpp-build --output-on-failure
```

Install for another C++ project:

```sh
cmake --install /tmp/watchdog-cpp-build --prefix /opt/watchdog-cpp
```

Consume from another CMake project:

```cmake
find_package(watchdog_cpp CONFIG REQUIRED)

add_executable(robot_main main.cpp)
target_link_libraries(robot_main PRIVATE watchdog::cpp)
```

Example consumer:

```sh
cmake -S sdk/cpp/examples/cmake_consumer -B /tmp/watchdog-cpp-consumer \
  -DCMAKE_PREFIX_PATH=/opt/watchdog-cpp
cmake --build /tmp/watchdog-cpp-consumer
```

## Optional Integration Rule

Robot processes should treat watchdog reporting as best-effort unless the robot
SKU explicitly requires it. Use `watchdog::Reporter` with retry throttling, call
`TrySend()` from a low-rate path, and avoid logging every send failure.
