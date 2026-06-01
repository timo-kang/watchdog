package logagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"watchdog/internal/health"
)

func TestAgentWritesSegmentManifestAndReportsHealth(t *testing.T) {
	root := t.TempDir()
	reporter := &recordingReporter{}
	agent, err := New(Config{
		SegmentDir:        filepath.Join(root, "segments"),
		ManifestDir:       filepath.Join(root, "manifests"),
		SourceID:          "imu.front",
		DataType:          "imu",
		Format:            "jsonl",
		HealthSourceID:    "watchdog-log-agent.imu.front",
		SamplesPerSegment: 2,
		SampleInterval:    0,
		MaxSegments:       1,
		StaleAfter:        3 * time.Second,
		Labels:            map[string]string{"robot": "sim"},
	}, reporter)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	if err := agent.Run(context.Background()); err != nil {
		t.Fatalf("run agent: %v", err)
	}

	manifests, err := filepath.Glob(filepath.Join(root, "manifests", "*.json"))
	if err != nil {
		t.Fatalf("glob manifests: %v", err)
	}
	if len(manifests) != 1 {
		t.Fatalf("manifest count = %d, want 1", len(manifests))
	}
	if len(reporter.reports) != 1 {
		t.Fatalf("report count = %d, want 1", len(reporter.reports))
	}
	report := reporter.reports[0]
	if report.SourceID != "watchdog-log-agent.imu.front" || report.Severity != health.SeverityOK {
		t.Fatalf("report = %+v", report)
	}
	segmentPath := report.Labels["segment_path"]
	if segmentPath == "" {
		t.Fatalf("report labels = %+v", report.Labels)
	}
	data, err := os.ReadFile(segmentPath)
	if err != nil {
		t.Fatalf("read segment: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("segment is empty")
	}
	if report.Metrics["logger.latest_segment_sample_count"] != 2 {
		t.Fatalf("metrics = %+v", report.Metrics)
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	if _, err := New(Config{}, nil); err == nil {
		t.Fatal("expected error")
	}
}

type recordingReporter struct {
	reports []HealthReport
}

func (r *recordingReporter) Send(report HealthReport) error {
	r.reports = append(r.reports, report)
	return nil
}
