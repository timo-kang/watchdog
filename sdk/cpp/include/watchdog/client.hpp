#pragma once

#include <cerrno>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <map>
#include <sstream>
#include <string>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>
#include <utility>

namespace watchdog {

enum class Severity {
  kOk,
  kWarn,
  kFail,
  kStale,
};

inline const char* SeverityToString(Severity severity) {
  switch (severity) {
    case Severity::kOk:
      return "ok";
    case Severity::kWarn:
      return "warn";
    case Severity::kFail:
      return "fail";
    case Severity::kStale:
      return "stale";
  }
  return "ok";
}

struct Report {
  std::string source_id;
  Severity severity = Severity::kOk;
  std::string reason;
  std::int64_t stale_after_ms = 0;
  std::string observed_at;
  std::map<std::string, double> metrics;
  std::map<std::string, std::string> labels;
};

class Client {
 public:
  explicit Client(std::string socket_path) : socket_path_(std::move(socket_path)) {}

  bool Send(const Report& report, std::string* error = nullptr) const {
    if (report.source_id.empty()) {
      AssignError(error, "source_id must not be empty");
      return false;
    }
    if (socket_path_.empty()) {
      AssignError(error, "socket path must not be empty");
      return false;
    }
    if (socket_path_.size() >= sizeof(sockaddr_un::sun_path)) {
      AssignError(error, "socket path is too long for sockaddr_un");
      return false;
    }

    const std::string payload = EncodeReport(report, error);
    if (error != nullptr && !error->empty()) {
      return false;
    }

    const int fd = ::socket(AF_UNIX, SOCK_DGRAM, 0);
    if (fd < 0) {
      AssignErrno(error, "socket");
      return false;
    }

    sockaddr_un addr{};
    addr.sun_family = AF_UNIX;
    std::snprintf(addr.sun_path, sizeof(addr.sun_path), "%s", socket_path_.c_str());

    const ssize_t written = ::sendto(
        fd,
        payload.data(),
        payload.size(),
        0,
        reinterpret_cast<const sockaddr*>(&addr),
        sizeof(addr));
    const int saved_errno = errno;
    ::close(fd);

    if (written != static_cast<ssize_t>(payload.size())) {
      errno = saved_errno;
      AssignErrno(error, "sendto");
      return false;
    }

    if (error != nullptr) {
      error->clear();
    }
    return true;
  }

  const std::string& socket_path() const { return socket_path_; }

 private:
  static void AssignError(std::string* error, const std::string& message) {
    if (error != nullptr) {
      *error = message;
    }
  }

  static void AssignErrno(std::string* error, const std::string& prefix) {
    if (error != nullptr) {
      *error = prefix + ": " + std::strerror(errno);
    }
  }

  static std::string EncodeReport(const Report& report, std::string* error) {
    std::ostringstream out;
    out.precision(15);
    out << '{';
    out << "\"source_id\":\"" << EscapeJSON(report.source_id) << '"';
    out << ",\"severity\":\"" << SeverityToString(report.severity) << '"';
    out << ",\"reason\":\"" << EscapeJSON(report.reason) << '"';
    if (report.stale_after_ms > 0) {
      out << ",\"stale_after_ms\":" << report.stale_after_ms;
    }
    if (!report.observed_at.empty()) {
      out << ",\"observed_at\":\"" << EscapeJSON(report.observed_at) << '"';
    }
    if (!report.metrics.empty()) {
      out << ",\"metrics\":{";
      bool first = true;
      for (const auto& entry : report.metrics) {
        if (!std::isfinite(entry.second)) {
          AssignError(error, "metrics must be finite numbers");
          return "";
        }
        if (!first) {
          out << ',';
        }
        first = false;
        out << '"' << EscapeJSON(entry.first) << "\":" << entry.second;
      }
      out << '}';
    }
    if (!report.labels.empty()) {
      out << ",\"labels\":{";
      bool first = true;
      for (const auto& entry : report.labels) {
        if (!first) {
          out << ',';
        }
        first = false;
        out << '"' << EscapeJSON(entry.first) << "\":\"" << EscapeJSON(entry.second) << '"';
      }
      out << '}';
    }
    out << '}';
    if (error != nullptr) {
      error->clear();
    }
    return out.str();
  }

  static std::string EscapeJSON(const std::string& value) {
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

  std::string socket_path_;
};

}  // namespace watchdog
