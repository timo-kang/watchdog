package app

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"watchdog/internal/adapters"
	"watchdog/internal/config"
	"watchdog/internal/health"
	"watchdog/internal/incident"
	"watchdog/internal/retention"
	"watchdog/internal/rules"
)

func TestTickLinksRawLogsWhenIncidentIsWritten(t *testing.T) {
	rawLogs := &recordingRawLogLinker{indexPath: filepath.Join(t.TempDir(), "index.json")}
	daemon := New(
		log.New(io.Discard, "", 0),
		time.Second,
		[]adapters.Collector{
			fakeCollector{observations: []health.Observation{
				{
					SourceID:         "robot.control",
					SourceType:       "module",
					CollectedAt:      time.Now(),
					ReportedSeverity: health.SeverityFail,
					ReportedReason:   "control loop failed",
				},
			}},
		},
		rules.New(config.RulesConfig{}),
		incident.New(t.TempDir(), true),
		rawLogs,
		noopSink{},
		nil,
		t.TempDir(),
		time.Minute,
		retention.Policy{},
	)

	if err := daemon.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if rawLogs.incidentPath == "" {
		t.Fatal("raw log linker was not called")
	}
	if rawLogs.snapshot.Overall != health.SeverityFail {
		t.Fatalf("linked snapshot overall = %s, want fail", rawLogs.snapshot.Overall)
	}
}

type fakeCollector struct {
	observations []health.Observation
	err          error
}

func (f fakeCollector) Name() string {
	return "fake"
}

func (f fakeCollector) Collect(_ context.Context) ([]health.Observation, error) {
	return f.observations, f.err
}

type recordingRawLogLinker struct {
	indexPath    string
	incidentPath string
	snapshot     health.Snapshot
}

func (r *recordingRawLogLinker) LinkIncident(incidentPath string, snapshot health.Snapshot) (string, error) {
	r.incidentPath = incidentPath
	r.snapshot = snapshot
	return r.indexPath, nil
}

type noopSink struct{}

func (noopSink) HandleTransition(_ context.Context, _ *health.Snapshot, _ health.Snapshot, _ string) error {
	return nil
}
