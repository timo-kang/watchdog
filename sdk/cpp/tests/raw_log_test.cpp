#include <cstdlib>
#include <chrono>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <sstream>
#include <string>

#include "watchdog/raw_log.hpp"

namespace {

std::filesystem::path TestRoot() {
  const auto now = std::chrono::system_clock::now().time_since_epoch().count();
  return std::filesystem::temp_directory_path() /
         ("watchdog-cpp-raw-log-test-" + std::to_string(now));
}

std::string ReadFile(const std::filesystem::path& path) {
  std::ifstream in(path, std::ios::binary);
  std::ostringstream out;
  out << in.rdbuf();
  return out.str();
}

int CountLines(const std::string& value) {
  int lines = 0;
  for (char ch : value) {
    if (ch == '\n') {
      ++lines;
    }
  }
  return lines;
}

bool Contains(const std::string& haystack, const std::string& needle) {
  return haystack.find(needle) != std::string::npos;
}

bool TestSegmentWriterWritesSegmentAndManifest() {
  const std::filesystem::path root = TestRoot();
  std::filesystem::remove_all(root);

  watchdog::rawlog::SegmentWriterOptions options;
  options.segment_dir = (root / "segments").string();
  options.manifest_dir = (root / "manifests").string();
  options.source_id = "imu.front";
  options.data_type = "imu";
  options.format = "jsonl";
  options.labels["robot"] = "sim";

  watchdog::rawlog::SegmentWriter writer(options);
  const auto started_at =
      std::chrono::system_clock::from_time_t(1780279323) + std::chrono::nanoseconds(123456789);
  std::string error;
  if (!writer.OpenAt(started_at, &error)) {
    std::cerr << "open failed: " << error << '\n';
    return false;
  }
  if (!writer.WriteLine("{\"seq\":1}", &error)) {
    std::cerr << "write first sample failed: " << error << '\n';
    return false;
  }
  if (!writer.WriteLine("{\"seq\":2}\n", &error)) {
    std::cerr << "write second sample failed: " << error << '\n';
    return false;
  }
  writer.AddDroppedSamples(3);

  watchdog::rawlog::SegmentManifest manifest;
  if (!writer.CloseAt(started_at + std::chrono::seconds(1), &manifest, &error)) {
    std::cerr << "close failed: " << error << '\n';
    return false;
  }

  if (manifest.schema_version != 1 || manifest.source_id != "imu.front" ||
      manifest.source_type != "sensor_raw" || manifest.data_type != "imu") {
    std::cerr << "manifest identity mismatch\n";
    return false;
  }
  if (manifest.sample_count != 2 || manifest.dropped_samples != 3 || manifest.bytes <= 0) {
    std::cerr << "manifest counters mismatch\n";
    return false;
  }
  if (!std::filesystem::exists(manifest.path)) {
    std::cerr << "segment path missing: " << manifest.path << '\n';
    return false;
  }
  if (!std::filesystem::exists(manifest.manifest_path)) {
    std::cerr << "manifest path missing: " << manifest.manifest_path << '\n';
    return false;
  }
  const std::string segment = ReadFile(manifest.path);
  if (CountLines(segment) != 2) {
    std::cerr << "segment line count mismatch: " << segment << '\n';
    return false;
  }
  const std::string manifest_json = ReadFile(manifest.manifest_path);
  if (!Contains(manifest_json, "\"schema_version\": 1") ||
      !Contains(manifest_json, "\"source_id\": \"imu.front\"") ||
      !Contains(manifest_json, "\"sample_count\": 2") ||
      !Contains(manifest_json, "\"dropped_samples\": 3") ||
      !Contains(manifest_json, "\"robot\": \"sim\"")) {
    std::cerr << "manifest JSON missing expected fields:\n" << manifest_json << '\n';
    return false;
  }

  std::filesystem::remove_all(root);
  return true;
}

bool TestSegmentWriterRejectsInvalidConfig() {
  watchdog::rawlog::SegmentWriter writer({});
  std::string error;
  if (writer.Open(&error)) {
    std::cerr << "invalid config unexpectedly opened\n";
    return false;
  }
  if (error != "source_id must not be empty" && error != "data_type must not be empty" &&
      error != "segment_dir must not be empty") {
    std::cerr << "unexpected invalid config error: " << error << '\n';
    return false;
  }
  return true;
}

bool TestAbortRemovesTempWithoutFinalSegment() {
  const std::filesystem::path root = TestRoot();
  std::filesystem::remove_all(root);

  watchdog::rawlog::SegmentWriterOptions options;
  options.segment_dir = (root / "segments").string();
  options.manifest_dir = (root / "manifests").string();
  options.source_id = "joints";
  options.data_type = "joint_state";

  watchdog::rawlog::SegmentWriter writer(options);
  std::string error;
  if (!writer.Open(&error)) {
    std::cerr << "open failed: " << error << '\n';
    return false;
  }
  const std::filesystem::path final_path = writer.final_path();
  if (!writer.WriteLine("{\"seq\":1}", &error)) {
    std::cerr << "write failed: " << error << '\n';
    return false;
  }
  writer.Abort();
  if (std::filesystem::exists(final_path)) {
    std::cerr << "final segment exists after abort\n";
    return false;
  }

  std::filesystem::remove_all(root);
  return true;
}

}  // namespace

int main() {
  if (!TestSegmentWriterWritesSegmentAndManifest()) {
    return EXIT_FAILURE;
  }
  if (!TestSegmentWriterRejectsInvalidConfig()) {
    return EXIT_FAILURE;
  }
  if (!TestAbortRemovesTempWithoutFinalSegment()) {
    return EXIT_FAILURE;
  }
  return EXIT_SUCCESS;
}
