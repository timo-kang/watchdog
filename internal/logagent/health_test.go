package logagent

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"watchdog/internal/health"
	"watchdog/internal/rawlog"
)

func TestModuleReporterSendsHealthReport(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "module.sock")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: socketPath, Net: "unixgram"})
	if err != nil {
		t.Fatalf("listen unixgram: %v", err)
	}
	defer conn.Close()

	report := HealthReport{
		SourceID:     "watchdog-log-agent.imu.front",
		SourceType:   "log_agent",
		Severity:     health.SeverityOK,
		Reason:       "segment written",
		ObservedAt:   time.Date(2026, 6, 1, 1, 2, 3, 0, time.UTC),
		StaleAfterMS: 3000,
		Metrics:      map[string]float64{"logger.bytes_written_total": 128},
		Labels:       map[string]string{"raw_source_id": "imu.front"},
	}
	if err := (ModuleReporter{SocketPath: socketPath}).Send(report); err != nil {
		t.Fatalf("send report: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUnix(buf)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var got HealthReport
	if err := json.Unmarshal(buf[:n], &got); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if got.SourceID != report.SourceID || got.SourceType != "log_agent" || got.Severity != health.SeverityOK {
		t.Fatalf("report = %+v", got)
	}
	if got.Metrics["logger.bytes_written_total"] != 128 {
		t.Fatalf("metrics = %+v", got.Metrics)
	}
}

func TestSegmentHealthReportIncludesSegmentEvidence(t *testing.T) {
	endedAt := time.Date(2026, 6, 1, 1, 2, 3, 0, time.UTC)
	report := SegmentHealthReport(
		"watchdog-log-agent.imu.front",
		rawlog.SegmentManifest{
			SourceID:    "imu.front",
			DataType:    "imu",
			Format:      "jsonl",
			Path:        "/var/lib/watchdog/logs/segments/imu.jsonl",
			EndedAt:     endedAt,
			SampleCount: 42,
			Bytes:       2048,
		},
		"/var/lib/watchdog/logs/manifests/imu.json",
		3*time.Second,
		4096,
		2,
		1,
	)
	if report.SourceType != "log_agent" || report.Severity != health.SeverityOK {
		t.Fatalf("report = %+v", report)
	}
	if report.StaleAfterMS != 3000 {
		t.Fatalf("stale_after_ms = %d", report.StaleAfterMS)
	}
	if report.Metrics["logger.latest_segment_sample_count"] != 42 ||
		report.Metrics["logger.segment_write_errors_total"] != 1 {
		t.Fatalf("metrics = %+v", report.Metrics)
	}
	if report.Labels["manifest_path"] == "" || report.Labels["segment_path"] == "" {
		t.Fatalf("labels = %+v", report.Labels)
	}
}
