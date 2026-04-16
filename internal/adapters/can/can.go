package can

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

type probeFunc func(ctx context.Context, backend string, iface config.CANInterfaceConfig) (InterfaceStatus, error)

type Collector struct {
	cfg   config.CANSourceConfig
	probe probeFunc
}

type InterfaceStatus struct {
	CollectedAt       time.Time
	LinkUp            bool
	Bitrate           int
	OnlineNodes       int
	OnlineNodesKnown  bool
	RXErrors          uint64
	TXErrors          uint64
	BusOffCount       uint64
	RestartCount      uint64
	State             string
	AdditionalInfo    map[string]string
	AdditionalMetrics map[string]float64
}

func New(cfg config.CANSourceConfig) *Collector {
	return &Collector{
		cfg:   cfg,
		probe: selectProbe(cfg.Backend),
	}
}

func (c *Collector) Name() string {
	return "can"
}

func (c *Collector) Collect(ctx context.Context) ([]health.Observation, error) {
	observations := make([]health.Observation, 0, len(c.cfg.Interfaces))
	for _, iface := range c.cfg.Interfaces {
		status, err := c.probe(ctx, c.cfg.Backend, iface)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", iface.Name, err)
		}
		collectedAt := status.CollectedAt
		if collectedAt.IsZero() {
			collectedAt = time.Now()
		}

		metrics := cloneMetrics(status.AdditionalMetrics)
		if metrics == nil {
			metrics = make(map[string]float64)
		}
		metrics["can.link_up"] = boolMetric(status.LinkUp)
		metrics["can.expected_bitrate"] = float64(iface.ExpectedBitrate)
		metrics["can.bitrate"] = float64(status.Bitrate)
		metrics["can.expected_nodes"] = float64(len(iface.ExpectedNodes))
		metrics["can.online_nodes"] = float64(status.OnlineNodes)
		metrics["can.online_nodes_known"] = boolMetric(status.OnlineNodesKnown)
		metrics["can.rx_errors"] = float64(status.RXErrors)
		metrics["can.tx_errors"] = float64(status.TXErrors)
		metrics["can.bus_off_count"] = float64(status.BusOffCount)
		metrics["can.restart_count"] = float64(status.RestartCount)
		if iface.RequireUp {
			metrics["can.require_up"] = 1
		}

		labels := cloneLabels(status.AdditionalInfo)
		if labels == nil {
			labels = make(map[string]string)
		}
		labels["backend"] = c.cfg.Backend
		labels["bus.kind"] = "can"
		labels["interface"] = iface.Name
		labels["bus_state"] = status.State

		observations = append(observations, health.Observation{
			SourceID:    iface.SourceID,
			SourceType:  "can",
			CollectedAt: collectedAt,
			Metrics:     metrics,
			Labels:      labels,
		})
	}
	return observations, nil
}

func selectProbe(backend string) probeFunc {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "socketcan":
		return probeSocketCAN
	case "command-json", "command_json", "cmdjson":
		return probeCommandJSON
	default:
		return unsupportedProbe
	}
}

func unsupportedProbe(_ context.Context, backend string, _ config.CANInterfaceConfig) (InterfaceStatus, error) {
	return InterfaceStatus{}, fmt.Errorf("CAN backend %q is not supported", backend)
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
