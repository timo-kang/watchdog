#include <cstdlib>
#include <iostream>
#include <string>

#include "watchdog/client.hpp"

int main(int argc, char** argv) {
  const std::string socket_path =
      argc > 1 ? argv[1] : "./var/run/watchdog/module.sock";

  watchdog::Client client(socket_path);
  watchdog::Report report;
  report.source_id = "planner";
  report.severity = watchdog::Severity::kWarn;
  report.reason = "deadline miss";
  report.stale_after_ms = 1500;
  report.metrics["deadline_miss_ms"] = 18.5;
  report.metrics["queue_depth"] = 3;
  report.labels["process"] = "planner_main";
  report.labels["node"] = "planner";

  std::string error;
  if (!client.Send(report, &error)) {
    std::cerr << "watchdog send failed: " << error << '\n';
    return EXIT_FAILURE;
  }

  std::cout << "sent watchdog heartbeat to " << client.socket_path() << '\n';
  return EXIT_SUCCESS;
}
