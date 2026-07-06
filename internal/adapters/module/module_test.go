package module

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"watchdog/internal/config"
	"watchdog/internal/health"
)

func TestCollectorReceivesReports(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "module.sock")
	collector := New(config.ModuleReportSourceConfig{
		Enabled:           true,
		SocketPath:        socketPath,
		MaxMessageBytes:   2048,
		DefaultStaleAfter: time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := collector.Start(ctx); err != nil {
		t.Fatalf("start collector: %v", err)
	}
	defer func() {
		if err := collector.Stop(context.Background()); err != nil {
			t.Fatalf("stop collector: %v", err)
		}
	}()

	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socketPath, Net: "unixgram"})
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	defer conn.Close()

	payload, err := json.Marshal(map[string]any{
		"source_id": "planner",
		"severity":  "warn",
		"reason":    "deadline miss",
		"metrics": map[string]float64{
			"deadline_miss_ms": 18.5,
		},
		"labels": map[string]string{
			"process": "planner_main",
		},
		"stale_after_ms": 1500,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		observations, err := collector.Collect(context.Background())
		if err != nil {
			t.Fatalf("collect: %v", err)
		}
		if len(observations) == 0 {
			time.Sleep(20 * time.Millisecond)
			continue
		}

		observation := observations[0]
		if observation.SourceID != "planner" {
			t.Fatalf("source_id = %q, want planner", observation.SourceID)
		}
		if observation.SourceType != "module" {
			t.Fatalf("source_type = %q, want module", observation.SourceType)
		}
		if observation.ReportedSeverity != health.SeverityWarn {
			t.Fatalf("severity = %s, want %s", observation.ReportedSeverity, health.SeverityWarn)
		}
		if observation.ReportedReason != "deadline miss" {
			t.Fatalf("reason = %q, want deadline miss", observation.ReportedReason)
		}
		if got := observation.Metrics["deadline_miss_ms"]; got != 18.5 {
			t.Fatalf("deadline_miss_ms = %f, want 18.5", got)
		}
		if got := observation.StaleAfter; got != 1500*time.Millisecond {
			t.Fatalf("stale_after = %s, want 1500ms", got)
		}
		return
	}

	t.Fatal("timed out waiting for module observation")
}

func TestCollectEvictsStaleSources(t *testing.T) {
	c := New(config.ModuleReportSourceConfig{
		Enabled:           true,
		SocketPath:        filepath.Join(t.TempDir(), "module.sock"),
		MaxMessageBytes:   2048,
		DefaultStaleAfter: time.Second,
		ReportTTL:         50 * time.Millisecond,
	})
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start collector: %v", err)
	}
	defer func() {
		if err := c.Stop(context.Background()); err != nil {
			t.Fatalf("stop collector: %v", err)
		}
	}()

	c.mu.Lock()
	c.reports["fresh"] = reportState{sourceID: "fresh", receivedAt: time.Now()}
	c.reports["stale"] = reportState{sourceID: "stale", receivedAt: time.Now().Add(-time.Second)}
	c.mu.Unlock()

	if _, err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	c.mu.RLock()
	_, hasFresh := c.reports["fresh"]
	_, hasStale := c.reports["stale"]
	c.mu.RUnlock()
	if !hasFresh {
		t.Fatal("fresh source should be retained")
	}
	if hasStale {
		t.Fatal("stale source should be evicted")
	}
}

func TestCollectorAcceptsReportedSourceType(t *testing.T) {
	report, err := decodeReport([]byte(`{
		"source_id": "robot.ethercat",
		"source_type": "ethercat",
		"severity": "ok",
		"metrics": {
			"ethercat.working_counter": 120,
			"ethercat.working_counter_goal": 120
		}
	}`), time.Second)
	if err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.sourceType != "ethercat" {
		t.Fatalf("source_type = %q, want ethercat", report.sourceType)
	}
}
