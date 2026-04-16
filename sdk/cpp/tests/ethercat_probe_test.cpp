#include <cstdlib>
#include <iostream>
#include <string>

#include "watchdog/ethercat_probe.hpp"

namespace {

bool TestBuildProbeReportFromSOEM() {
  watchdog::ethercat::SOEMMasterSnapshot snapshot;
  snapshot.interface = "enp3s0";
  snapshot.working_counter = 118;
  snapshot.working_counter_expected = 120;
  snapshot.labels["master"] = "master0";
  snapshot.metrics["ethercat.dc_drift_us"] = 3.4;

  for (int index = 1; index <= 12; ++index) {
    watchdog::ethercat::SOEMSlaveSnapshot slave;
    slave.position = index;
    slave.name = "slave_" + std::to_string(index);
    slave.al_state = index == 7 ? 0x04 : 0x08;
    slave.lost = index == 12;
    snapshot.slaves.push_back(slave);
  }

  std::string error;
  const watchdog::ethercat::ProbeReport report =
      watchdog::ethercat::BuildProbeReportFromSOEM(snapshot, &error);
  if (!error.empty()) {
    std::cerr << "unexpected build error: " << error << '\n';
    return false;
  }
  if (report.slaves_seen != 12) {
    std::cerr << "slaves_seen = " << report.slaves_seen << ", want 12\n";
    return false;
  }
  if (report.slave_errors != 2) {
    std::cerr << "slave_errors = " << report.slave_errors << ", want 2\n";
    return false;
  }
  if (report.master_state != "safeop") {
    std::cerr << "master_state = " << report.master_state << ", want safeop\n";
    return false;
  }
  const std::string payload = watchdog::ethercat::EncodeProbeReport(report, &error);
  if (!error.empty()) {
    std::cerr << "unexpected encode error: " << error << '\n';
    return false;
  }
  if (payload.find("\"slave_errors\":2") == std::string::npos) {
    std::cerr << "payload missing slave_errors count\n";
    return false;
  }
  if (payload.find("\"lost\":true") == std::string::npos) {
    std::cerr << "payload missing lost slave marker\n";
    return false;
  }
  return true;
}

bool TestValidateRejectsDuplicateSlavePosition() {
  watchdog::ethercat::ProbeReport report;
  report.slaves_seen = 2;
  watchdog::ethercat::SlaveStatus first;
  first.position = 1;
  first.state = "op";
  report.slaves.push_back(first);
  watchdog::ethercat::SlaveStatus second;
  second.position = 1;
  second.state = "op";
  report.slaves.push_back(second);

  std::string error;
  if (watchdog::ethercat::ValidateProbeReport(report, &error)) {
    std::cerr << "expected duplicate position validation failure\n";
    return false;
  }
  if (error.find("duplicate slave position") == std::string::npos) {
    std::cerr << "unexpected validation error: " << error << '\n';
    return false;
  }
  return true;
}

}  // namespace

int main() {
  if (!TestBuildProbeReportFromSOEM()) {
    return EXIT_FAILURE;
  }
  if (!TestValidateRejectsDuplicateSlavePosition()) {
    return EXIT_FAILURE;
  }
  return EXIT_SUCCESS;
}
