package main

import (
	"bytes"
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
	printStatus(&buf, status, now)
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
	printStatus(&buf, status, now)
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
