package manager

import (
	"sync"
	"testing"
	"time"

	"watchdog/internal/dtc"
)

type captureSink struct {
	mu     sync.Mutex
	events []dtc.DtcEvent
}

func (c *captureSink) Record(ev dtc.DtcEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
	return nil
}

func (c *captureSink) count(code string, tr dtc.Transition) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.events {
		if e.Code == code && e.Transition == tr {
			n++
		}
	}
	return n
}

func report(source string, at time.Time, deadlineMS int64, faults ...dtc.Fault) dtc.FaultReport {
	return dtc.FaultReport{
		SchemaVersion: dtc.SchemaVersion,
		SourceID:      source,
		PublishedAt:   at,
		DeadlineMS:    deadlineMS,
		Active:        faults,
	}
}

func TestIngestEmitsOpenedAndClosed(t *testing.T) {
	sink := &captureSink{}
	m := New("robot-1", Options{}, sink)
	t0 := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

	f := dtc.Fault{Code: "P1310", Severity: dtc.SeverityWarn, Since: t0}
	if err := m.Ingest(report("s1", t0, 1000, f), t0); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if sink.count("P1310", dtc.TransitionOpened) != 1 {
		t.Fatalf("expected 1 opened event for P1310")
	}
	// Next report no longer carries P1310 -> closed.
	if err := m.Ingest(report("s1", t0.Add(time.Second), 1000), t0.Add(time.Second)); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if sink.count("P1310", dtc.TransitionClosed) != 1 {
		t.Fatalf("expected 1 closed event for P1310")
	}
}

func TestEvaluateLivenessAndRecovery(t *testing.T) {
	sink := &captureSink{}
	m := New("robot-1", Options{}, sink)
	t0 := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

	f := dtc.Fault{Code: "P1310", Severity: dtc.SeverityWarn, Since: t0}
	if err := m.Ingest(report("s1", t0, 1000, f), t0); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	alive := m.Evaluate(t0.Add(500 * time.Millisecond))
	if len(alive.Sources) != 1 || !alive.Sources[0].Alive {
		t.Fatalf("source should be alive within deadline: %+v", alive.Sources)
	}
	if len(alive.Active) != 1 || alive.Active[0].Code != "P1310" {
		t.Fatalf("alive aggregate should carry the real fault: %+v", alive.Active)
	}

	stale := m.Evaluate(t0.Add(2 * time.Second))
	if stale.Sources[0].Alive {
		t.Fatalf("source should be stale past deadline")
	}
	if len(stale.Active) != 1 || stale.Active[0].Code != SourceHungCode {
		t.Fatalf("stale aggregate should carry SourceHungCode, got %+v", stale.Active)
	}
	if sink.count(SourceHungCode, dtc.TransitionOpened) != 1 {
		t.Fatalf("expected a source-hung opened event")
	}

	// A fresh report recovers liveness.
	if err := m.Ingest(report("s1", t0.Add(3*time.Second), 1000), t0.Add(3*time.Second)); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if sink.count(SourceHungCode, dtc.TransitionClosed) != 1 {
		t.Fatalf("expected a source-hung closed (recovered) event")
	}
	recovered := m.Evaluate(t0.Add(3*time.Second + 100*time.Millisecond))
	if !recovered.Sources[0].Alive {
		t.Fatalf("source should be alive after fresh report")
	}
}

func TestNotificationsCapPerCode(t *testing.T) {
	m := New("robot-1", Options{NoticeCap: 2}, nil)
	now := time.Now()
	rep := dtc.DiagnosticsReport{Active: []dtc.Fault{
		{Code: "X", Severity: dtc.SeverityWarn},
		{Code: "X", Severity: dtc.SeverityFatal},
		{Code: "X", Severity: dtc.SeverityWarn},
		{Code: "Y", Severity: dtc.SeverityInfo},
	}}
	n := m.Notifications(rep, now)
	countX, countY := 0, 0
	for _, notif := range n {
		switch notif.Code {
		case "X":
			countX++
		case "Y":
			countY++
		}
	}
	if countX != 2 {
		t.Fatalf("code X should be capped at 2, got %d", countX)
	}
	if countY != 1 {
		t.Fatalf("code Y should have 1, got %d", countY)
	}
	// Highest severity survives the cap (FATAL first).
	for _, notif := range n {
		if notif.Code == "X" && notif.Severity == dtc.SeverityFatal {
			return
		}
	}
	t.Fatalf("capped X notifications should include the FATAL instance")
}

func TestIngestRejectsInvalidReport(t *testing.T) {
	m := New("robot-1", Options{}, nil)
	if err := m.Ingest(dtc.FaultReport{SchemaVersion: 1}, time.Now()); err == nil {
		t.Fatal("expected rejection of report with empty source_id")
	}
}
