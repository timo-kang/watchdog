package logagent

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"watchdog/internal/health"
	"watchdog/internal/rawlog"
)

type HealthReport struct {
	SourceID     string             `json:"source_id"`
	SourceType   string             `json:"source_type"`
	Severity     health.Severity    `json:"severity"`
	Reason       string             `json:"reason,omitempty"`
	ObservedAt   time.Time          `json:"observed_at,omitempty"`
	Metrics      map[string]float64 `json:"metrics,omitempty"`
	Labels       map[string]string  `json:"labels,omitempty"`
	StaleAfterMS int64              `json:"stale_after_ms,omitempty"`
}

type Reporter interface {
	Send(report HealthReport) error
}

type ModuleReporter struct {
	SocketPath string
}

func (r ModuleReporter) Send(report HealthReport) error {
	if r.SocketPath == "" {
		return nil
	}
	if report.SourceType == "" {
		report.SourceType = "log_agent"
	}
	if report.Severity == "" {
		report.Severity = health.SeverityOK
	}
	data, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal health report: %w", err)
	}
	conn, err := net.Dial("unixgram", r.SocketPath)
	if err != nil {
		return fmt.Errorf("dial module socket: %w", err)
	}
	defer conn.Close()
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("send health report: %w", err)
	}
	return nil
}

func SegmentHealthReport(
	healthSourceID string,
	manifest rawlog.SegmentManifest,
	manifestPath string,
	staleAfter time.Duration,
	totalBytes int64,
	totalDropped int64,
	totalErrors int64,
) HealthReport {
	return HealthReport{
		SourceID:     healthSourceID,
		SourceType:   "log_agent",
		Severity:     health.SeverityOK,
		Reason:       "segment written",
		ObservedAt:   manifest.EndedAt,
		StaleAfterMS: staleAfter.Milliseconds(),
		Metrics: map[string]float64{
			"logger.active_sources":              1,
			"logger.bytes_written_total":         float64(totalBytes),
			"logger.dropped_samples_total":       float64(totalDropped),
			"logger.segment_write_errors_total":  float64(totalErrors),
			"logger.latest_segment_bytes":        float64(manifest.Bytes),
			"logger.latest_segment_sample_count": float64(manifest.SampleCount),
			"logger.latest_segment_end_unix":     float64(manifest.EndedAt.Unix()),
		},
		Labels: map[string]string{
			"raw_source_id": manifest.SourceID,
			"data_type":     manifest.DataType,
			"format":        manifest.Format,
			"manifest_path": manifestPath,
			"segment_path":  manifest.Path,
		},
	}
}

func ErrorHealthReport(
	healthSourceID string,
	reason string,
	observedAt time.Time,
	staleAfter time.Duration,
	totalErrors int64,
) HealthReport {
	return HealthReport{
		SourceID:     healthSourceID,
		SourceType:   "log_agent",
		Severity:     health.SeverityFail,
		Reason:       reason,
		ObservedAt:   observedAt.UTC(),
		StaleAfterMS: staleAfter.Milliseconds(),
		Metrics: map[string]float64{
			"logger.active_sources":             1,
			"logger.segment_write_errors_total": float64(totalErrors),
		},
	}
}

func DefaultHealthSourceID(rawSourceID string) string {
	if rawSourceID == "" {
		return "watchdog-log-agent"
	}
	return "watchdog-log-agent." + rawSourceID
}
