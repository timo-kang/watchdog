#pragma once

#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <map>
#include <ostream>
#include <set>
#include <sstream>
#include <string>
#include <utility>
#include <vector>

namespace watchdog::ethercat {

struct SlaveStatus {
  int position = 0;
  std::string name;
  std::string state = "op";
  bool lost = false;
  std::string error;
};

struct ProbeReport {
  std::string collected_at;
  bool link_known = true;
  bool link_up = true;
  std::string interface;
  std::string master_state = "op";
  int slaves_seen = 0;
  int slave_errors = 0;
  int working_counter = 0;
  int working_counter_expected = 0;
  std::vector<SlaveStatus> slaves;
  std::map<std::string, double> metrics;
  std::map<std::string, std::string> labels;
};

struct SOEMSlaveSnapshot {
  int position = 0;
  std::string name;
  std::uint16_t al_state = 0x08;
  bool lost = false;
  std::string error;
};

struct SOEMMasterSnapshot {
  std::string collected_at;
  bool link_known = true;
  bool link_up = true;
  std::string interface;
  std::string master_state;
  int slaves_seen = 0;
  int slave_errors = 0;
  int working_counter = 0;
  int working_counter_expected = 0;
  std::vector<SOEMSlaveSnapshot> slaves;
  std::map<std::string, double> metrics;
  std::map<std::string, std::string> labels;
};

inline std::string NormalizeALState(std::string value) {
  for (char& ch : value) {
    if (ch >= 'A' && ch <= 'Z') {
      ch = static_cast<char>(ch - 'A' + 'a');
    }
  }
  if (value == "pre-op" || value == "pre_operational" || value == "pre-operational") {
    return "preop";
  }
  if (value == "safe-op" || value == "safe_operational" || value == "safe-operational") {
    return "safeop";
  }
  if (value == "operational") {
    return "op";
  }
  return value;
}

inline std::string SOEMStateToString(std::uint16_t state) {
  switch (state & 0x0f) {
    case 0x01:
      return "init";
    case 0x02:
      return "preop";
    case 0x03:
      return "boot";
    case 0x04:
      return "safeop";
    case 0x08:
      return "op";
    default:
      return "unknown";
  }
}

inline SlaveStatus MakeSOEMSlaveStatus(
    int position,
    std::string name,
    std::uint16_t al_state,
    bool lost = false,
    std::string error = "") {
  SlaveStatus slave;
  slave.position = position;
  slave.name = std::move(name);
  slave.state = SOEMStateToString(al_state);
  slave.lost = lost;
  slave.error = std::move(error);
  return slave;
}

inline void AddSOEMSlave(
    ProbeReport* report,
    int position,
    std::string name,
    std::uint16_t al_state,
    bool lost = false,
    std::string error = "") {
  if (report == nullptr) {
    return;
  }
  report->slaves.push_back(
      MakeSOEMSlaveStatus(position, std::move(name), al_state, lost, std::move(error)));
}

inline void DeriveSummary(ProbeReport* report) {
  if (report == nullptr) {
    return;
  }
  if (report->slaves_seen <= 0) {
    report->slaves_seen = static_cast<int>(report->slaves.size());
  }
  if (report->master_state.empty()) {
    report->master_state = "op";
  }
  if (report->slave_errors > 0 || report->slaves.empty()) {
    return;
  }

  int slave_errors = 0;
  std::string derived_state = report->master_state;
  for (const auto& slave : report->slaves) {
    const std::string state = NormalizeALState(slave.state);
    if (slave.lost || !slave.error.empty() || (!state.empty() && state != "op")) {
      ++slave_errors;
    }
    if (state == "init") {
      derived_state = "init";
    } else if (state == "preop" && derived_state != "init") {
      derived_state = "preop";
    } else if (state == "safeop" && derived_state != "init" && derived_state != "preop") {
      derived_state = "safeop";
    }
  }
  report->slave_errors = slave_errors;
  report->master_state = derived_state;
}

inline bool ValidateProbeReport(const ProbeReport& report, std::string* error = nullptr) {
  if (report.slaves_seen < 0) {
    if (error != nullptr) {
      *error = "slaves_seen must be >= 0";
    }
    return false;
  }
  if (report.slave_errors < 0) {
    if (error != nullptr) {
      *error = "slave_errors must be >= 0";
    }
    return false;
  }
  if (report.working_counter < 0) {
    if (error != nullptr) {
      *error = "working_counter must be >= 0";
    }
    return false;
  }
  if (report.working_counter_expected < 0) {
    if (error != nullptr) {
      *error = "working_counter_expected must be >= 0";
    }
    return false;
  }
  if (report.slaves_seen > 0 && static_cast<int>(report.slaves.size()) > report.slaves_seen) {
    if (error != nullptr) {
      *error = "slaves array length must be <= slaves_seen";
    }
    return false;
  }

  std::set<int> seen_positions;
  for (const auto& slave : report.slaves) {
    if (slave.position <= 0) {
      if (error != nullptr) {
        *error = "slave positions must be >= 1";
      }
      return false;
    }
    if (!seen_positions.insert(slave.position).second) {
      if (error != nullptr) {
        *error = "duplicate slave position " + std::to_string(slave.position);
      }
      return false;
    }
    if (NormalizeALState(slave.state).empty()) {
      if (error != nullptr) {
        *error = "slave state must not be empty";
      }
      return false;
    }
  }

  for (const auto& entry : report.metrics) {
    if (!std::isfinite(entry.second)) {
      if (error != nullptr) {
        *error = "metrics must be finite numbers";
      }
      return false;
    }
  }

  if (error != nullptr) {
    error->clear();
  }
  return true;
}

inline ProbeReport BuildProbeReportFromSOEM(
    const SOEMMasterSnapshot& snapshot,
    std::string* error = nullptr) {
  ProbeReport report;
  report.collected_at = snapshot.collected_at;
  report.link_known = snapshot.link_known;
  report.link_up = snapshot.link_up;
  report.interface = snapshot.interface;
  report.master_state = NormalizeALState(snapshot.master_state);
  report.slaves_seen = snapshot.slaves_seen;
  report.slave_errors = snapshot.slave_errors;
  report.working_counter = snapshot.working_counter;
  report.working_counter_expected = snapshot.working_counter_expected;
  report.metrics = snapshot.metrics;
  report.labels = snapshot.labels;
  for (const auto& slave : snapshot.slaves) {
    report.slaves.push_back(
        MakeSOEMSlaveStatus(slave.position, slave.name, slave.al_state, slave.lost, slave.error));
  }

  DeriveSummary(&report);
  if (!ValidateProbeReport(report, error)) {
    return ProbeReport{};
  }
  return report;
}

inline std::string EscapeJSON(const std::string& value) {
  std::string out;
  out.reserve(value.size());
  for (unsigned char ch : value) {
    switch (ch) {
      case '\\':
        out += "\\\\";
        break;
      case '"':
        out += "\\\"";
        break;
      case '\b':
        out += "\\b";
        break;
      case '\f':
        out += "\\f";
        break;
      case '\n':
        out += "\\n";
        break;
      case '\r':
        out += "\\r";
        break;
      case '\t':
        out += "\\t";
        break;
      default:
        if (ch < 0x20) {
          char buf[7];
          std::snprintf(buf, sizeof(buf), "\\u%04x", ch);
          out += buf;
        } else {
          out.push_back(static_cast<char>(ch));
        }
    }
  }
  return out;
}

inline std::string EncodeProbeReport(const ProbeReport& input, std::string* error = nullptr) {
  ProbeReport report = input;
  DeriveSummary(&report);
  if (!ValidateProbeReport(report, error)) {
    return "";
  }

  std::ostringstream out;
  out.precision(15);
  out << '{';
  bool first = true;

  const auto add_field = [&](const std::string& fragment, bool* first_field, std::ostringstream* stream) {
    if (!*first_field) {
      *stream << ',';
    }
    *first_field = false;
    *stream << fragment;
  };

  if (!report.collected_at.empty()) {
    add_field("\"collected_at\":\"" + EscapeJSON(report.collected_at) + '"', &first, &out);
  }
  add_field(std::string("\"link_known\":") + (report.link_known ? "true" : "false"), &first, &out);
  add_field(std::string("\"link_up\":") + (report.link_up ? "true" : "false"), &first, &out);
  if (!report.interface.empty()) {
    add_field("\"interface\":\"" + EscapeJSON(report.interface) + '"', &first, &out);
  }
  add_field("\"master_state\":\"" + EscapeJSON(report.master_state) + '"', &first, &out);
  add_field("\"slaves_seen\":" + std::to_string(report.slaves_seen), &first, &out);
  add_field("\"slave_errors\":" + std::to_string(report.slave_errors), &first, &out);
  add_field("\"working_counter\":" + std::to_string(report.working_counter), &first, &out);
  add_field(
      "\"working_counter_expected\":" + std::to_string(report.working_counter_expected),
      &first,
      &out);

  out << ",\"slaves\":[";
  bool first_slave = true;
  for (const auto& slave : report.slaves) {
    if (!first_slave) {
      out << ',';
    }
    first_slave = false;
    out << '{';
    out << "\"position\":" << slave.position;
    if (!slave.name.empty()) {
      out << ",\"name\":\"" << EscapeJSON(slave.name) << '"';
    }
    out << ",\"state\":\"" << EscapeJSON(NormalizeALState(slave.state)) << '"';
    if (slave.lost) {
      out << ",\"lost\":true";
    }
    if (!slave.error.empty()) {
      out << ",\"error\":\"" << EscapeJSON(slave.error) << '"';
    }
    out << '}';
  }
  out << ']';

  if (!report.labels.empty()) {
    out << ",\"labels\":{";
    bool first_label = true;
    for (const auto& entry : report.labels) {
      if (!first_label) {
        out << ',';
      }
      first_label = false;
      out << '"' << EscapeJSON(entry.first) << "\":\"" << EscapeJSON(entry.second) << '"';
    }
    out << '}';
  }

  if (!report.metrics.empty()) {
    out << ",\"metrics\":{";
    bool first_metric = true;
    for (const auto& entry : report.metrics) {
      if (!first_metric) {
        out << ',';
      }
      first_metric = false;
      out << '"' << EscapeJSON(entry.first) << "\":" << entry.second;
    }
    out << '}';
  }

  out << '}';
  if (error != nullptr) {
    error->clear();
  }
  return out.str();
}

inline bool WriteProbeReport(std::ostream* stream, const ProbeReport& report, std::string* error = nullptr) {
  if (stream == nullptr) {
    if (error != nullptr) {
      *error = "stream must not be null";
    }
    return false;
  }

  const std::string payload = EncodeProbeReport(report, error);
  if (error != nullptr && !error->empty()) {
    return false;
  }
  *stream << payload;
  if (!*stream) {
    if (error != nullptr) {
      *error = "failed to write probe payload";
    }
    return false;
  }
  if (error != nullptr) {
    error->clear();
  }
  return true;
}

}  // namespace watchdog::ethercat
