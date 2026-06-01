#pragma once

#include <atomic>
#include <chrono>
#include <cctype>
#include <cstdint>
#include <cstdio>
#include <ctime>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <map>
#include <sstream>
#include <string>
#include <utility>

namespace watchdog::rawlog {

struct ClockInfo {
  std::string time_base = "system";
  bool synchronized = true;
};

struct SegmentWriterOptions {
  std::string segment_dir = "/var/lib/watchdog/logs/segments";
  std::string manifest_dir = "/var/lib/watchdog/logs/manifests";
  std::string source_id;
  std::string source_type = "sensor_raw";
  std::string data_type;
  std::string format = "jsonl";
  ClockInfo clock;
  std::map<std::string, std::string> labels;
};

struct SegmentManifest {
  int schema_version = 1;
  std::string segment_id;
  std::string source_id;
  std::string source_type;
  std::string data_type;
  std::string format;
  std::string path;
  std::string manifest_path;
  std::chrono::system_clock::time_point started_at{};
  std::chrono::system_clock::time_point ended_at{};
  std::int64_t sample_count = 0;
  std::int64_t dropped_samples = 0;
  std::int64_t bytes = 0;
  ClockInfo clock;
  std::map<std::string, std::string> labels;
};

class SegmentWriter {
 public:
  explicit SegmentWriter(SegmentWriterOptions options) : options_(std::move(options)) {}

  ~SegmentWriter() { Abort(); }

  SegmentWriter(const SegmentWriter&) = delete;
  SegmentWriter& operator=(const SegmentWriter&) = delete;

  bool Open(std::string* error = nullptr) {
    return OpenAt(std::chrono::system_clock::now(), error);
  }

  bool OpenAt(std::chrono::system_clock::time_point started_at, std::string* error = nullptr) {
    if (open_) {
      return AssignError(error, "segment is already open");
    }
    if (!ValidateOptions(error)) {
      return false;
    }

    started_at_ = started_at;
    segment_id_ = MakeSegmentID(options_.source_id, started_at_);

    std::filesystem::path segment_dir =
        std::filesystem::path(options_.segment_dir) / DatePath(started_at_);
    std::error_code ec;
    std::filesystem::create_directories(segment_dir, ec);
    if (ec) {
      return AssignError(error, "create segment dir: " + ec.message());
    }
    std::filesystem::create_directories(options_.manifest_dir, ec);
    if (ec) {
      return AssignError(error, "create manifest dir: " + ec.message());
    }

    final_path_ = segment_dir / (segment_id_ + "." + SafeFileToken(options_.format));
    manifest_path_ = std::filesystem::path(options_.manifest_dir) / (segment_id_ + ".json");
    temp_path_ = segment_dir / ("." + segment_id_ + "." + NextTempSuffix() + ".tmp");

    stream_.open(temp_path_, std::ios::binary | std::ios::out | std::ios::trunc);
    if (!stream_.is_open()) {
      return AssignError(error, "open temp segment failed");
    }

    sample_count_ = 0;
    dropped_samples_ = 0;
    bytes_ = 0;
    open_ = true;
    ClearError(error);
    return true;
  }

  bool WriteLine(const std::string& line, std::string* error = nullptr) {
    if (!open_) {
      return AssignError(error, "segment is not open");
    }
    stream_ << line;
    std::int64_t written = static_cast<std::int64_t>(line.size());
    if (line.empty() || line.back() != '\n') {
      stream_ << '\n';
      ++written;
    }
    if (!stream_) {
      return AssignError(error, "write segment failed");
    }
    ++sample_count_;
    bytes_ += written;
    ClearError(error);
    return true;
  }

  void AddDroppedSamples(std::int64_t count) {
    if (count > 0) {
      dropped_samples_ += count;
    }
  }

  bool Close(SegmentManifest* manifest = nullptr, std::string* error = nullptr) {
    return CloseAt(std::chrono::system_clock::now(), manifest, error);
  }

  bool CloseAt(
      std::chrono::system_clock::time_point ended_at,
      SegmentManifest* manifest = nullptr,
      std::string* error = nullptr) {
    if (!open_) {
      return AssignError(error, "segment is not open");
    }
    if (ended_at < started_at_) {
      Abort();
      return AssignError(error, "ended_at must be >= started_at");
    }

    stream_.flush();
    if (!stream_) {
      Abort();
      return AssignError(error, "flush segment failed");
    }
    stream_.close();
    if (!stream_) {
      Abort();
      return AssignError(error, "close segment failed");
    }

    std::error_code ec;
    std::filesystem::rename(temp_path_, final_path_, ec);
    if (ec) {
      Abort();
      return AssignError(error, "rename segment: " + ec.message());
    }

    SegmentManifest next;
    next.segment_id = segment_id_;
    next.source_id = options_.source_id;
    next.source_type = options_.source_type.empty() ? "sensor_raw" : options_.source_type;
    next.data_type = options_.data_type;
    next.format = options_.format.empty() ? "jsonl" : options_.format;
    next.path = final_path_.string();
    next.manifest_path = manifest_path_.string();
    next.started_at = started_at_;
    next.ended_at = ended_at;
    next.sample_count = sample_count_;
    next.dropped_samples = dropped_samples_;
    next.bytes = bytes_;
    next.clock = options_.clock;
    next.labels = options_.labels;

    if (!WriteManifestFile(next, error)) {
      open_ = false;
      return false;
    }

    open_ = false;
    if (manifest != nullptr) {
      *manifest = next;
    }
    ClearError(error);
    return true;
  }

  void Abort() {
    if (!open_) {
      return;
    }
    if (stream_.is_open()) {
      stream_.close();
    }
    std::error_code ignored;
    std::filesystem::remove(temp_path_, ignored);
    open_ = false;
  }

  bool is_open() const { return open_; }
  const std::string& segment_id() const { return segment_id_; }
  const std::filesystem::path& final_path() const { return final_path_; }
  const std::filesystem::path& manifest_path() const { return manifest_path_; }
  std::int64_t sample_count() const { return sample_count_; }
  std::int64_t dropped_samples() const { return dropped_samples_; }
  std::int64_t bytes() const { return bytes_; }

  static std::string EncodeManifest(const SegmentManifest& manifest) {
    std::ostringstream out;
    out << "{\n";
    out << "  \"schema_version\": " << manifest.schema_version << ",\n";
    out << "  \"segment_id\": \"" << EscapeJSON(manifest.segment_id) << "\",\n";
    out << "  \"source_id\": \"" << EscapeJSON(manifest.source_id) << "\",\n";
    if (!manifest.source_type.empty()) {
      out << "  \"source_type\": \"" << EscapeJSON(manifest.source_type) << "\",\n";
    }
    out << "  \"data_type\": \"" << EscapeJSON(manifest.data_type) << "\",\n";
    out << "  \"format\": \"" << EscapeJSON(manifest.format) << "\",\n";
    out << "  \"path\": \"" << EscapeJSON(manifest.path) << "\",\n";
    out << "  \"started_at\": \"" << FormatRFC3339Nano(manifest.started_at) << "\",\n";
    out << "  \"ended_at\": \"" << FormatRFC3339Nano(manifest.ended_at) << "\",\n";
    out << "  \"sample_count\": " << manifest.sample_count << ",\n";
    out << "  \"dropped_samples\": " << manifest.dropped_samples << ",\n";
    out << "  \"bytes\": " << manifest.bytes << ",\n";
    out << "  \"clock\": {\n";
    out << "    \"time_base\": \"" << EscapeJSON(manifest.clock.time_base) << "\",\n";
    out << "    \"synchronized\": " << (manifest.clock.synchronized ? "true" : "false") << "\n";
    out << "  }";
    if (!manifest.labels.empty()) {
      out << ",\n  \"labels\": {";
      bool first = true;
      for (const auto& entry : manifest.labels) {
        out << (first ? "\n" : ",\n");
        first = false;
        out << "    \"" << EscapeJSON(entry.first) << "\": \""
            << EscapeJSON(entry.second) << "\"";
      }
      out << "\n  }";
    }
    out << "\n}\n";
    return out.str();
  }

  static std::string FormatRFC3339Nano(std::chrono::system_clock::time_point value) {
    const auto since_epoch = value.time_since_epoch();
    const auto seconds = std::chrono::duration_cast<std::chrono::seconds>(since_epoch);
    const auto nanos = std::chrono::duration_cast<std::chrono::nanoseconds>(
        since_epoch - seconds);
    std::time_t time = seconds.count();
    std::tm tm{};
    gmtime_r(&time, &tm);
    std::ostringstream out;
    out << std::setfill('0')
        << std::setw(4) << tm.tm_year + 1900 << '-'
        << std::setw(2) << tm.tm_mon + 1 << '-'
        << std::setw(2) << tm.tm_mday << 'T'
        << std::setw(2) << tm.tm_hour << ':'
        << std::setw(2) << tm.tm_min << ':'
        << std::setw(2) << tm.tm_sec << '.'
        << std::setw(9) << nanos.count() << 'Z';
    return out.str();
  }

  static std::string SafeFileToken(const std::string& value) {
    std::string out;
    out.reserve(value.size());
    for (unsigned char ch : value) {
      if (std::isalnum(ch) || ch == '.' || ch == '-' || ch == '_') {
        out.push_back(static_cast<char>(ch));
      } else {
        out.push_back('_');
      }
    }
    while (!out.empty() && (out.front() == '.' || out.front() == '-' || out.front() == '_')) {
      out.erase(out.begin());
    }
    while (!out.empty() && (out.back() == '.' || out.back() == '-' || out.back() == '_')) {
      out.pop_back();
    }
    return out.empty() ? "segment" : out;
  }

 private:
  bool ValidateOptions(std::string* error) const {
    if (options_.segment_dir.empty()) {
      return AssignError(error, "segment_dir must not be empty");
    }
    if (options_.manifest_dir.empty()) {
      return AssignError(error, "manifest_dir must not be empty");
    }
    if (options_.source_id.empty()) {
      return AssignError(error, "source_id must not be empty");
    }
    if (options_.data_type.empty()) {
      return AssignError(error, "data_type must not be empty");
    }
    return true;
  }

  bool WriteManifestFile(const SegmentManifest& manifest, std::string* error) const {
    const std::filesystem::path temp_path = manifest_path_.string() + ".tmp";
    {
      std::ofstream out(temp_path, std::ios::binary | std::ios::out | std::ios::trunc);
      if (!out.is_open()) {
        return AssignError(error, "open temp manifest failed");
      }
      out << EncodeManifest(manifest);
      out.close();
      if (!out) {
        return AssignError(error, "write temp manifest failed");
      }
    }
    std::error_code ec;
    std::filesystem::rename(temp_path, manifest_path_, ec);
    if (ec) {
      std::error_code ignored;
      std::filesystem::remove(temp_path, ignored);
      return AssignError(error, "rename manifest: " + ec.message());
    }
    return true;
  }

  static std::string DatePath(std::chrono::system_clock::time_point value) {
    const auto seconds = std::chrono::duration_cast<std::chrono::seconds>(value.time_since_epoch());
    std::time_t time = seconds.count();
    std::tm tm{};
    gmtime_r(&time, &tm);
    std::ostringstream out;
    out << std::setfill('0')
        << std::setw(4) << tm.tm_year + 1900 << '-'
        << std::setw(2) << tm.tm_mon + 1 << '-'
        << std::setw(2) << tm.tm_mday;
    return out.str();
  }

  static std::string MakeSegmentID(
      const std::string& source_id,
      std::chrono::system_clock::time_point started_at) {
    const auto since_epoch = started_at.time_since_epoch();
    const auto seconds = std::chrono::duration_cast<std::chrono::seconds>(since_epoch);
    const auto nanos = std::chrono::duration_cast<std::chrono::nanoseconds>(
        since_epoch - seconds);
    std::time_t time = seconds.count();
    std::tm tm{};
    gmtime_r(&time, &tm);
    std::ostringstream out;
    out << SafeFileToken(source_id) << '.'
        << std::setfill('0')
        << std::setw(4) << tm.tm_year + 1900
        << std::setw(2) << tm.tm_mon + 1
        << std::setw(2) << tm.tm_mday << 'T'
        << std::setw(2) << tm.tm_hour
        << std::setw(2) << tm.tm_min
        << std::setw(2) << tm.tm_sec << '.'
        << std::setw(9) << nanos.count() << 'Z';
    return out.str();
  }

  static std::string NextTempSuffix() {
    static std::atomic<std::uint64_t> counter{0};
    return std::to_string(++counter);
  }

  static bool AssignError(std::string* error, const std::string& message) {
    if (error != nullptr) {
      *error = message;
    }
    return false;
  }

  static void ClearError(std::string* error) {
    if (error != nullptr) {
      error->clear();
    }
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

  SegmentWriterOptions options_;
  std::ofstream stream_;
  bool open_ = false;
  std::string segment_id_;
  std::filesystem::path final_path_;
  std::filesystem::path manifest_path_;
  std::filesystem::path temp_path_;
  std::chrono::system_clock::time_point started_at_{};
  std::int64_t sample_count_ = 0;
  std::int64_t dropped_samples_ = 0;
  std::int64_t bytes_ = 0;
};

}  // namespace watchdog::rawlog
