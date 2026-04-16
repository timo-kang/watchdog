package health

import (
	"fmt"
	"time"
)

type Severity string

const (
	SeverityOK    Severity = "ok"
	SeverityWarn  Severity = "warn"
	SeverityFail  Severity = "fail"
	SeverityStale Severity = "stale"
)

func ParseSeverity(value string) (Severity, error) {
	severity := Severity(value)
	switch severity {
	case SeverityOK, SeverityWarn, SeverityFail, SeverityStale:
		return severity, nil
	default:
		return "", fmt.Errorf("unknown severity %q", value)
	}
}

func CompareSeverity(a, b Severity) int {
	return severityRank(a) - severityRank(b)
}

func MaxSeverity(values ...Severity) Severity {
	out := SeverityOK
	for _, v := range values {
		if CompareSeverity(v, out) > 0 {
			out = v
		}
	}
	return out
}

func severityRank(value Severity) int {
	switch value {
	case SeverityStale:
		return 3
	case SeverityFail:
		return 2
	case SeverityWarn:
		return 1
	default:
		return 0
	}
}

type Observation struct {
	SourceID         string             `json:"source_id"`
	SourceType       string             `json:"source_type"`
	CollectedAt      time.Time          `json:"collected_at"`
	Metrics          map[string]float64 `json:"metrics,omitempty"`
	Labels           map[string]string  `json:"labels,omitempty"`
	ReportedSeverity Severity           `json:"-"`
	ReportedReason   string             `json:"-"`
	StaleAfter       time.Duration      `json:"-"`
}

type Status struct {
	SourceID   string             `json:"source_id"`
	SourceType string             `json:"source_type"`
	Severity   Severity           `json:"severity"`
	Reason     string             `json:"reason,omitempty"`
	ObservedAt time.Time          `json:"observed_at"`
	Metrics    map[string]float64 `json:"metrics,omitempty"`
	Labels     map[string]string  `json:"labels,omitempty"`
}

type ComponentSource struct {
	SourceType string    `json:"source_type"`
	Severity   Severity  `json:"severity"`
	Reason     string    `json:"reason,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
}

type ComponentStatus struct {
	ComponentID string            `json:"component_id"`
	Severity    Severity          `json:"severity"`
	Reason      string            `json:"reason,omitempty"`
	ObservedAt  time.Time         `json:"observed_at"`
	Sources     []ComponentSource `json:"sources,omitempty"`
}

type Snapshot struct {
	Hostname    string            `json:"hostname"`
	CollectedAt time.Time         `json:"collected_at"`
	Overall     Severity          `json:"overall"`
	Statuses    []Status          `json:"statuses"`
	Components  []ComponentStatus `json:"components,omitempty"`
	Errors      []string          `json:"errors,omitempty"`
}
