#pragma once

// Source-side fault-diagnosis (DTC) reporter for the watchdog middleware.
//
// A source says "fault on/off" via Set()/Clear(); the reporter owns publication,
// timing, and serialization of the v1 FaultReport (see docs/dtc-model.md), which
// doubles as the source's liveness heartbeat. It carries facts only — it never
// decides or performs a recovery action. Header-only, no dependencies, and
// non-blocking best-effort: a missing consumer never blocks or throws.

#include <cerrno>
#include <chrono>
#include <condition_variable>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <ctime>
#include <functional>
#include <map>
#include <mutex>
#include <sstream>
#include <string>
#include <sys/socket.h>
#include <sys/un.h>
#include <thread>
#include <unistd.h>
#include <utility>
#include <vector>

namespace watchdog {
namespace dtc {

enum class Severity { kInfo, kWarn, kFatal };

inline const char* SeverityToString(Severity s) {
  switch (s) {
    case Severity::kInfo:
      return "INFO";
    case Severity::kWarn:
      return "WARN";
    case Severity::kFatal:
      return "FATAL";
  }
  return "WARN";
}

struct FaultUnit {
  std::string part;
  std::string instance;
};

struct Fault {
  Severity severity = Severity::kWarn;
  std::string since;  // RFC3339; the reporter stamps it on transition if empty
  std::vector<FaultUnit> units;
  std::string detail;
};

// Sender delivers a serialized FaultReport payload. Returns false on failure.
// Making the transport a callback keeps the reporter transport-agnostic and
// testable (inject a capturing sender).
using Sender = std::function<bool(const std::string& payload, std::string* error)>;

struct FaultReporterOptions {
  bool enabled = true;
  std::string source_id;
  std::string instance;
  std::int64_t deadline_ms = 1500;                     // liveness window advertised to consumers
  std::string socket_path = "/run/watchdog/dtc.sock";  // used by the default datagram sender
};

namespace detail {

inline std::string EscapeJSON(const std::string& value) {
  std::string out;
  out.reserve(value.size());
  for (unsigned char ch : value) {
    switch (ch) {
      case '\\': out += "\\\\"; break;
      case '"': out += "\\\""; break;
      case '\b': out += "\\b"; break;
      case '\f': out += "\\f"; break;
      case '\n': out += "\\n"; break;
      case '\r': out += "\\r"; break;
      case '\t': out += "\\t"; break;
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

inline std::string NowRFC3339() {
  std::time_t t = std::time(nullptr);
  std::tm tm{};
  gmtime_r(&t, &tm);
  char buf[32];
  std::strftime(buf, sizeof(buf), "%Y-%m-%dT%H:%M:%SZ", &tm);
  return std::string(buf);
}

// UnixDatagramSender is the default transport: one datagram per report,
// non-blocking (MSG_DONTWAIT) so a full/absent receiver never stalls the caller.
inline Sender UnixDatagramSender(std::string socket_path) {
  return [socket_path](const std::string& payload, std::string* error) -> bool {
    if (socket_path.empty() || socket_path.size() >= sizeof(sockaddr_un::sun_path)) {
      if (error != nullptr) *error = "invalid socket path";
      return false;
    }
    const int fd = ::socket(AF_UNIX, SOCK_DGRAM, 0);
    if (fd < 0) {
      if (error != nullptr) *error = std::string("socket: ") + std::strerror(errno);
      return false;
    }
    sockaddr_un addr{};
    addr.sun_family = AF_UNIX;
    std::snprintf(addr.sun_path, sizeof(addr.sun_path), "%s", socket_path.c_str());
    const ssize_t w = ::sendto(fd, payload.data(), payload.size(), MSG_DONTWAIT,
                               reinterpret_cast<const sockaddr*>(&addr), sizeof(addr));
    const int saved = errno;
    ::close(fd);
    if (w != static_cast<ssize_t>(payload.size())) {
      errno = saved;
      if (error != nullptr) *error = std::string("sendto: ") + std::strerror(errno);
      return false;
    }
    if (error != nullptr) error->clear();
    return true;
  };
}

}  // namespace detail

class FaultReporter {
 public:
  explicit FaultReporter(FaultReporterOptions options)
      : options_(std::move(options)),
        sender_(detail::UnixDatagramSender(options_.socket_path)) {}

  // Inject a custom transport (also used by tests).
  void SetSender(Sender sender) {
    std::lock_guard<std::mutex> lock(mu_);
    sender_ = std::move(sender);
  }

  // Set (register) an active DTC. Thread-safe. The onset timestamp (`since`) is
  // preserved across repeated Set()s so a re-asserted fault keeps its origin time.
  void Set(const std::string& code, Severity severity, Fault detail = {}) {
    std::lock_guard<std::mutex> lock(mu_);
    detail.severity = severity;
    auto it = active_.find(code);
    if (it != active_.end() && !it->second.since.empty()) {
      detail.since = it->second.since;
    } else if (detail.since.empty()) {
      detail.since = detail::NowRFC3339();
    }
    active_[code] = std::move(detail);
  }

  // Clear (reset) a DTC. Thread-safe.
  void Clear(const std::string& code) {
    std::lock_guard<std::mutex> lock(mu_);
    active_.erase(code);
  }

  bool Has(const std::string& code) {
    std::lock_guard<std::mutex> lock(mu_);
    return active_.find(code) != active_.end();
  }

  std::size_t ActiveCount() {
    std::lock_guard<std::mutex> lock(mu_);
    return active_.size();
  }

  // BuildJSON serializes the current FaultReport (v1 schema). Stamps published_at
  // now and increments the sequence. Exposed for testing without a transport.
  std::string BuildJSON() {
    std::lock_guard<std::mutex> lock(mu_);
    return BuildLocked(detail::NowRFC3339(), ++sequence_);
  }

  // Publish builds and sends the current FaultReport. Call it periodically (the
  // heartbeat) and immediately after a transition. Non-blocking best-effort:
  // disabled -> no-op (false); send failure -> false. Never throws or blocks.
  bool Publish(std::string* error = nullptr) {
    Sender sender;
    std::string payload;
    {
      std::lock_guard<std::mutex> lock(mu_);
      if (!options_.enabled) {
        if (error != nullptr) error->clear();
        return false;
      }
      payload = BuildLocked(detail::NowRFC3339(), ++sequence_);
      sender = sender_;
    }
    if (!sender) {
      if (error != nullptr) *error = "no sender configured";
      return false;
    }
    return sender(payload, error);
  }

 private:
  std::string BuildLocked(const std::string& published_at, std::uint64_t seq) const {
    std::ostringstream out;
    out << "{\"schema_version\":1";
    out << ",\"source_id\":\"" << detail::EscapeJSON(options_.source_id) << '"';
    if (!options_.instance.empty()) {
      out << ",\"instance\":\"" << detail::EscapeJSON(options_.instance) << '"';
    }
    out << ",\"sequence\":" << seq;
    out << ",\"published_at\":\"" << published_at << '"';
    out << ",\"deadline_ms\":" << options_.deadline_ms;
    out << ",\"active\":[";
    bool first = true;
    for (const auto& entry : active_) {
      if (!first) out << ',';
      first = false;
      const Fault& f = entry.second;
      out << "{\"code\":\"" << detail::EscapeJSON(entry.first) << '"';
      out << ",\"severity\":\"" << SeverityToString(f.severity) << '"';
      out << ",\"since\":\"" << detail::EscapeJSON(f.since) << '"';
      if (!f.units.empty()) {
        out << ",\"units\":[";
        bool uf = true;
        for (const auto& u : f.units) {
          if (!uf) out << ',';
          uf = false;
          out << "{\"part\":\"" << detail::EscapeJSON(u.part) << '"';
          if (!u.instance.empty()) {
            out << ",\"instance\":\"" << detail::EscapeJSON(u.instance) << '"';
          }
          out << '}';
        }
        out << ']';
      }
      if (!f.detail.empty()) {
        out << ",\"detail\":\"" << detail::EscapeJSON(f.detail) << '"';
      }
      out << '}';
    }
    out << "]}";
    return out.str();
  }

  FaultReporterOptions options_;
  std::mutex mu_;
  std::map<std::string, Fault> active_;
  std::uint64_t sequence_ = 0;
  Sender sender_;
};

// DataFreshnessWatchdog runs on its own thread and invokes on_stale() once if
// Feed() is not called within `deadline`. Detection only: the callback decides
// which DTC to Set(); it never performs recovery. A blocked feeder thread cannot
// suppress the alarm because the watchdog runs independently. Destroy or Stop()
// to end the thread.
class DataFreshnessWatchdog {
 public:
  DataFreshnessWatchdog(std::chrono::milliseconds deadline, std::function<void()> on_stale)
      : deadline_(deadline), on_stale_(std::move(on_stale)) {
    last_feed_ = Clock::now();
    thread_ = std::thread([this] { Loop(); });
  }

  ~DataFreshnessWatchdog() { Stop(); }

  DataFreshnessWatchdog(const DataFreshnessWatchdog&) = delete;
  DataFreshnessWatchdog& operator=(const DataFreshnessWatchdog&) = delete;

  // Feed marks the data fresh, resetting the deadline and re-arming the alarm.
  void Feed() {
    std::lock_guard<std::mutex> lock(mu_);
    last_feed_ = Clock::now();
    fired_ = false;
  }

  void Stop() {
    {
      std::lock_guard<std::mutex> lock(mu_);
      if (stop_) return;
      stop_ = true;
    }
    cv_.notify_all();
    if (thread_.joinable()) thread_.join();
  }

 private:
  using Clock = std::chrono::steady_clock;

  void Loop() {
    std::unique_lock<std::mutex> lock(mu_);
    while (!stop_) {
      const auto wait = deadline_ / 2 > std::chrono::milliseconds(1) ? deadline_ / 2
                                                                     : std::chrono::milliseconds(1);
      cv_.wait_for(lock, wait, [this] { return stop_; });
      if (stop_) break;
      if (!fired_ && Clock::now() - last_feed_ > deadline_) {
        fired_ = true;
        auto cb = on_stale_;
        lock.unlock();
        if (cb) cb();
        lock.lock();
      }
    }
  }

  std::chrono::milliseconds deadline_;
  std::function<void()> on_stale_;
  std::mutex mu_;
  std::condition_variable cv_;
  Clock::time_point last_feed_;
  bool fired_ = false;
  bool stop_ = false;
  std::thread thread_;
};

}  // namespace dtc
}  // namespace watchdog
