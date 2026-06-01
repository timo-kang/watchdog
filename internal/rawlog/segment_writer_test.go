package rawlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSegmentWriterWritesSegmentAndManifest(t *testing.T) {
	root := t.TempDir()
	startedAt := time.Date(2026, 6, 1, 1, 2, 3, 4, time.UTC)
	writer, err := NewSegmentWriter(SegmentWriterConfig{
		SegmentDir:  filepath.Join(root, "segments"),
		ManifestDir: filepath.Join(root, "manifests"),
		SourceID:    "imu.front",
		DataType:    "imu",
		Format:      "jsonl",
		Clock: ClockInfo{
			TimeBase:     "system",
			Synchronized: true,
		},
		Labels: map[string]string{"robot": "sim"},
	})
	if err != nil {
		t.Fatalf("new segment writer: %v", err)
	}

	handle, err := writer.Open(startedAt)
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	if err := handle.WriteJSON(map[string]any{"seq": 1}); err != nil {
		t.Fatalf("write first sample: %v", err)
	}
	if err := handle.WriteLine([]byte(`{"seq":2}`)); err != nil {
		t.Fatalf("write second sample: %v", err)
	}
	handle.DropSamples(3)

	manifest, manifestPath, err := handle.Close(startedAt.Add(time.Second))
	if err != nil {
		t.Fatalf("close segment: %v", err)
	}
	if manifest.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d", manifest.SchemaVersion)
	}
	if manifest.SourceType != "sensor_raw" {
		t.Fatalf("source_type = %q, want sensor_raw", manifest.SourceType)
	}
	if manifest.SampleCount != 2 || manifest.DroppedSamples != 3 {
		t.Fatalf("counts = %d/%d", manifest.SampleCount, manifest.DroppedSamples)
	}
	if manifest.Bytes <= 0 {
		t.Fatalf("bytes = %d, want > 0", manifest.Bytes)
	}
	if !strings.Contains(manifest.Path, filepath.Join("segments", "2026-06-01")) {
		t.Fatalf("segment path = %q", manifest.Path)
	}

	data, err := os.ReadFile(manifest.Path)
	if err != nil {
		t.Fatalf("read segment: %v", err)
	}
	if got := strings.Count(string(data), "\n"); got != 2 {
		t.Fatalf("line count = %d, want 2; data=%q", got, data)
	}

	rawManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var decoded SegmentManifest
	if err := json.Unmarshal(rawManifest, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if decoded.SegmentID != manifest.SegmentID || decoded.Path != manifest.Path {
		t.Fatalf("decoded manifest = %+v, original = %+v", decoded, manifest)
	}
}

func TestSegmentWriterRejectsInvalidConfig(t *testing.T) {
	if _, err := NewSegmentWriter(SegmentWriterConfig{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestSegmentWriterAbortRemovesTempFile(t *testing.T) {
	root := t.TempDir()
	writer, err := NewSegmentWriter(SegmentWriterConfig{
		SegmentDir:  filepath.Join(root, "segments"),
		ManifestDir: filepath.Join(root, "manifests"),
		SourceID:    "joints",
		DataType:    "joint_state",
	})
	if err != nil {
		t.Fatalf("new segment writer: %v", err)
	}
	handle, err := writer.Open(time.Date(2026, 6, 1, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	tempPath := handle.tempPath
	if err := handle.WriteJSON(map[string]any{"seq": 1}); err != nil {
		t.Fatalf("write sample: %v", err)
	}
	if err := handle.Abort(); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("temp path still exists or unexpected err: %v", err)
	}
}
