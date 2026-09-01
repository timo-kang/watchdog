// Package manager is the per-robot DiagnosticsManager: it ingests source
// FaultReports, judges per-instance liveness, aggregates active faults into a
// DiagnosticsReport, groups operator notifications per code with a cap, and emits
// DTC transition events to a durable sink. It carries facts only — it never
// decides or performs a recovery action.
package manager

import (
	"sort"
	"sync"
	"time"

	"watchdog/internal/dtc"
)

// SourceHungCode is the reserved DTC a manager raises for a source whose report
// is older than its advertised liveness deadline (a dead source cannot report its
// own death). Adopters map this code in their catalog.
const SourceHungCode = "source.hung"

// EventSink receives DTC transition events (fault opened/closed, source
// hung/recovered). Implementations persist them; a nil sink is a no-op.
type EventSink interface {
	Record(ev dtc.DtcEvent) error
}

// Options tunes manager behavior.
type Options struct {
	// NoticeCap bounds how many notifications a single code produces per call
	// to Notifications, so many instances of the same fault do not flood the
	// operator. 0 means unlimited.
	NoticeCap int
}

type sourceKey struct {
	SourceID string
	Instance string
}

type sourceState struct {
	last       dtc.FaultReport
	receivedAt time.Time
	active     map[string]dtc.Fault // current active faults by code
	hung       bool                 // last-evaluated liveness state
}

// Manager aggregates the diagnostics of one robot. It is safe for concurrent use.
type Manager struct {
	robotID string
	opts    Options
	sink    EventSink

	mu      sync.Mutex
	sources map[sourceKey]*sourceState
}

// New creates a manager for robotID. sink may be nil.
func New(robotID string, opts Options, sink EventSink) *Manager {
	return &Manager{
		robotID: robotID,
		opts:    opts,
		sink:    sink,
		sources: make(map[sourceKey]*sourceState),
	}
}

// Ingest records a source's latest report and emits opened/closed DTC events for
// faults that changed since that source's previous report. Invalid reports are
// rejected (returned error) and not recorded.
func (m *Manager) Ingest(report dtc.FaultReport, receivedAt time.Time) error {
	if err := report.Validate(); err != nil {
		return err
	}
	key := sourceKey{SourceID: report.SourceID, Instance: report.Instance}

	newActive := make(map[string]dtc.Fault, len(report.Active))
	for _, f := range report.Active {
		newActive[f.Code] = f
	}

	m.mu.Lock()
	st, ok := m.sources[key]
	if !ok {
		st = &sourceState{active: make(map[string]dtc.Fault)}
		m.sources[key] = st
	}
	prev := st.active
	st.last = report
	st.receivedAt = receivedAt
	st.active = newActive
	// A fresh report proves the source is alive again.
	recovered := st.hung
	st.hung = false
	sink := m.sink
	robotSourceID := report.SourceID
	m.mu.Unlock()

	// Emit transitions outside the lock.
	if recovered {
		m.emit(sink, dtc.DtcEvent{
			SchemaVersion: dtc.SchemaVersion,
			Code:          SourceHungCode,
			Severity:      dtc.SeverityFatal,
			Transition:    dtc.TransitionClosed,
			At:            receivedAt,
			SourceID:      robotSourceID,
		})
	}
	for code, f := range newActive {
		if _, existed := prev[code]; !existed {
			m.emit(sink, m.faultEvent(code, f, dtc.TransitionOpened, receivedAt, robotSourceID))
		}
	}
	for code, f := range prev {
		if _, still := newActive[code]; !still {
			m.emit(sink, m.faultEvent(code, f, dtc.TransitionClosed, receivedAt, robotSourceID))
		}
	}
	return nil
}

// Evaluate judges per-instance liveness at now, emits source hung/recovered
// events on transition, and returns the per-robot aggregate. A source whose last
// report is stale is marked not-alive; its stale faults are dropped and replaced
// by a single SourceHungCode fault (the dead source can't be trusted for detail).
func (m *Manager) Evaluate(now time.Time) dtc.DiagnosticsReport {
	m.mu.Lock()
	type transition struct {
		sourceID string
		hung     bool
	}
	var transitions []transition
	report := dtc.DiagnosticsReport{
		SchemaVersion: dtc.SchemaVersion,
		RobotID:       m.robotID,
		GeneratedAt:   now,
	}
	keys := make([]sourceKey, 0, len(m.sources))
	for k := range m.sources {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].SourceID != keys[j].SourceID {
			return keys[i].SourceID < keys[j].SourceID
		}
		return keys[i].Instance < keys[j].Instance
	})
	for _, k := range keys {
		st := m.sources[k]
		stale := st.last.IsStale(now)
		if stale != st.hung {
			st.hung = stale
			transitions = append(transitions, transition{sourceID: k.SourceID, hung: stale})
		}
		report.Sources = append(report.Sources, dtc.SourceLiveness{
			SourceID:   k.SourceID,
			Instance:   k.Instance,
			LastSeenAt: st.receivedAt,
			Alive:      !stale,
		})
		if stale {
			report.Active = append(report.Active, dtc.Fault{
				Code:     SourceHungCode,
				Severity: dtc.SeverityFatal,
				Since:    st.last.PublishedAt,
				Units:    []dtc.FaultUnit{{Part: k.SourceID}},
				Detail:   "source liveness deadline exceeded",
			})
			continue
		}
		codes := make([]string, 0, len(st.active))
		for code := range st.active {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		for _, code := range codes {
			report.Active = append(report.Active, st.active[code])
		}
	}
	sink := m.sink
	m.mu.Unlock()

	for _, tr := range transitions {
		trans := dtc.TransitionOpened
		if !tr.hung {
			trans = dtc.TransitionClosed
		}
		m.emit(sink, dtc.DtcEvent{
			SchemaVersion: dtc.SchemaVersion,
			Code:          SourceHungCode,
			Severity:      dtc.SeverityFatal,
			Transition:    trans,
			At:            now,
			SourceID:      tr.sourceID,
			Units:         []dtc.FaultUnit{{Part: tr.sourceID}},
		})
	}
	return report
}

// Notifications groups the report's active faults by code and produces at most
// NoticeCap notifications per code (0 = unlimited), highest severity first, so a
// fault affecting many instances does not flood the operator.
func (m *Manager) Notifications(report dtc.DiagnosticsReport, now time.Time) []dtc.Notification {
	perCode := map[string][]dtc.Fault{}
	order := []string{}
	for _, f := range report.Active {
		if _, ok := perCode[f.Code]; !ok {
			order = append(order, f.Code)
		}
		perCode[f.Code] = append(perCode[f.Code], f)
	}
	sort.Strings(order)
	var out []dtc.Notification
	for _, code := range order {
		faults := perCode[code]
		sort.SliceStable(faults, func(i, j int) bool {
			return severityRank(faults[i].Severity) > severityRank(faults[j].Severity)
		})
		limit := len(faults)
		if m.opts.NoticeCap > 0 && limit > m.opts.NoticeCap {
			limit = m.opts.NoticeCap
		}
		for i := 0; i < limit; i++ {
			out = append(out, dtc.Notification{
				Code:     code,
				Severity: faults[i].Severity,
				Title:    code,
				Message:  faults[i].Detail,
				At:       now,
			})
		}
	}
	return out
}

func (m *Manager) faultEvent(code string, f dtc.Fault, tr dtc.Transition, at time.Time, sourceID string) dtc.DtcEvent {
	return dtc.DtcEvent{
		SchemaVersion: dtc.SchemaVersion,
		Code:          code,
		Severity:      f.Severity,
		Transition:    tr,
		At:            at,
		SourceID:      sourceID,
		Units:         f.Units,
	}
}

func (m *Manager) emit(sink EventSink, ev dtc.DtcEvent) {
	if sink == nil {
		return
	}
	_ = sink.Record(ev)
}

func severityRank(s dtc.Severity) int {
	switch s {
	case dtc.SeverityFatal:
		return 3
	case dtc.SeverityWarn:
		return 2
	case dtc.SeverityInfo:
		return 1
	default:
		return 0
	}
}
