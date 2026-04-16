package actions

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"watchdog/internal/health"
)

func TestUnixDatagramSinkSendsRequest(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "supervisor.sock")
	addr := &net.UnixAddr{Name: socketPath, Net: "unixgram"}
	listener, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Fatalf("listen unixgram: %v", err)
	}
	defer listener.Close()

	spoolDir := filepath.Join(t.TempDir(), "spool")
	sink := NewUnixDatagramSink(socketPath, true, spoolDir, 64)
	now := time.Now()
	snapshot := health.Snapshot{
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

	done := make(chan Request, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _, readErr := listener.ReadFromUnix(buf)
		if readErr != nil {
			t.Errorf("read unixgram: %v", readErr)
			return
		}
		var request Request
		if err := json.Unmarshal(buf[:n], &request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		done <- request
	}()

	if err := sink.HandleTransition(context.Background(), nil, snapshot, "/tmp/incident.json"); err != nil {
		t.Fatalf("handle transition: %v", err)
	}

	select {
	case request := <-done:
		if request.RequestedAction != ActionNotify {
			t.Fatalf("requested_action = %s, want %s", request.RequestedAction, ActionNotify)
		}
		if request.RequestID == "" {
			t.Fatal("expected request_id")
		}
		if request.IncidentPath != "/tmp/incident.json" {
			t.Fatalf("incident_path = %q, want /tmp/incident.json", request.IncidentPath)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for action request")
	}
}

func TestUnixDatagramSinkSpoolsAndReplays(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "supervisor.sock")
	spoolDir := filepath.Join(t.TempDir(), "spool")
	sink := NewUnixDatagramSink(socketPath, true, spoolDir, 64)
	now := time.Now()

	first := health.Snapshot{
		Hostname:    "robot-1",
		CollectedAt: now,
		Overall:     health.SeverityFail,
		Statuses: []health.Status{
			{SourceID: "actuators", SourceType: "ethercat", Severity: health.SeverityFail, Reason: "lost slave", ObservedAt: now, Metrics: map[string]float64{"ethercat.slaves_lost": 1}},
		},
		Components: []health.ComponentStatus{
			{
				ComponentID: "actuators",
				Severity:    health.SeverityFail,
				Reason:      "ethercat fail: lost slave",
				ObservedAt:  now,
				Sources: []health.ComponentSource{
					{SourceType: "ethercat", Severity: health.SeverityFail, Reason: "lost slave", ObservedAt: now},
				},
			},
		},
	}

	if err := sink.HandleTransition(context.Background(), nil, first, "/tmp/first.json"); err != nil {
		t.Fatalf("handle transition with missing socket: %v", err)
	}
	entries, err := os.ReadDir(spoolDir)
	if err != nil {
		t.Fatalf("read spool dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}

	addr := &net.UnixAddr{Name: socketPath, Net: "unixgram"}
	listener, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Fatalf("listen unixgram: %v", err)
	}
	defer listener.Close()

	second := health.Snapshot{
		Hostname:    "robot-1",
		CollectedAt: now.Add(time.Second),
		Overall:     health.SeverityWarn,
		Statuses: []health.Status{
			{SourceID: "host", SourceType: "host", Severity: health.SeverityWarn, Reason: "cpu temp", ObservedAt: now.Add(time.Second)},
		},
		Components: []health.ComponentStatus{
			{
				ComponentID: "host",
				Severity:    health.SeverityWarn,
				Reason:      "host warn: cpu temp",
				ObservedAt:  now.Add(time.Second),
				Sources: []health.ComponentSource{
					{SourceType: "host", Severity: health.SeverityWarn, Reason: "cpu temp", ObservedAt: now.Add(time.Second)},
				},
			},
		},
	}

	done := make(chan []Request, 1)
	go func() {
		var requests []Request
		for i := 0; i < 2; i++ {
			buf := make([]byte, 4096)
			n, _, readErr := listener.ReadFromUnix(buf)
			if readErr != nil {
				t.Errorf("read unixgram: %v", readErr)
				return
			}
			var request Request
			if err := json.Unmarshal(buf[:n], &request); err != nil {
				t.Errorf("decode request: %v", err)
				return
			}
			requests = append(requests, request)
		}
		done <- requests
	}()

	if err := sink.HandleTransition(context.Background(), nil, second, "/tmp/second.json"); err != nil {
		t.Fatalf("handle transition with live socket: %v", err)
	}

	select {
	case requests := <-done:
		if len(requests) != 2 {
			t.Fatalf("len(requests) = %d, want 2", len(requests))
		}
		if requests[0].RequestedAction != ActionSafeStop {
			t.Fatalf("first action = %s, want %s", requests[0].RequestedAction, ActionSafeStop)
		}
		if requests[1].RequestedAction != ActionNotify {
			t.Fatalf("second action = %s, want %s", requests[1].RequestedAction, ActionNotify)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for replayed action requests")
	}

	entries, err = os.ReadDir(spoolDir)
	if err != nil {
		t.Fatalf("read spool dir after replay: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(entries) after replay = %d, want 0", len(entries))
	}
}
