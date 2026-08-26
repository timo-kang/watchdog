#include <atomic>
#include <chrono>
#include <cstdlib>
#include <iostream>
#include <string>
#include <thread>

#include "watchdog/fault_reporter.hpp"

namespace {

using watchdog::dtc::DataFreshnessWatchdog;
using watchdog::dtc::Fault;
using watchdog::dtc::FaultReporter;
using watchdog::dtc::FaultReporterOptions;
using watchdog::dtc::FaultUnit;
using watchdog::dtc::Severity;

bool contains(const std::string& haystack, const std::string& needle) {
  return haystack.find(needle) != std::string::npos;
}

bool TestSetClearActiveCount() {
  FaultReporterOptions opts;
  opts.source_id = "robot-1.raidrive";
  FaultReporter r(opts);
  r.Set("C1131", Severity::kFatal);
  r.Set("P1310", Severity::kWarn);
  if (r.ActiveCount() != 2 || !r.Has("C1131") || !r.Has("P1310")) {
    std::cerr << "set: expected 2 active with both codes\n";
    return false;
  }
  r.Clear("P1310");
  if (r.ActiveCount() != 1 || r.Has("P1310") || !r.Has("C1131")) {
    std::cerr << "clear: expected only C1131 active\n";
    return false;
  }
  return true;
}

bool TestBuildJSONShapeAndSequence() {
  FaultReporterOptions opts;
  opts.source_id = "robot-1.raidrive";
  opts.instance = "left_hip";
  opts.deadline_ms = 1000;
  FaultReporter r(opts);

  Fault f;
  f.units.push_back(FaultUnit{"joint.left_hip", ""});
  f.detail = "actuator overheat";
  r.Set("C1131", Severity::kFatal, f);

  const std::string j1 = r.BuildJSON();
  for (const std::string& want :
       {std::string("\"schema_version\":1"), std::string("\"source_id\":\"robot-1.raidrive\""),
        std::string("\"instance\":\"left_hip\""), std::string("\"deadline_ms\":1000"),
        std::string("\"code\":\"C1131\""), std::string("\"severity\":\"FATAL\""),
        std::string("\"part\":\"joint.left_hip\""), std::string("\"detail\":\"actuator overheat\""),
        std::string("\"sequence\":1")}) {
    if (!contains(j1, want)) {
      std::cerr << "BuildJSON missing " << want << " in: " << j1 << "\n";
      return false;
    }
  }
  const std::string j2 = r.BuildJSON();
  if (!contains(j2, "\"sequence\":2")) {
    std::cerr << "sequence did not increment: " << j2 << "\n";
    return false;
  }
  return true;
}

bool TestPublishViaInjectedSender() {
  FaultReporterOptions opts;
  opts.source_id = "robot-1.main";
  FaultReporter r(opts);

  std::string captured;
  int calls = 0;
  r.SetSender([&](const std::string& payload, std::string* err) {
    captured = payload;
    ++calls;
    if (err != nullptr) err->clear();
    return true;
  });
  r.Set("P1310", Severity::kWarn);
  if (!r.Publish() || calls != 1 || !contains(captured, "\"source_id\":\"robot-1.main\"") ||
      !contains(captured, "\"code\":\"P1310\"")) {
    std::cerr << "publish did not deliver expected payload: " << captured << "\n";
    return false;
  }

  // Disabled reporter is a no-op: no send, returns false.
  FaultReporterOptions off = opts;
  off.enabled = false;
  FaultReporter r2(off);
  int calls2 = 0;
  r2.SetSender([&](const std::string&, std::string*) {
    ++calls2;
    return true;
  });
  if (r2.Publish() || calls2 != 0) {
    std::cerr << "disabled reporter should not send\n";
    return false;
  }
  return true;
}

bool TestDataFreshnessWatchdogFires() {
  std::atomic<bool> fired{false};
  {
    DataFreshnessWatchdog wd(std::chrono::milliseconds(40), [&] { fired = true; });
    std::this_thread::sleep_for(std::chrono::milliseconds(300));  // >> deadline, no Feed()
  }
  if (!fired) {
    std::cerr << "watchdog should fire when not fed\n";
    return false;
  }
  return true;
}

bool TestDataFreshnessWatchdogFedStaysQuiet() {
  std::atomic<bool> fired{false};
  {
    DataFreshnessWatchdog wd(std::chrono::seconds(10), [&] { fired = true; });
    wd.Feed();
    std::this_thread::sleep_for(std::chrono::milliseconds(100));  // << deadline
  }
  if (fired) {
    std::cerr << "watchdog should not fire well within its deadline\n";
    return false;
  }
  return true;
}

}  // namespace

int main() {
  const struct {
    const char* name;
    bool (*fn)();
  } tests[] = {
      {"TestSetClearActiveCount", TestSetClearActiveCount},
      {"TestBuildJSONShapeAndSequence", TestBuildJSONShapeAndSequence},
      {"TestPublishViaInjectedSender", TestPublishViaInjectedSender},
      {"TestDataFreshnessWatchdogFires", TestDataFreshnessWatchdogFires},
      {"TestDataFreshnessWatchdogFedStaysQuiet", TestDataFreshnessWatchdogFedStaysQuiet},
  };
  bool ok = true;
  for (const auto& t : tests) {
    if (!t.fn()) {
      std::cerr << "FAILED: " << t.name << "\n";
      ok = false;
    }
  }
  return ok ? EXIT_SUCCESS : EXIT_FAILURE;
}
