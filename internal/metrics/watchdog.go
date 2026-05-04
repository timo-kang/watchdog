package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"watchdog/internal/health"
)

type WatchdogCollector struct {
	mu sync.RWMutex

	snapshot             *health.Snapshot
	collectorStats       map[string]watchdogCollectorStats
	incidentTotals       map[string]uint64
	actionSinkErrorTotal uint64

	snapshotTimestampDesc  *prometheus.Desc
	snapshotOverallDesc    *prometheus.Desc
	snapshotErrorsDesc     *prometheus.Desc
	snapshotStatusesDesc   *prometheus.Desc
	snapshotComponentsDesc *prometheus.Desc
	componentSeverityDesc  *prometheus.Desc
	statusSeverityDesc     *prometheus.Desc
	statusObservedAtDesc   *prometheus.Desc
	statusMetricDesc       *prometheus.Desc
	collectorDurationDesc  *prometheus.Desc
	collectorSuccessDesc   *prometheus.Desc
	collectorErrorDesc     *prometheus.Desc
	collectorHealthyDesc   *prometheus.Desc
	incidentTotalsDesc     *prometheus.Desc
	actionSinkErrorsDesc   *prometheus.Desc
}

type watchdogCollectorStats struct {
	LastDurationSeconds float64
	SuccessTotal        uint64
	ErrorTotal          uint64
	Healthy             bool
}

func NewWatchdogCollector() *WatchdogCollector {
	return &WatchdogCollector{
		collectorStats: make(map[string]watchdogCollectorStats),
		incidentTotals: map[string]uint64{
			"written": 0,
			"skipped": 0,
			"error":   0,
		},
		snapshotTimestampDesc: prometheus.NewDesc(
			"watchdog_snapshot_timestamp_seconds",
			"Unix timestamp of the most recent watchdog snapshot.",
			nil, nil,
		),
		snapshotOverallDesc: prometheus.NewDesc(
			"watchdog_snapshot_overall_code",
			"Overall watchdog severity as a numeric code: ok=0, warn=1, fail=2, stale=3.",
			nil, nil,
		),
		snapshotErrorsDesc: prometheus.NewDesc(
			"watchdog_snapshot_errors",
			"Number of collection/runtime errors attached to the most recent snapshot.",
			nil, nil,
		),
		snapshotStatusesDesc: prometheus.NewDesc(
			"watchdog_snapshot_statuses",
			"Number of raw source statuses in the most recent snapshot.",
			nil, nil,
		),
		snapshotComponentsDesc: prometheus.NewDesc(
			"watchdog_snapshot_components",
			"Number of derived component states in the most recent snapshot.",
			nil, nil,
		),
		componentSeverityDesc: prometheus.NewDesc(
			"watchdog_component_severity_code",
			"Component severity as a numeric code: ok=0, warn=1, fail=2, stale=3.",
			[]string{"component_id"}, nil,
		),
		statusSeverityDesc: prometheus.NewDesc(
			"watchdog_status_severity_code",
			"Source status severity as a numeric code: ok=0, warn=1, fail=2, stale=3.",
			[]string{"source_id", "source_type"}, nil,
		),
		statusObservedAtDesc: prometheus.NewDesc(
			"watchdog_status_observed_at_seconds",
			"Unix timestamp when the source status was observed.",
			[]string{"source_id", "source_type"}, nil,
		),
		statusMetricDesc: prometheus.NewDesc(
			"watchdog_status_metric_value",
			"Numeric status metric exported by a source.",
			[]string{"source_id", "source_type", "metric_name"}, nil,
		),
		collectorDurationDesc: prometheus.NewDesc(
			"watchdog_collector_last_duration_seconds",
			"Last collection duration for a collector in seconds.",
			[]string{"collector"}, nil,
		),
		collectorSuccessDesc: prometheus.NewDesc(
			"watchdog_collector_success_total",
			"Total successful collection calls per collector.",
			[]string{"collector"}, nil,
		),
		collectorErrorDesc: prometheus.NewDesc(
			"watchdog_collector_error_total",
			"Total failed collection calls per collector.",
			[]string{"collector"}, nil,
		),
		collectorHealthyDesc: prometheus.NewDesc(
			"watchdog_collector_healthy",
			"Whether the collector succeeded on its most recent run: healthy=1, unhealthy=0.",
			[]string{"collector"}, nil,
		),
		incidentTotalsDesc: prometheus.NewDesc(
			"watchdog_incident_writes_total",
			"Total incident writer outcomes by result.",
			[]string{"result"}, nil,
		),
		actionSinkErrorsDesc: prometheus.NewDesc(
			"watchdog_action_sink_error_total",
			"Total action sink errors observed by watchdog.",
			nil, nil,
		),
	}
}

func (c *WatchdogCollector) ObserveCollectorResult(name string, duration time.Duration, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats := c.collectorStats[name]
	stats.LastDurationSeconds = duration.Seconds()
	if err != nil {
		stats.ErrorTotal++
		stats.Healthy = false
	} else {
		stats.SuccessTotal++
		stats.Healthy = true
	}
	c.collectorStats[name] = stats
}

func (c *WatchdogCollector) ObserveSnapshot(snapshot health.Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cloned := snapshot
	if snapshot.Statuses != nil {
		cloned.Statuses = append([]health.Status(nil), snapshot.Statuses...)
	}
	if snapshot.Components != nil {
		cloned.Components = append([]health.ComponentStatus(nil), snapshot.Components...)
	}
	if snapshot.Errors != nil {
		cloned.Errors = append([]string(nil), snapshot.Errors...)
	}
	c.snapshot = &cloned
}

func (c *WatchdogCollector) ObserveIncidentWrite(written bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch {
	case err != nil:
		c.incidentTotals["error"]++
	case written:
		c.incidentTotals["written"]++
	default:
		c.incidentTotals["skipped"]++
	}
}

func (c *WatchdogCollector) ObserveActionSink(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.actionSinkErrorTotal++
}

func (c *WatchdogCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.snapshotTimestampDesc
	ch <- c.snapshotOverallDesc
	ch <- c.snapshotErrorsDesc
	ch <- c.snapshotStatusesDesc
	ch <- c.snapshotComponentsDesc
	ch <- c.componentSeverityDesc
	ch <- c.statusSeverityDesc
	ch <- c.statusObservedAtDesc
	ch <- c.statusMetricDesc
	ch <- c.collectorDurationDesc
	ch <- c.collectorSuccessDesc
	ch <- c.collectorErrorDesc
	ch <- c.collectorHealthyDesc
	ch <- c.incidentTotalsDesc
	ch <- c.actionSinkErrorsDesc
}

func (c *WatchdogCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.snapshot != nil {
		ch <- prometheus.MustNewConstMetric(c.snapshotTimestampDesc, prometheus.GaugeValue, float64(c.snapshot.CollectedAt.Unix()))
		ch <- prometheus.MustNewConstMetric(c.snapshotOverallDesc, prometheus.GaugeValue, severityCode(c.snapshot.Overall))
		ch <- prometheus.MustNewConstMetric(c.snapshotErrorsDesc, prometheus.GaugeValue, float64(len(c.snapshot.Errors)))
		ch <- prometheus.MustNewConstMetric(c.snapshotStatusesDesc, prometheus.GaugeValue, float64(len(c.snapshot.Statuses)))
		ch <- prometheus.MustNewConstMetric(c.snapshotComponentsDesc, prometheus.GaugeValue, float64(len(c.snapshot.Components)))

		for _, component := range c.snapshot.Components {
			ch <- prometheus.MustNewConstMetric(
				c.componentSeverityDesc,
				prometheus.GaugeValue,
				severityCode(component.Severity),
				component.ComponentID,
			)
		}
		for _, status := range c.snapshot.Statuses {
			ch <- prometheus.MustNewConstMetric(
				c.statusSeverityDesc,
				prometheus.GaugeValue,
				severityCode(status.Severity),
				status.SourceID,
				status.SourceType,
			)
			ch <- prometheus.MustNewConstMetric(
				c.statusObservedAtDesc,
				prometheus.GaugeValue,
				float64(status.ObservedAt.Unix()),
				status.SourceID,
				status.SourceType,
			)
			for metricName, value := range status.Metrics {
				ch <- prometheus.MustNewConstMetric(
					c.statusMetricDesc,
					prometheus.GaugeValue,
					value,
					status.SourceID,
					status.SourceType,
					metricName,
				)
			}
		}
	}

	for name, stats := range c.collectorStats {
		ch <- prometheus.MustNewConstMetric(c.collectorDurationDesc, prometheus.GaugeValue, stats.LastDurationSeconds, name)
		ch <- prometheus.MustNewConstMetric(c.collectorSuccessDesc, prometheus.CounterValue, float64(stats.SuccessTotal), name)
		ch <- prometheus.MustNewConstMetric(c.collectorErrorDesc, prometheus.CounterValue, float64(stats.ErrorTotal), name)
		ch <- prometheus.MustNewConstMetric(c.collectorHealthyDesc, prometheus.GaugeValue, boolToGauge(stats.Healthy), name)
	}

	for result, total := range c.incidentTotals {
		ch <- prometheus.MustNewConstMetric(c.incidentTotalsDesc, prometheus.CounterValue, float64(total), result)
	}
	ch <- prometheus.MustNewConstMetric(c.actionSinkErrorsDesc, prometheus.CounterValue, float64(c.actionSinkErrorTotal))
}

func severityCode(severity health.Severity) float64 {
	switch severity {
	case health.SeverityWarn:
		return 1
	case health.SeverityFail:
		return 2
	case health.SeverityStale:
		return 3
	default:
		return 0
	}
}

func boolToGauge(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
