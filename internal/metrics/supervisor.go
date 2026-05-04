package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"watchdog/internal/actions"
	"watchdog/internal/health"
)

type SupervisorStateView struct {
	UpdatedAt        time.Time
	OverallAction    actions.Kind
	ActiveComponents []SupervisorComponentView
}

type SupervisorComponentView struct {
	ComponentID string
	Action      actions.Kind
	Severity    health.Severity
	Latched     bool
}

type SupervisorHookView struct {
	Action            actions.Kind
	Executed          bool
	Suppressed        bool
	Errored           bool
	CommandConfigured bool
	Duration          time.Duration
}

type SupervisorCollector struct {
	mu sync.RWMutex

	state             SupervisorStateView
	requestTotals     map[string]uint64
	hookTotals        map[string]uint64
	lastHookDurations map[actions.Kind]float64

	stateUpdatedDesc     *prometheus.Desc
	overallActionDesc    *prometheus.Desc
	activeComponentsDesc *prometheus.Desc
	componentActionDesc  *prometheus.Desc
	requestTotalsDesc    *prometheus.Desc
	hookTotalsDesc       *prometheus.Desc
	lastHookDurationDesc *prometheus.Desc
}

func NewSupervisorCollector() *SupervisorCollector {
	return &SupervisorCollector{
		requestTotals:     make(map[string]uint64),
		hookTotals:        make(map[string]uint64),
		lastHookDurations: make(map[actions.Kind]float64),
		stateUpdatedDesc: prometheus.NewDesc(
			"watchdog_supervisor_state_updated_at_seconds",
			"Unix timestamp when the supervisor state was last updated.",
			nil, nil,
		),
		overallActionDesc: prometheus.NewDesc(
			"watchdog_supervisor_overall_action",
			"Current supervisor overall action as a one-hot gauge.",
			[]string{"action"}, nil,
		),
		activeComponentsDesc: prometheus.NewDesc(
			"watchdog_supervisor_active_components",
			"Number of active components currently held by the supervisor.",
			nil, nil,
		),
		componentActionDesc: prometheus.NewDesc(
			"watchdog_supervisor_component_action",
			"Current supervisor action and severity for each active component.",
			[]string{"component_id", "action", "severity", "latched"}, nil,
		),
		requestTotalsDesc: prometheus.NewDesc(
			"watchdog_supervisor_requests_total",
			"Total supervisor requests by event, requested action, and result.",
			[]string{"event", "requested_action", "result"}, nil,
		),
		hookTotalsDesc: prometheus.NewDesc(
			"watchdog_supervisor_hook_total",
			"Total supervisor hook outcomes by action and result.",
			[]string{"action", "result"}, nil,
		),
		lastHookDurationDesc: prometheus.NewDesc(
			"watchdog_supervisor_hook_last_duration_seconds",
			"Last observed hook duration for an action in seconds.",
			[]string{"action"}, nil,
		),
	}
}

func (c *SupervisorCollector) ObserveState(state SupervisorStateView) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = state
}

func (c *SupervisorCollector) ObserveRequest(request actions.Request, result string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := string(request.Event) + "|" + string(request.RequestedAction) + "|" + result
	c.requestTotals[key]++
}

func (c *SupervisorCollector) ObserveHook(hook SupervisorHookView) {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := "not_configured"
	switch {
	case hook.Errored:
		result = "error"
	case hook.Suppressed:
		result = "suppressed"
	case hook.Executed:
		result = "executed"
	case hook.CommandConfigured:
		result = "ready"
	}
	key := string(hook.Action) + "|" + result
	c.hookTotals[key]++
	if hook.Duration > 0 {
		c.lastHookDurations[hook.Action] = hook.Duration.Seconds()
	}
}

func (c *SupervisorCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.stateUpdatedDesc
	ch <- c.overallActionDesc
	ch <- c.activeComponentsDesc
	ch <- c.componentActionDesc
	ch <- c.requestTotalsDesc
	ch <- c.hookTotalsDesc
	ch <- c.lastHookDurationDesc
}

func (c *SupervisorCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.state.UpdatedAt.IsZero() {
		ch <- prometheus.MustNewConstMetric(c.stateUpdatedDesc, prometheus.GaugeValue, float64(c.state.UpdatedAt.Unix()))
	}
	ch <- prometheus.MustNewConstMetric(c.activeComponentsDesc, prometheus.GaugeValue, float64(len(c.state.ActiveComponents)))

	for _, action := range []actions.Kind{actions.ActionNotify, actions.ActionDegrade, actions.ActionSafeStop, actions.ActionResolve} {
		value := 0.0
		if c.state.OverallAction == action {
			value = 1
		}
		ch <- prometheus.MustNewConstMetric(c.overallActionDesc, prometheus.GaugeValue, value, string(action))
	}

	for _, component := range c.state.ActiveComponents {
		ch <- prometheus.MustNewConstMetric(
			c.componentActionDesc,
			prometheus.GaugeValue,
			1,
			component.ComponentID,
			string(component.Action),
			string(component.Severity),
			boolLabel(component.Latched),
		)
	}

	for key, total := range c.requestTotals {
		event, action, result := splitTriple(key)
		ch <- prometheus.MustNewConstMetric(c.requestTotalsDesc, prometheus.CounterValue, float64(total), event, action, result)
	}

	for key, total := range c.hookTotals {
		action, result := splitPair(key)
		ch <- prometheus.MustNewConstMetric(c.hookTotalsDesc, prometheus.CounterValue, float64(total), action, result)
	}

	for action, duration := range c.lastHookDurations {
		ch <- prometheus.MustNewConstMetric(c.lastHookDurationDesc, prometheus.GaugeValue, duration, string(action))
	}
}

func boolLabel(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func splitPair(value string) (string, string) {
	for i := 0; i < len(value); i++ {
		if value[i] == '|' {
			return value[:i], value[i+1:]
		}
	}
	return value, ""
}

func splitTriple(value string) (string, string, string) {
	first, rest := splitPair(value)
	second, third := splitPair(rest)
	return first, second, third
}
