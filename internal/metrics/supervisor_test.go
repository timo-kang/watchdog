package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"watchdog/internal/actions"
	"watchdog/internal/health"
)

func TestSupervisorCollectorExportsStateAndCounters(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := NewSupervisorCollector()
	if err := registry.Register(collector); err != nil {
		t.Fatalf("register collector: %v", err)
	}

	collector.ObserveState(SupervisorStateView{
		UpdatedAt:     time.Unix(1714464000, 0).UTC(),
		OverallAction: actions.ActionDegrade,
		ActiveComponents: []SupervisorComponentView{{
			ComponentID: "planner",
			Action:      actions.ActionDegrade,
			Severity:    health.SeverityStale,
			Latched:     true,
		}},
	})
	collector.ObserveRequest(actions.Request{
		Event:           actions.EventTransition,
		RequestedAction: actions.ActionDegrade,
	}, "applied")
	collector.ObserveHook(SupervisorHookView{
		Action:            actions.ActionDegrade,
		Executed:          true,
		CommandConfigured: true,
		Duration:          150 * time.Millisecond,
	})

	if got := testutil.CollectAndCount(collector); got == 0 {
		t.Fatal("expected metrics to be collected")
	}

	expected := `
# HELP watchdog_supervisor_active_components Number of active components currently held by the supervisor.
# TYPE watchdog_supervisor_active_components gauge
watchdog_supervisor_active_components 1
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "watchdog_supervisor_active_components"); err != nil {
		t.Fatalf("compare active components metric: %v", err)
	}
}
