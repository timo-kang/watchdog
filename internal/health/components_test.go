package health

import (
	"testing"
	"time"
)

func TestBuildComponentsGroupsBySourceID(t *testing.T) {
	now := time.Now()
	components := BuildComponents([]Status{
		{
			SourceID:   "planner",
			SourceType: "module",
			Severity:   SeverityStale,
			Reason:     "heartbeat expired",
			ObservedAt: now.Add(-2 * time.Second),
		},
		{
			SourceID:   "planner",
			SourceType: "process",
			Severity:   SeverityFail,
			Reason:     "service failed",
			ObservedAt: now,
		},
		{
			SourceID:   "host",
			SourceType: "host",
			Severity:   SeverityWarn,
			Reason:     "cpu temp high",
			ObservedAt: now.Add(-time.Second),
		},
	})

	if len(components) != 2 {
		t.Fatalf("len(components) = %d, want 2", len(components))
	}

	planner := components[1]
	if planner.ComponentID != "planner" {
		t.Fatalf("planner component id = %q, want planner", planner.ComponentID)
	}
	if planner.Severity != SeverityStale {
		t.Fatalf("planner severity = %s, want %s", planner.Severity, SeverityStale)
	}
	if planner.Reason != "module stale: heartbeat expired; process fail: service failed" {
		t.Fatalf("planner reason = %q", planner.Reason)
	}
	if len(planner.Sources) != 2 {
		t.Fatalf("len(planner.Sources) = %d, want 2", len(planner.Sources))
	}
	if planner.Sources[0].SourceType != "module" {
		t.Fatalf("planner primary source type = %q, want module", planner.Sources[0].SourceType)
	}
}

func TestBuildComponentsPrefersProcessForSameSeverity(t *testing.T) {
	now := time.Now()
	components := BuildComponents([]Status{
		{
			SourceID:   "planner",
			SourceType: "module",
			Severity:   SeverityWarn,
			Reason:     "deadline miss",
			ObservedAt: now,
		},
		{
			SourceID:   "planner",
			SourceType: "process",
			Severity:   SeverityWarn,
			Reason:     "restarting",
			ObservedAt: now.Add(-time.Second),
		},
	})

	if got := components[0].Reason; got != "process warn: restarting; module warn: deadline miss" {
		t.Fatalf("component reason = %q", got)
	}
}

func TestOverallFromComponents(t *testing.T) {
	overall := OverallFromComponents([]ComponentStatus{
		{ComponentID: "host", Severity: SeverityOK},
		{ComponentID: "planner", Severity: SeverityWarn},
		{ComponentID: "camera", Severity: SeverityFail},
	})

	if overall != SeverityFail {
		t.Fatalf("overall = %s, want %s", overall, SeverityFail)
	}
}
