package systemd

import (
	"context"
	"testing"
	"time"

	"watchdog/internal/config"
)

func TestCollectParsesUnitState(t *testing.T) {
	collector := New(config.SystemdSourceConfig{
		Enabled: true,
		Units: []config.SystemdUnitConfig{
			{
				Name:           "planner.service",
				SourceID:       "planner",
				RequireMainPID: true,
			},
		},
	})
	collector.show = func(context.Context, string) ([]byte, error) {
		return []byte(
			"Id=planner.service\n" +
				"LoadState=loaded\n" +
				"ActiveState=active\n" +
				"SubState=running\n" +
				"UnitFileState=enabled\n" +
				"ExecMainPID=4242\n" +
				"NRestarts=2\n" +
				"Result=success\n" +
				"InvocationID=abc123\n",
		), nil
	}

	observations, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("len(observations) = %d, want 1", len(observations))
	}

	observation := observations[0]
	if observation.SourceID != "planner" {
		t.Fatalf("source_id = %q, want planner", observation.SourceID)
	}
	if observation.SourceType != "process" {
		t.Fatalf("source_type = %q, want process", observation.SourceType)
	}
	if got := observation.Metrics["process.main_pid"]; got != 4242 {
		t.Fatalf("process.main_pid = %f, want 4242", got)
	}
	if got := observation.Metrics["process.restarts"]; got != 2 {
		t.Fatalf("process.restarts = %f, want 2", got)
	}
	if got := observation.Labels["active_state"]; got != "active" {
		t.Fatalf("active_state = %q, want active", got)
	}
	if got := observation.Labels["unit"]; got != "planner.service" {
		t.Fatalf("unit = %q, want planner.service", got)
	}
	if observation.CollectedAt.Before(time.Now().Add(-2 * time.Second)) {
		t.Fatal("collected_at is unexpectedly old")
	}
}

func TestParseShowOutputRejectsMissingState(t *testing.T) {
	_, err := parseShowOutput("planner.service", []byte("Id=planner.service\nLoadState=loaded\n"))
	if err == nil {
		t.Fatal("expected error for missing ActiveState")
	}
}
