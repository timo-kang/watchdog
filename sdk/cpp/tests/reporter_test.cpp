#include <cstdlib>
#include <chrono>
#include <iostream>
#include <limits>
#include <string>

#include "watchdog/reporter.hpp"

namespace {

bool TestBuildReportMergesBaseAndSampleLabels() {
  watchdog::ReporterOptions options;
  options.source_id = "robot.main";
  options.source_type = "module";
  options.stale_after_ms = 1500;
  options.labels["process"] = "robot_control_node";
  options.labels["robot"] = "generic_robot";

  watchdog::Reporter reporter(options);
  watchdog::ReporterSample sample = watchdog::MakeOkSample();
  watchdog::AddMetric(&sample, "work_count", 118);
  watchdog::AddMetric(&sample, "expected_wkc", 120);
  watchdog::AddLabel(&sample, "mode", "observe_only");
  watchdog::AddLabel(&sample, "robot", "robot_override");

  const watchdog::Report report = reporter.BuildReport(sample);
  if (report.source_id != "robot.main") {
    std::cerr << "source_id = " << report.source_id << ", want robot.main\n";
    return false;
  }
  if (report.source_type != "module") {
    std::cerr << "source_type = " << report.source_type << ", want module\n";
    return false;
  }
  if (report.stale_after_ms != 1500) {
    std::cerr << "stale_after_ms = " << report.stale_after_ms << ", want 1500\n";
    return false;
  }
  if (report.metrics.at("work_count") != 118) {
    std::cerr << "work_count metric mismatch\n";
    return false;
  }
  if (report.labels.at("process") != "robot_control_node") {
    std::cerr << "process label mismatch\n";
    return false;
  }
  if (report.labels.at("mode") != "observe_only") {
    std::cerr << "mode label mismatch\n";
    return false;
  }
  if (report.labels.at("robot") != "robot_override") {
    std::cerr << "sample label should override base robot label\n";
    return false;
  }
  return true;
}

bool TestBuildReportCanRepresentEtherCATSource() {
  watchdog::ReporterOptions options;
  options.source_id = "robot.ethercat";
  options.source_type = "ethercat";

  watchdog::Reporter reporter(options);
  watchdog::ReporterSample sample = watchdog::MakeOkSample();
  watchdog::AddMetric(&sample, "ethercat.working_counter", 120);
  watchdog::AddMetric(&sample, "ethercat.working_counter_goal", 120);

  const watchdog::Report report = reporter.BuildReport(sample);
  if (report.source_id != "robot.ethercat") {
    std::cerr << "source_id = " << report.source_id << ", want robot.ethercat\n";
    return false;
  }
  if (report.source_type != "ethercat") {
    std::cerr << "source_type = " << report.source_type << ", want ethercat\n";
    return false;
  }
  if (report.metrics.at("ethercat.working_counter") != 120) {
    std::cerr << "ethercat.working_counter metric mismatch\n";
    return false;
  }
  return true;
}

bool TestEncodeReportIncludesProtocolV1Fields() {
  watchdog::Report report;
  report.source_id = "robot.main";
  report.source_type = "module";
  report.severity = watchdog::Severity::kOk;
  report.reason = "";
  report.stale_after_ms = 1500;
  report.metrics["control_period_us"] = 551;
  report.metrics["imu_ready"] = 1;
  report.labels["process"] = "robot_control_node";
  report.labels["surface"] = "robot_main";

  std::string error;
  const std::string payload = watchdog::Client::Encode(report, &error);
  if (!error.empty()) {
    std::cerr << "encode returned error: " << error << '\n';
    return false;
  }
  const std::string want =
      "{\"source_id\":\"robot.main\",\"source_type\":\"module\",\"severity\":\"ok\","
      "\"reason\":\"\",\"stale_after_ms\":1500,\"metrics\":{\"control_period_us\":551,"
      "\"imu_ready\":1},\"labels\":{\"process\":\"robot_control_node\","
      "\"surface\":\"robot_main\"}}";
  if (payload != want) {
    std::cerr << "encoded payload mismatch\nwant: " << want << "\ngot:  " << payload << '\n';
    return false;
  }
  return true;
}

bool TestEncodeReportRejectsNonFiniteMetrics() {
  watchdog::Report report;
  report.source_id = "robot.main";
  report.severity = watchdog::Severity::kOk;
  report.metrics["control_period_us"] = std::numeric_limits<double>::quiet_NaN();

  std::string error;
  const std::string payload = watchdog::Client::Encode(report, &error);
  if (!payload.empty()) {
    std::cerr << "non-finite metric produced payload: " << payload << '\n';
    return false;
  }
  if (error != "metrics must be finite numbers") {
    std::cerr << "non-finite metric error mismatch: " << error << '\n';
    return false;
  }
  return true;
}

bool TestHelpersIgnoreNullSample() {
  watchdog::AddMetric(nullptr, "ignored", 1);
  watchdog::AddLabel(nullptr, "ignored", "value");
  return true;
}

bool TestDisabledReporterIsQuietNoop() {
  watchdog::ReporterOptions options;
  options.enabled = false;
  options.source_id = "robot.main";
  options.socket_path = "/tmp/watchdog-reporter-test-missing.sock";

  watchdog::Reporter reporter(options);
  watchdog::ReporterSample sample = watchdog::MakeOkSample();
  std::string error = "keep me honest";
  if (reporter.TrySend(sample, &error)) {
    std::cerr << "disabled reporter unexpectedly sent\n";
    return false;
  }
  if (!error.empty()) {
    std::cerr << "disabled reporter returned error: " << error << '\n';
    return false;
  }
  if (reporter.last_status() != watchdog::ReporterStatus::kDisabled) {
    std::cerr << "disabled reporter status mismatch\n";
    return false;
  }
  if (reporter.consecutive_errors() != 0) {
    std::cerr << "disabled reporter counted an error\n";
    return false;
  }
  return true;
}

bool TestMissingSocketIsRetryLimited() {
  watchdog::ReporterOptions options;
  options.source_id = "robot.main";
  options.socket_path = "/tmp/watchdog-reporter-test-missing.sock";
  options.min_interval = std::chrono::milliseconds(1);
  options.error_retry_interval = std::chrono::seconds(60);

  watchdog::Reporter reporter(options);
  watchdog::ReporterSample sample = watchdog::MakeOkSample();
  std::string error;
  if (reporter.TrySend(sample, &error)) {
    std::cerr << "missing socket unexpectedly sent\n";
    return false;
  }
  if (error.empty()) {
    std::cerr << "missing socket should expose first send error when requested\n";
    return false;
  }
  if (reporter.last_status() != watchdog::ReporterStatus::kSendFailed) {
    std::cerr << "missing socket first status mismatch\n";
    return false;
  }
  if (reporter.consecutive_errors() != 1) {
    std::cerr << "missing socket error count mismatch after first attempt\n";
    return false;
  }

  error = "clear me";
  if (reporter.TrySend(sample, &error)) {
    std::cerr << "retry-limited send unexpectedly sent\n";
    return false;
  }
  if (!error.empty()) {
    std::cerr << "retry-limited skip should not return an error: " << error << '\n';
    return false;
  }
  if (reporter.last_status() != watchdog::ReporterStatus::kErrorRetryLimited) {
    std::cerr << "retry-limited status mismatch\n";
    return false;
  }
  if (reporter.consecutive_errors() != 1) {
    std::cerr << "retry-limited skip should not increment error count\n";
    return false;
  }
  return true;
}

}  // namespace

int main() {
  if (!TestBuildReportMergesBaseAndSampleLabels()) {
    return EXIT_FAILURE;
  }
  if (!TestBuildReportCanRepresentEtherCATSource()) {
    return EXIT_FAILURE;
  }
  if (!TestEncodeReportIncludesProtocolV1Fields()) {
    return EXIT_FAILURE;
  }
  if (!TestEncodeReportRejectsNonFiniteMetrics()) {
    return EXIT_FAILURE;
  }
  if (!TestHelpersIgnoreNullSample()) {
    return EXIT_FAILURE;
  }
  if (!TestDisabledReporterIsQuietNoop()) {
    return EXIT_FAILURE;
  }
  if (!TestMissingSocketIsRetryLimited()) {
    return EXIT_FAILURE;
  }
  return EXIT_SUCCESS;
}
