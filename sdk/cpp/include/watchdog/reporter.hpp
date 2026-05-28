#pragma once

#include <algorithm>
#include <chrono>
#include <cstdint>
#include <map>
#include <string>
#include <utility>

#include "watchdog/client.hpp"

namespace watchdog {

struct ReporterOptions {
  bool enabled = true;
  std::string socket_path = "/run/watchdog/module.sock";
  std::string source_id;
  std::string source_type;
  std::int64_t stale_after_ms = 1500;
  std::chrono::milliseconds min_interval{1000};
  std::chrono::milliseconds error_retry_interval{5000};
  std::map<std::string, std::string> labels;
};

struct ReporterSample {
  Severity severity = Severity::kOk;
  std::string reason;
  std::string observed_at;
  std::map<std::string, double> metrics;
  std::map<std::string, std::string> labels;
};

enum class ReporterStatus {
  kNeverSent,
  kDisabled,
  kRateLimited,
  kErrorRetryLimited,
  kSent,
  kSendFailed,
};

class Reporter {
 public:
  explicit Reporter(ReporterOptions options)
      : options_(std::move(options)), client_(options_.socket_path) {}

  bool TrySend(const ReporterSample& sample, std::string* error = nullptr) {
    const auto now = Clock::now();
    if (!options_.enabled) {
      last_status_ = ReporterStatus::kDisabled;
      ClearError(error);
      return false;
    }

    const auto retry_interval =
        consecutive_errors_ > 0
            ? std::max(options_.min_interval, options_.error_retry_interval)
            : options_.min_interval;
    if (last_attempt_.time_since_epoch().count() != 0 &&
        now - last_attempt_ < retry_interval) {
      last_status_ = consecutive_errors_ > 0 ? ReporterStatus::kErrorRetryLimited
                                             : ReporterStatus::kRateLimited;
      if (error != nullptr) {
        error->clear();
      }
      return false;
    }

    last_attempt_ = now;
    return SendBuiltReport(BuildReport(sample), now, error);
  }

  bool SendNow(const ReporterSample& sample, std::string* error = nullptr) {
    if (!options_.enabled) {
      last_status_ = ReporterStatus::kDisabled;
      ClearError(error);
      return false;
    }

    const auto now = Clock::now();
    last_attempt_ = now;
    return SendBuiltReport(BuildReport(sample), now, error);
  }

  Report BuildReport(const ReporterSample& sample) const {
    Report report;
    report.source_id = options_.source_id;
    report.source_type = options_.source_type;
    report.severity = sample.severity;
    report.reason = sample.reason;
    report.observed_at = sample.observed_at;
    report.stale_after_ms = options_.stale_after_ms;
    report.metrics = sample.metrics;
    report.labels = options_.labels;
    for (const auto& entry : sample.labels) {
      report.labels[entry.first] = entry.second;
    }
    return report;
  }

  const ReporterOptions& options() const { return options_; }

  int consecutive_errors() const { return consecutive_errors_; }

  ReporterStatus last_status() const { return last_status_; }

  const std::string& last_error() const { return last_error_; }

 private:
  using Clock = std::chrono::steady_clock;

  bool SendBuiltReport(const Report& report, Clock::time_point now, std::string* error) {
    std::string send_error;
    if (!client_.Send(report, &send_error)) {
      ++consecutive_errors_;
      last_error_ = send_error;
      last_status_ = ReporterStatus::kSendFailed;
      if (error != nullptr) {
        *error = send_error;
      }
      return false;
    }

    last_send_ = now;
    consecutive_errors_ = 0;
    last_error_.clear();
    last_status_ = ReporterStatus::kSent;
    ClearError(error);
    return true;
  }

  static void ClearError(std::string* error) {
    if (error != nullptr) {
      error->clear();
    }
  }

  ReporterOptions options_;
  Client client_;
  Clock::time_point last_attempt_{};
  Clock::time_point last_send_{};
  int consecutive_errors_ = 0;
  ReporterStatus last_status_ = ReporterStatus::kNeverSent;
  std::string last_error_;
};

inline ReporterSample MakeOkSample() {
  ReporterSample sample;
  sample.severity = Severity::kOk;
  return sample;
}

inline void AddMetric(ReporterSample* sample, const std::string& key, double value) {
  if (sample == nullptr) {
    return;
  }
  sample->metrics[key] = value;
}

inline void AddLabel(ReporterSample* sample, const std::string& key, std::string value) {
  if (sample == nullptr) {
    return;
  }
  sample->labels[key] = std::move(value);
}

}  // namespace watchdog
