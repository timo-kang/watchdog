#include <cstdlib>
#include <iostream>
#include <string>

#include "watchdog/reporter.hpp"

int main() {
  watchdog::ReporterOptions options;
  options.enabled = false;
  options.source_id = "robot.main";
  options.source_type = "module";
  options.labels["process"] = "robot_main";

  watchdog::Reporter reporter(options);
  watchdog::ReporterSample sample = watchdog::MakeOkSample();
  watchdog::AddMetric(&sample, "control_period_us", 550);
  watchdog::AddLabel(&sample, "mode", "observe_only");

  const watchdog::Report report = reporter.BuildReport(sample);
  std::string error;
  const std::string payload = watchdog::Client::Encode(report, &error);
  if (!error.empty()) {
    std::cerr << error << '\n';
    return EXIT_FAILURE;
  }

  std::cout << payload << '\n';
  return EXIT_SUCCESS;
}
