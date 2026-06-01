#include <cstdlib>
#include <chrono>
#include <iostream>
#include <string>

#include "watchdog/raw_log.hpp"

int main(int argc, char** argv) {
  const std::string root = argc > 1 ? argv[1] : "./var/lib/watchdog/logs";

  watchdog::rawlog::SegmentWriterOptions options;
  options.segment_dir = root + "/segments";
  options.manifest_dir = root + "/manifests";
  options.source_id = "demo.imu";
  options.data_type = "imu";
  options.format = "jsonl";
  options.labels["producer"] = "cpp_example";

  watchdog::rawlog::SegmentWriter writer(options);
  std::string error;
  if (!writer.Open(&error)) {
    std::cerr << "open raw segment failed: " << error << '\n';
    return EXIT_FAILURE;
  }

  for (int seq = 1; seq <= 3; ++seq) {
    const std::string sample = std::string(
        "{\"schema_version\":1,\"source_id\":\"demo.imu\",\"data_type\":\"imu\","
        "\"sequence\":") + std::to_string(seq) + "}";
    if (!writer.WriteLine(sample, &error)) {
      std::cerr << "write raw sample failed: " << error << '\n';
      writer.Abort();
      return EXIT_FAILURE;
    }
  }

  watchdog::rawlog::SegmentManifest manifest;
  if (!writer.Close(&manifest, &error)) {
    std::cerr << "close raw segment failed: " << error << '\n';
    return EXIT_FAILURE;
  }

  std::cout << "wrote raw segment: " << manifest.path << '\n';
  std::cout << "wrote raw manifest: " << manifest.manifest_path << '\n';
  return EXIT_SUCCESS;
}
