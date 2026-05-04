package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"watchdog/internal/health"
)

func TestWatchdogCollectorExportsSnapshotAndCollectorStats(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := NewWatchdogCollector()
	if err := registry.Register(collector); err != nil {
		t.Fatalf("register collector: %v", err)
	}

	collector.ObserveCollectorResult("time_sync", 25*time.Millisecond, nil)
	collector.ObserveCollectorResult("network", 10*time.Millisecond, assertErr{})
	collector.ObserveIncidentWrite(true, nil)
	collector.ObserveActionSink(assertErr{})
	collector.ObserveSnapshot(health.Snapshot{
		CollectedAt: time.Unix(1714464000, 0).UTC(),
		Overall:     health.SeverityWarn,
		Statuses: []health.Status{{
			SourceID:   "system-clock",
			SourceType: "time_sync",
			Severity:   health.SeverityWarn,
			ObservedAt: time.Unix(1714464000, 0).UTC(),
			Metrics: map[string]float64{
				"time.unsynchronized_for_s": 12,
			},
		}},
		Components: []health.ComponentStatus{{
			ComponentID: "system-clock",
			Severity:    health.SeverityWarn,
		}},
		Errors: []string{"network: timeout"},
	})

	if got := testutil.CollectAndCount(collector); got == 0 {
		t.Fatal("expected metrics to be collected")
	}

	expected := `
# HELP watchdog_snapshot_overall_code Overall watchdog severity as a numeric code: ok=0, warn=1, fail=2, stale=3.
# TYPE watchdog_snapshot_overall_code gauge
watchdog_snapshot_overall_code 1
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "watchdog_snapshot_overall_code"); err != nil {
		t.Fatalf("compare overall metric: %v", err)
	}
}

type assertErr struct{}

func (assertErr) Error() string { return "boom" }
