// Package dtc defines the transport-agnostic v1 fault-diagnosis (DTC) data model:
// the neutral types a source reports, a manager aggregates, a recorder logs, and
// an operator surface displays. JSON is the canonical serialization; any transport
// (Unix datagram, ROS2, shared memory) carries these same shapes. Nothing here
// decides or executes an action — the model carries facts only.
package dtc

import (
	"fmt"
	"time"
)

// SchemaVersion is the frozen v1 wire version. Compatibility policy: unknown
// fields are ignored (forward-compatible); new fields are added optional-only;
// a change that breaks existing producers bumps this to 2 with a new fixture set.
const SchemaVersion = 1

// Severity is the DTC severity level.
type Severity string

const (
	SeverityInfo  Severity = "INFO"
	SeverityWarn  Severity = "WARN"
	SeverityFatal Severity = "FATAL"
)

// Valid reports whether s is a known severity.
func (s Severity) Valid() bool {
	switch s {
	case SeverityInfo, SeverityWarn, SeverityFatal:
		return true
	default:
		return false
	}
}

// Transition is the direction of a fault state change.
type Transition string

const (
	TransitionOpened Transition = "opened"
	TransitionClosed Transition = "closed"
)

// Valid reports whether t is a known transition.
func (t Transition) Valid() bool {
	return t == TransitionOpened || t == TransitionClosed
}

// FaultUnit identifies one affected part of the robot.
type FaultUnit struct {
	Part     string `json:"part"`               // required: stable part identity, e.g. "joint.left_hip"
	Instance string `json:"instance,omitempty"` // optional: instance discriminator when a part repeats
}

// Fault is one active DTC.
type Fault struct {
	Code     string      `json:"code"`             // required: catalog code, e.g. "P1310"
	Severity Severity    `json:"severity"`         // required: INFO | WARN | FATAL
	Since    time.Time   `json:"since"`            // when it became active
	Units    []FaultUnit `json:"units,omitempty"`  // affected parts
	Detail   string      `json:"detail,omitempty"` // optional human-readable detail
}

// FaultReport is a source's periodic publication of its currently-active faults.
// It doubles as the source's liveness heartbeat: a consumer that does not receive
// a report within DeadlineMS treats the source as hung — itself a fault. A source
// publishes at a steady cadence and immediately on any fault transition.
type FaultReport struct {
	SchemaVersion int       `json:"schema_version"`     // must equal SchemaVersion
	SourceID      string    `json:"source_id"`          // required: stable source identity
	Instance      string    `json:"instance,omitempty"` // optional: instance discriminator
	Sequence      uint64    `json:"sequence"`           // monotonic per (source_id, instance)
	PublishedAt   time.Time `json:"published_at"`       // publication timestamp
	DeadlineMS    int64     `json:"deadline_ms"`        // liveness deadline; hung if no report within this
	Active        []Fault   `json:"active"`             // currently active faults (may be empty)
}

// IsStale reports whether the report is older than its liveness deadline at now.
// A DeadlineMS of 0 disables liveness (never stale).
func (r FaultReport) IsStale(now time.Time) bool {
	if r.DeadlineMS <= 0 {
		return false
	}
	return now.Sub(r.PublishedAt) > time.Duration(r.DeadlineMS)*time.Millisecond
}

// Validate reports whether the report is a conformant v1 source publication.
// This is the source-facing acceptance contract, pinned by the conformance suite.
func (r FaultReport) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version %d unsupported (want %d)", r.SchemaVersion, SchemaVersion)
	}
	if r.SourceID == "" {
		return fmt.Errorf("source_id is required")
	}
	if r.DeadlineMS < 0 {
		return fmt.Errorf("deadline_ms must not be negative")
	}
	for i, f := range r.Active {
		if err := f.Validate(); err != nil {
			return fmt.Errorf("active[%d]: %w", i, err)
		}
	}
	return nil
}

// Validate reports whether the fault is well-formed.
func (f Fault) Validate() error {
	if f.Code == "" {
		return fmt.Errorf("code is required")
	}
	if !f.Severity.Valid() {
		return fmt.Errorf("severity %q is invalid", f.Severity)
	}
	for i, u := range f.Units {
		if u.Part == "" {
			return fmt.Errorf("units[%d].part is required", i)
		}
	}
	return nil
}

// SourceLiveness is the manager's per-source liveness view.
type SourceLiveness struct {
	SourceID   string    `json:"source_id"`
	Instance   string    `json:"instance,omitempty"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Alive      bool      `json:"alive"`
}

// DiagnosticsReport is a per-robot aggregate produced by the manager.
type DiagnosticsReport struct {
	SchemaVersion int              `json:"schema_version"`
	RobotID       string           `json:"robot_id"`
	GeneratedAt   time.Time        `json:"generated_at"`
	Sources       []SourceLiveness `json:"sources"`
	Active        []Fault          `json:"active"`
}

// DtcEvent is a fault transition (opened/closed) recorded for the event log and
// the event data recorder.
type DtcEvent struct {
	SchemaVersion int         `json:"schema_version"`
	Code          string      `json:"code"`
	Severity      Severity    `json:"severity"`
	Transition    Transition  `json:"transition"`
	At            time.Time   `json:"at"`
	SourceID      string      `json:"source_id"`
	Units         []FaultUnit `json:"units,omitempty"`
}

// DiagnosticsEpisode groups the DTC events of one diagnostics session.
type DiagnosticsEpisode struct {
	SchemaVersion int        `json:"schema_version"`
	EpisodeID     string     `json:"episode_id"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	Events        []DtcEvent `json:"events"`
}

// Notification is an operator-facing message derived from a fault.
type Notification struct {
	Code     string    `json:"code"`
	Severity Severity  `json:"severity"`
	Title    string    `json:"title"`
	Message  string    `json:"message"`
	At       time.Time `json:"at"`
}

// GetDiagnosticsHistoryRequest queries recorded DTC events.
type GetDiagnosticsHistoryRequest struct {
	Since *time.Time `json:"since,omitempty"`
	Until *time.Time `json:"until,omitempty"`
	Codes []string   `json:"codes,omitempty"`
	Limit int        `json:"limit,omitempty"`
}

// GetDiagnosticsHistoryResponse returns the matching recorded events.
type GetDiagnosticsHistoryResponse struct {
	Events []DtcEvent `json:"events"`
}

// RunSelfCheckRequest asks a source or the manager to run its self-check.
type RunSelfCheckRequest struct {
	Scope string `json:"scope,omitempty"`
}

// RunSelfCheckResponse returns the faults the self-check found.
type RunSelfCheckResponse struct {
	RanAt  time.Time `json:"ran_at"`
	Faults []Fault   `json:"faults"`
}
