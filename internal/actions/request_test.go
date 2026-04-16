package actions

import (
	"testing"
	"time"

	"watchdog/internal/health"
)

func TestBuildRequestEtherCATLostSlaveRequestsSafeStop(t *testing.T) {
	now := time.Now()
	snapshot := health.Snapshot{
		Hostname:    "robot-1",
		CollectedAt: now,
		Overall:     health.SeverityFail,
		Statuses: []health.Status{
			{
				SourceID:   "actuators",
				SourceType: "ethercat",
				Severity:   health.SeverityFail,
				Reason:     "lost slave 12",
				ObservedAt: now,
				Metrics: map[string]float64{
					"ethercat.link_known":      1,
					"ethercat.link_up":         1,
					"ethercat.require_link":    1,
					"ethercat.slaves_lost":     1,
					"ethercat.expected_slaves": 12,
					"ethercat.slaves_seen":     11,
				},
				Labels: map[string]string{
					"master_state":   "op",
					"expected_state": "op",
				},
			},
		},
		Components: []health.ComponentStatus{
			{
				ComponentID: "actuators",
				Severity:    health.SeverityFail,
				Reason:      "ethercat fail: lost slave 12",
				ObservedAt:  now,
				Sources: []health.ComponentSource{
					{SourceType: "ethercat", Severity: health.SeverityFail, Reason: "lost slave 12", ObservedAt: now},
				},
			},
		},
	}

	request, ok := BuildRequest(nil, snapshot, "/tmp/incident.json", true)
	if !ok {
		t.Fatal("expected request")
	}
	if request.RequestedAction != ActionSafeStop {
		t.Fatalf("requested_action = %s, want %s", request.RequestedAction, ActionSafeStop)
	}
	if request.RequestID == "" {
		t.Fatal("expected request_id")
	}
	if len(request.Components) != 1 || request.Components[0].RequestedAction != ActionSafeStop {
		t.Fatalf("component action = %#v", request.Components)
	}
}

func TestBuildRequestWarnHostRequestsNotify(t *testing.T) {
	now := time.Now()
	snapshot := health.Snapshot{
		Hostname:    "robot-1",
		CollectedAt: now,
		Overall:     health.SeverityWarn,
		Statuses: []health.Status{
			{
				SourceID:   "host",
				SourceType: "host",
				Severity:   health.SeverityWarn,
				Reason:     "cpu temp high",
				ObservedAt: now,
			},
		},
		Components: []health.ComponentStatus{
			{
				ComponentID: "host",
				Severity:    health.SeverityWarn,
				Reason:      "host warn: cpu temp high",
				ObservedAt:  now,
				Sources: []health.ComponentSource{
					{SourceType: "host", Severity: health.SeverityWarn, Reason: "cpu temp high", ObservedAt: now},
				},
			},
		},
	}

	request, ok := BuildRequest(nil, snapshot, "", true)
	if !ok {
		t.Fatal("expected request")
	}
	if request.RequestedAction != ActionNotify {
		t.Fatalf("requested_action = %s, want %s", request.RequestedAction, ActionNotify)
	}
}

func TestBuildRequestResolvedSendsResolve(t *testing.T) {
	now := time.Now()
	previous := &health.Snapshot{
		Hostname:    "robot-1",
		CollectedAt: now.Add(-time.Second),
		Overall:     health.SeverityFail,
		Statuses: []health.Status{
			{SourceID: "actuators", SourceType: "ethercat", Severity: health.SeverityFail, Reason: "lost slave", ObservedAt: now.Add(-time.Second)},
		},
		Components: []health.ComponentStatus{
			{
				ComponentID: "actuators",
				Severity:    health.SeverityFail,
				Reason:      "ethercat fail: lost slave",
				ObservedAt:  now.Add(-time.Second),
				Sources: []health.ComponentSource{
					{SourceType: "ethercat", Severity: health.SeverityFail, Reason: "lost slave", ObservedAt: now.Add(-time.Second)},
				},
			},
		},
	}
	next := health.Snapshot{
		Hostname:    "robot-1",
		CollectedAt: now,
		Overall:     health.SeverityOK,
	}

	request, ok := BuildRequest(previous, next, "", true)
	if !ok {
		t.Fatal("expected resolve request")
	}
	if request.Event != EventResolved || request.RequestedAction != ActionResolve {
		t.Fatalf("request = %#v", request)
	}
	if request.RequestID == "" {
		t.Fatal("expected request_id")
	}
	if len(request.Resolved) != 1 || request.Resolved[0] != "actuators" {
		t.Fatalf("resolved = %#v", request.Resolved)
	}
}

func TestBuildRequestSkipsEquivalentState(t *testing.T) {
	now := time.Now()
	previous := &health.Snapshot{
		Hostname:    "robot-1",
		CollectedAt: now.Add(-time.Second),
		Overall:     health.SeverityWarn,
		Statuses: []health.Status{
			{SourceID: "host", SourceType: "host", Severity: health.SeverityWarn, Reason: "cpu temp", ObservedAt: now.Add(-time.Second)},
		},
		Components: []health.ComponentStatus{
			{
				ComponentID: "host",
				Severity:    health.SeverityWarn,
				Reason:      "host warn: cpu temp",
				ObservedAt:  now.Add(-time.Second),
				Sources: []health.ComponentSource{
					{SourceType: "host", Severity: health.SeverityWarn, Reason: "cpu temp", ObservedAt: now.Add(-time.Second)},
				},
			},
		},
	}
	next := health.Snapshot{
		Hostname:    "robot-1",
		CollectedAt: now,
		Overall:     health.SeverityWarn,
		Statuses: []health.Status{
			{SourceID: "host", SourceType: "host", Severity: health.SeverityWarn, Reason: "cpu temp", ObservedAt: now},
		},
		Components: []health.ComponentStatus{
			{
				ComponentID: "host",
				Severity:    health.SeverityWarn,
				Reason:      "host warn: cpu temp",
				ObservedAt:  now,
				Sources: []health.ComponentSource{
					{SourceType: "host", Severity: health.SeverityWarn, Reason: "cpu temp", ObservedAt: now},
				},
			},
		},
	}

	if _, ok := BuildRequest(previous, next, "", true); ok {
		t.Fatal("expected no request for equivalent state")
	}
}

func TestBuildRequestSkipsEquivalentActionWhenOnlyReasonTextChanges(t *testing.T) {
	now := time.Now()
	previous := &health.Snapshot{
		Hostname:    "robot-1",
		CollectedAt: now.Add(-time.Second),
		Overall:     health.SeverityStale,
		Statuses: []health.Status{
			{SourceID: "planner", SourceType: "module", Severity: health.SeverityStale, Reason: "last report 2.00s ago", ObservedAt: now.Add(-time.Second)},
		},
		Components: []health.ComponentStatus{
			{
				ComponentID: "planner",
				Severity:    health.SeverityStale,
				Reason:      "module stale: last report 2.00s ago > stale_after 1.50s; last reported warn: deadline miss",
				ObservedAt:  now.Add(-time.Second),
				Sources: []health.ComponentSource{
					{SourceType: "module", Severity: health.SeverityStale, Reason: "last report 2.00s ago", ObservedAt: now.Add(-time.Second)},
				},
			},
		},
	}
	next := health.Snapshot{
		Hostname:    "robot-1",
		CollectedAt: now,
		Overall:     health.SeverityStale,
		Statuses: []health.Status{
			{SourceID: "planner", SourceType: "module", Severity: health.SeverityStale, Reason: "last report 3.00s ago", ObservedAt: now},
		},
		Components: []health.ComponentStatus{
			{
				ComponentID: "planner",
				Severity:    health.SeverityStale,
				Reason:      "module stale: last report 3.00s ago > stale_after 1.50s; last reported warn: deadline miss",
				ObservedAt:  now,
				Sources: []health.ComponentSource{
					{SourceType: "module", Severity: health.SeverityStale, Reason: "last report 3.00s ago", ObservedAt: now},
				},
			},
		},
	}

	if _, ok := BuildRequest(previous, next, "", true); ok {
		t.Fatal("expected no request when only reason text changes inside the same stale/degrade state")
	}
}
