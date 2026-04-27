package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"watchdog/internal/actions"
	"watchdog/internal/health"
	"watchdog/internal/supervisor"
)

func TestPrintStatusFormatsReadableSummary(t *testing.T) {
	now := time.Date(2026, 4, 20, 4, 40, 30, 0, time.UTC)
	status := supervisor.StatusView{
		State: supervisor.State{
			SchemaVersion: 1,
			UpdatedAt:     time.Date(2026, 4, 20, 4, 40, 28, 0, time.UTC),
			LastRequestID: "20260420T044028.797341374Z-transition-degrade-planner",
			OverallAction: actions.ActionDegrade,
			LastHookAt: supervisor.HookTimes{
				Degrade: time.Date(2026, 4, 20, 4, 40, 28, 0, time.UTC),
			},
			ActiveComponents: []supervisor.ComponentState{
				{
					ComponentID:      "planner",
					ActiveAction:     actions.ActionDegrade,
					Severity:         health.SeverityStale,
					Reason:           "module stale: last report 2.12s ago > stale_after 1.50s; last reported warn: deadline miss",
					SourceTypes:      []string{"module"},
					Latched:          true,
					FirstActivatedAt: time.Date(2026, 4, 20, 4, 40, 27, 0, time.UTC),
					LastRequestAt:    time.Date(2026, 4, 20, 4, 40, 28, 0, time.UTC),
				},
			},
		},
		Latest: &supervisor.AuditRecord{
			Request: actions.Request{
				RequestID:       "20260420T044028.797341374Z-transition-degrade-planner",
				Event:           actions.EventTransition,
				Timestamp:       time.Date(2026, 4, 20, 4, 40, 28, 0, time.UTC),
				Overall:         health.SeverityStale,
				RequestedAction: actions.ActionDegrade,
				Reason:          "degrade requested for planner: module stale: last report 2.12s ago > stale_after 1.50s; last reported warn: deadline miss",
			},
			Decision: supervisor.ApplyResult{
				ChangedComponents: []string{"planner"},
			},
			Hook: &supervisor.HookResult{
				Action:   actions.ActionDegrade,
				Executed: false,
			},
		},
	}

	var buf bytes.Buffer
	printStatus(&buf, status, now, statusOptions{})
	got := buf.String()

	for _, want := range []string{
		"Overall: degrade",
		"Active: 1 component",
		"Active Components",
		"1. planner",
		"State: stale -> degrade [latched]",
		"Sources: module",
		"Reason:",
		"- module stale: last report 2.12s ago > stale_after 1.50s",
		"- last reported warn: deadline miss",
		"Latest Request",
		"Changed: planner",
		"Hook: not configured",
		"Hook Timestamps",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q\n%s", want, got)
		}
	}
}

func TestPrintStatusHandlesEmptyState(t *testing.T) {
	now := time.Date(2026, 4, 20, 4, 40, 30, 0, time.UTC)
	status := supervisor.StatusView{
		State: supervisor.State{
			SchemaVersion: 1,
		},
	}

	var buf bytes.Buffer
	printStatus(&buf, status, now, statusOptions{})
	got := buf.String()

	for _, want := range []string{
		"Overall: none",
		"Active: 0 components",
		"Active Components",
		"  none",
		"Latest Request",
		"Hook Timestamps",
		"  resolve: -",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q\n%s", want, got)
		}
	}
}

func TestPrintStatusVerboseShowsIncidentAndRequestDetails(t *testing.T) {
	now := time.Date(2026, 4, 27, 5, 43, 10, 0, time.UTC)
	status := supervisor.StatusView{
		State: supervisor.State{
			SchemaVersion: 1,
			UpdatedAt:     time.Date(2026, 4, 27, 5, 42, 56, 0, time.UTC),
			OverallAction: actions.ActionDegrade,
			LastHookAt: supervisor.HookTimes{
				Notify:  time.Date(2026, 4, 27, 5, 42, 2, 0, time.UTC),
				Degrade: time.Date(2026, 4, 27, 5, 42, 56, 0, time.UTC),
			},
			ActiveComponents: []supervisor.ComponentState{
				{
					ComponentID:      "planner",
					ActiveAction:     actions.ActionDegrade,
					Severity:         health.SeverityStale,
					Reason:           "module stale: last report 1.95s ago > stale_after 1.50s; last reported warn: deadline miss",
					SourceTypes:      []string{"module"},
					Latched:          true,
					FirstActivatedAt: time.Date(2026, 4, 27, 5, 42, 2, 0, time.UTC),
					LastRequestAt:    time.Date(2026, 4, 27, 5, 42, 56, 0, time.UTC),
				},
			},
		},
		Latest: &supervisor.AuditRecord{
			Request: actions.Request{
				RequestID:       "20260427T054256.051279240Z-transition-degrade-planner",
				Event:           actions.EventTransition,
				Timestamp:       time.Date(2026, 4, 27, 5, 42, 56, 0, time.UTC),
				Overall:         health.SeverityStale,
				PreviousOverall: health.SeverityWarn,
				RequestedAction: actions.ActionDegrade,
				Reason:          "degrade requested for planner: module stale: last report 1.95s ago > stale_after 1.50s; last reported warn: deadline miss",
				IncidentPath:    "/var/lib/watchdog/incidents/20260427T054256Z_stale.json",
				Components: []actions.ComponentRequest{
					{
						ComponentID:     "planner",
						Severity:        health.SeverityStale,
						RequestedAction: actions.ActionDegrade,
						Reason:          "module stale: last report 1.95s ago > stale_after 1.50s; last reported warn: deadline miss",
						SourceTypes:     []string{"module"},
					},
				},
				Errors: []string{"sample request error"},
			},
			Decision: supervisor.ApplyResult{
				ChangedComponents: []string{"planner"},
			},
			Hook: &supervisor.HookResult{
				Action:            actions.ActionDegrade,
				Executed:          false,
				Suppressed:        true,
				SuppressionReason: "degrade cooldown active",
				Command:           []string{"/usr/local/bin/mock-hook", "degrade"},
				Stdout:            "hook stdout",
				Stderr:            "hook stderr",
			},
		},
	}
	incident := &health.Snapshot{
		Hostname:    "robot01",
		CollectedAt: time.Date(2026, 4, 27, 5, 42, 56, 0, time.UTC),
		Overall:     health.SeverityStale,
		Statuses: []health.Status{
			{
				SourceID:   "planner",
				SourceType: "module",
				Severity:   health.SeverityStale,
				Reason:     "module stale: last report 1.95s ago > stale_after 1.50s; last reported warn: deadline miss",
				ObservedAt: time.Date(2026, 4, 27, 5, 42, 56, 0, time.UTC),
				Metrics: map[string]float64{
					"age.s":            1.95,
					"deadline_miss_ms": 18.5,
					"stale_after.s":    1.5,
				},
				Labels: map[string]string{
					"process": "planner_main",
				},
			},
		},
		Components: []health.ComponentStatus{
			{
				ComponentID: "planner",
				Severity:    health.SeverityStale,
				Reason:      "module stale: last report 1.95s ago > stale_after 1.50s; last reported warn: deadline miss",
				ObservedAt:  time.Date(2026, 4, 27, 5, 42, 56, 0, time.UTC),
				Sources: []health.ComponentSource{{
					SourceType: "module",
					Severity:   health.SeverityStale,
					Reason:     "module stale: last report 1.95s ago > stale_after 1.50s; last reported warn: deadline miss",
					ObservedAt: time.Date(2026, 4, 27, 5, 42, 56, 0, time.UTC),
				}},
			},
		},
		Errors: []string{"sample incident error"},
	}

	var buf bytes.Buffer
	printStatus(&buf, status, now, statusOptions{
		verbose:      true,
		incident:     incident,
		incidentPath: "/var/lib/watchdog/incidents/20260427T054256Z_stale.json",
	})
	got := buf.String()

	for _, want := range []string{
		"Incident: /var/lib/watchdog/incidents/20260427T054256Z_stale.json",
		"Previous overall: warn",
		"Request errors:",
		"Requested components:",
		"State: stale -> degrade",
		"Hook command: /usr/local/bin/mock-hook degrade",
		"Hook suppression: degrade cooldown active",
		"Hook stdout:",
		"Hook stderr:",
		"Incident Snapshot",
		"Raw statuses: 1",
		"Status details:",
		"Metrics:",
		"- deadline_miss_ms=18.5",
		"Labels:",
		"- process=planner_main",
		"Component details:",
		"Errors:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("verbose output missing %q\n%s", want, got)
		}
	}
}

func TestPrintStatusVerboseShowsIncidentLoadError(t *testing.T) {
	now := time.Date(2026, 4, 27, 5, 43, 10, 0, time.UTC)
	status := supervisor.StatusView{}
	var buf bytes.Buffer
	printStatus(&buf, status, now, statusOptions{verbose: true, incidentErr: errors.New("boom")})
	got := buf.String()
	if !strings.Contains(got, "load error: boom") {
		t.Fatalf("output missing incident load error\n%s", got)
	}
}
