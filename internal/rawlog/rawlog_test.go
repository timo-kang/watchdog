package rawlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"watchdog/internal/health"
)

func TestLinkIncidentWritesMatchingSegmentIndex(t *testing.T) {
	root := t.TempDir()
	manifestDir := filepath.Join(root, "manifests")
	indexDir := filepath.Join(root, "incident-index")
	if err := os.MkdirAll(filepath.Join(manifestDir, "imu"), 0o755); err != nil {
		t.Fatalf("create manifest dir: %v", err)
	}

	incidentAt := time.Date(2026, 5, 29, 10, 0, 30, 0, time.UTC)
	writeManifest(t, filepath.Join(manifestDir, "imu", "imu-001.json"), SegmentManifest{
		SchemaVersion: 1,
		SegmentID:     "imu-001",
		SourceID:      "robot.imu",
		DataType:      "imu",
		Format:        "mcap",
		Path:          "/var/lib/watchdog/logs/segments/imu-001.mcap",
		StartedAt:     incidentAt.Add(-10 * time.Second),
		EndedAt:       incidentAt.Add(5 * time.Second),
		SampleCount:   1500,
		Bytes:         4096,
	})
	writeManifest(t, filepath.Join(manifestDir, "old-joints.json"), SegmentManifest{
		SchemaVersion: 1,
		SegmentID:     "joint-old",
		SourceID:      "robot.joints",
		DataType:      "joint_state",
		Format:        "mcap",
		Path:          "/var/lib/watchdog/logs/segments/joint-old.mcap",
		StartedAt:     incidentAt.Add(-2 * time.Minute),
		EndedAt:       incidentAt.Add(-90 * time.Second),
		SampleCount:   1000,
		Bytes:         2048,
	})

	linker := Linker{
		ManifestDir:      manifestDir,
		IncidentIndexDir: indexDir,
		PreWindow:        30 * time.Second,
		PostWindow:       30 * time.Second,
	}
	indexPath, err := linker.LinkIncident(
		"/var/lib/watchdog/incidents/20260529T100030Z_fail.json",
		health.Snapshot{CollectedAt: incidentAt},
	)
	if err != nil {
		t.Fatalf("link incident: %v", err)
	}
	if filepath.Base(indexPath) != "20260529T100030Z_fail.rawlog-index.json" {
		t.Fatalf("index path = %q", indexPath)
	}

	index := readIndex(t, indexPath)
	if index.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", index.SchemaVersion)
	}
	if !index.IncidentAt.Equal(incidentAt) {
		t.Fatalf("incident_at = %s, want %s", index.IncidentAt, incidentAt)
	}
	if !index.Window.StartedAt.Equal(incidentAt.Add(-30*time.Second)) ||
		!index.Window.EndedAt.Equal(incidentAt.Add(30*time.Second)) {
		t.Fatalf("window = %+v", index.Window)
	}
	if len(index.Errors) != 0 {
		t.Fatalf("errors = %v", index.Errors)
	}
	if len(index.Segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1: %+v", len(index.Segments), index.Segments)
	}
	segment := index.Segments[0]
	if segment.SegmentID != "imu-001" || segment.SourceID != "robot.imu" || segment.Path == "" {
		t.Fatalf("segment = %+v", segment)
	}
	if !strings.HasSuffix(segment.ManifestPath, filepath.Join("imu", "imu-001.json")) {
		t.Fatalf("manifest_path = %q", segment.ManifestPath)
	}
}

func TestLinkIncidentRecordsManifestErrors(t *testing.T) {
	root := t.TempDir()
	manifestDir := filepath.Join(root, "manifests")
	indexDir := filepath.Join(root, "incident-index")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatalf("create manifest dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(manifestDir, "broken.json"), []byte(`{"schema_version":0}`), 0o644); err != nil {
		t.Fatalf("write broken manifest: %v", err)
	}

	indexPath, err := Linker{
		ManifestDir:      manifestDir,
		IncidentIndexDir: indexDir,
		PreWindow:        time.Second,
		PostWindow:       time.Second,
	}.LinkIncident("/tmp/incidents/fail.json", health.Snapshot{CollectedAt: time.Now()})
	if err != nil {
		t.Fatalf("link incident: %v", err)
	}

	index := readIndex(t, indexPath)
	if len(index.Segments) != 0 {
		t.Fatalf("segments = %+v, want none", index.Segments)
	}
	if len(index.Errors) != 1 || !strings.Contains(index.Errors[0], "unsupported schema_version") {
		t.Fatalf("errors = %v", index.Errors)
	}
}

func TestLinkIncidentSkipsEmptyIncidentPath(t *testing.T) {
	indexPath, err := Linker{
		ManifestDir:      t.TempDir(),
		IncidentIndexDir: t.TempDir(),
	}.LinkIncident("", health.Snapshot{CollectedAt: time.Now()})
	if err != nil {
		t.Fatalf("link incident: %v", err)
	}
	if indexPath != "" {
		t.Fatalf("index path = %q, want empty", indexPath)
	}
}

func writeManifest(t *testing.T, path string, manifest SegmentManifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func readIndex(t *testing.T, path string) IncidentIndex {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var index IncidentIndex
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	return index
}
