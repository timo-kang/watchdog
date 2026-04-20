package power

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"watchdog/internal/adapters"
	"watchdog/internal/config"
	"watchdog/internal/health"
)

var _ adapters.Collector = (*Collector)(nil)

type Collector struct {
	cfg       config.PowerSourceConfig
	sysfsRoot string
}

func New(cfg config.PowerSourceConfig) *Collector {
	return &Collector{
		cfg:       cfg,
		sysfsRoot: "/sys/class/power_supply",
	}
}

func (c *Collector) Name() string {
	return "power"
}

func (c *Collector) Collect(context.Context) ([]health.Observation, error) {
	now := time.Now()
	observations := make([]health.Observation, 0, len(c.cfg.Supplies))
	for _, supply := range c.cfg.Supplies {
		base := filepath.Join(c.sysfsRoot, supply.Name)
		info, err := c.readSupply(base)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", supply.Name, err)
		}

		metrics := map[string]float64{
			"power.present": boolMetric(info.present),
			"power.online":  boolMetric(info.online),
		}
		if supply.RequirePresent {
			metrics["power.require_present"] = 1
		}
		if supply.RequireOnline {
			metrics["power.require_online"] = 1
		}
		if info.capacityPct > 0 {
			metrics["power.capacity_pct"] = info.capacityPct
		}
		if info.voltageV > 0 {
			metrics["power.voltage_v"] = info.voltageV
		}
		if info.currentA > 0 {
			metrics["power.current_a"] = info.currentA
		}
		if info.powerW > 0 {
			metrics["power.power_w"] = info.powerW
		}
		if info.tempC > 0 {
			metrics["power.temp_c"] = info.tempC
		}
		if info.cycleCount > 0 {
			metrics["power.cycle_count"] = float64(info.cycleCount)
		}

		labels := map[string]string{
			"backend":      "sysfs",
			"bus.kind":     "power",
			"supply":       supply.Name,
			"type":         info.typ,
			"status":       info.status,
			"health":       info.health,
			"technology":   info.technology,
			"model_name":   info.modelName,
			"manufacturer": info.manufacturer,
			"serial":       info.serial,
		}

		observations = append(observations, health.Observation{
			SourceID:    supply.SourceID,
			SourceType:  "power",
			CollectedAt: now,
			Metrics:     metrics,
			Labels:      labels,
		})
	}
	return observations, nil
}

type supplyInfo struct {
	present      bool
	online       bool
	capacityPct  float64
	voltageV     float64
	currentA     float64
	powerW       float64
	tempC        float64
	cycleCount   uint64
	typ          string
	status       string
	health       string
	technology   string
	modelName    string
	manufacturer string
	serial       string
}

func (c *Collector) readSupply(base string) (supplyInfo, error) {
	if _, err := os.Stat(base); err != nil {
		return supplyInfo{}, err
	}
	return supplyInfo{
		present:      readBoolFile(filepath.Join(base, "present"), true),
		online:       readBoolFile(filepath.Join(base, "online"), false),
		capacityPct:  readScaledFloat(filepath.Join(base, "capacity"), 1),
		voltageV:     readScaledFloat(filepath.Join(base, "voltage_now"), 1_000_000),
		currentA:     readScaledFloat(filepath.Join(base, "current_now"), 1_000_000),
		powerW:       readScaledFloat(filepath.Join(base, "power_now"), 1_000_000),
		tempC:        readScaledFloat(filepath.Join(base, "temp"), 10),
		cycleCount:   readUint(filepath.Join(base, "cycle_count")),
		typ:          readString(filepath.Join(base, "type")),
		status:       strings.ToLower(readString(filepath.Join(base, "status"))),
		health:       strings.ToLower(readString(filepath.Join(base, "health"))),
		technology:   readString(filepath.Join(base, "technology")),
		modelName:    readString(filepath.Join(base, "model_name")),
		manufacturer: readString(filepath.Join(base, "manufacturer")),
		serial:       readString(filepath.Join(base, "serial_number")),
	}, nil
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readBoolFile(path string, defaultValue bool) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultValue
	}
	value := strings.TrimSpace(string(data))
	return value == "1" || strings.EqualFold(value, "yes")
}

func readScaledFloat(path string, scale float64) float64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return 0
	}
	if scale <= 0 {
		return value
	}
	return value / scale
}

func readUint(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func boolMetric(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
