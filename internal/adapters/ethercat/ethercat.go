package ethercat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"watchdog/internal/adapters"
	"watchdog/internal/config"
	"watchdog/internal/health"
)

var _ adapters.Collector = (*Collector)(nil)

type probeFunc func(ctx context.Context, backend string, master config.EtherCATMasterConfig) (MasterStatus, error)

type Collector struct {
	cfg   config.EtherCATSourceConfig
	probe probeFunc
}

type MasterStatus struct {
	CollectedAt            time.Time
	LinkKnown              bool
	LinkUp                 bool
	MasterState            string
	SlavesSeen             int
	SlaveErrors            int
	WorkingCounter         int
	WorkingCounterExpected int
	AdditionalInfo         map[string]string
	AdditionalMetrics      map[string]float64
}

func New(cfg config.EtherCATSourceConfig) *Collector {
	return &Collector{
		cfg:   cfg,
		probe: selectProbe(cfg.Backend),
	}
}

func (c *Collector) Name() string {
	return "ethercat"
}

func (c *Collector) Collect(ctx context.Context) ([]health.Observation, error) {
	observations := make([]health.Observation, 0, len(c.cfg.Masters))
	for _, master := range c.cfg.Masters {
		status, err := c.probe(ctx, c.cfg.Backend, master)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", master.Name, err)
		}
		collectedAt := status.CollectedAt
		if collectedAt.IsZero() {
			collectedAt = time.Now()
		}

		metrics := cloneMetrics(status.AdditionalMetrics)
		if metrics == nil {
			metrics = make(map[string]float64)
		}
		metrics["ethercat.link_known"] = boolMetric(status.LinkKnown)
		metrics["ethercat.link_up"] = boolMetric(status.LinkUp)
		metrics["ethercat.slaves_seen"] = float64(status.SlavesSeen)
		metrics["ethercat.slave_errors"] = float64(status.SlaveErrors)
		metrics["ethercat.expected_slaves"] = float64(master.ExpectedSlaves)
		metrics["ethercat.working_counter"] = float64(status.WorkingCounter)
		metrics["ethercat.working_counter_goal"] = float64(status.WorkingCounterExpected)
		if master.RequireLink {
			metrics["ethercat.require_link"] = 1
		}

		labels := cloneLabels(status.AdditionalInfo)
		if labels == nil {
			labels = make(map[string]string)
		}
		labels["backend"] = c.cfg.Backend
		labels["bus.kind"] = "ethercat"
		labels["master"] = master.Name
		labels["master_state"] = strings.ToLower(status.MasterState)
		labels["expected_state"] = strings.ToLower(master.ExpectedState)

		observations = append(observations, health.Observation{
			SourceID:    master.SourceID,
			SourceType:  "ethercat",
			CollectedAt: collectedAt,
			Metrics:     metrics,
			Labels:      labels,
		})
	}
	return observations, nil
}

func selectProbe(backend string) probeFunc {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "igh":
		return probeIgHCLI
	case "command-json", "command_json", "cmdjson":
		return probeCommandJSON
	case "soem":
		return probeSOEM
	default:
		return unsupportedProbe
	}
}

func unsupportedProbe(_ context.Context, backend string, _ config.EtherCATMasterConfig) (MasterStatus, error) {
	return MasterStatus{}, fmt.Errorf("EtherCAT backend %q is not supported", backend)
}

func boolMetric(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func cloneMetrics(values map[string]float64) map[string]float64 {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]float64, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func cloneLabels(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
