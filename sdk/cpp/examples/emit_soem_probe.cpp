#include <cstdlib>
#include <iostream>
#include <string>

#include "watchdog/ethercat_probe.hpp"

int main() {
  watchdog::ethercat::SOEMMasterSnapshot snapshot;
  snapshot.interface = "enp3s0";
  snapshot.working_counter = 120;
  snapshot.working_counter_expected = 120;
  snapshot.labels["master"] = "master0";

  for (int index = 1; index <= 12; ++index) {
    watchdog::ethercat::SOEMSlaveSnapshot slave;
    slave.position = index;
    slave.name = "slave_" + std::to_string(index);
    slave.al_state = index == 7 ? 0x04 : 0x08;
    snapshot.slaves.push_back(slave);
  }

  snapshot.metrics["ethercat.dc_drift_us"] = 3.4;

  std::string error;
  const watchdog::ethercat::ProbeReport report =
      watchdog::ethercat::BuildProbeReportFromSOEM(snapshot, &error);
  if (!error.empty()) {
    std::cerr << "failed to build SOEM probe: " << error << '\n';
    return EXIT_FAILURE;
  }
  if (!watchdog::ethercat::WriteProbeReport(&std::cout, report, &error)) {
    std::cerr << "failed to encode SOEM probe: " << error << '\n';
    return EXIT_FAILURE;
  }
  std::cout << '\n';
  return EXIT_SUCCESS;
}
