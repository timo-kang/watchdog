package network

import (
	"bufio"
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
	cfg        config.NetworkSourceConfig
	sysfsRoot  string
	netDevPath string
	last       map[string]ifaceSample
}

type ifaceSample struct {
	at        time.Time
	rxBytes   uint64
	txBytes   uint64
	rxPackets uint64
	txPackets uint64
	rxErrors  uint64
	txErrors  uint64
	rxDropped uint64
	txDropped uint64
}

func New(cfg config.NetworkSourceConfig) *Collector {
	return &Collector{
		cfg:        cfg,
		sysfsRoot:  "/sys/class/net",
		netDevPath: "/proc/net/dev",
		last:       make(map[string]ifaceSample),
	}
}

func (c *Collector) Name() string {
	return "network"
}

func (c *Collector) Collect(context.Context) ([]health.Observation, error) {
	counters, err := readNetDev(c.netDevPath)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	observations := make([]health.Observation, 0, len(c.cfg.Interfaces))
	for _, iface := range c.cfg.Interfaces {
		info, err := c.readInterface(iface)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", iface.Name, err)
		}
		sample, ok := counters[iface.Name]
		if !ok {
			return nil, fmt.Errorf("%s: missing /proc/net/dev counters", iface.Name)
		}

		metrics := map[string]float64{
			"network.link_up":        boolMetric(info.linkUp),
			"network.speed_mbps":     float64(info.speedMbps),
			"network.mtu":            float64(info.mtu),
			"network.rx_bytes":       float64(sample.rxBytes),
			"network.tx_bytes":       float64(sample.txBytes),
			"network.rx_packets":     float64(sample.rxPackets),
			"network.tx_packets":     float64(sample.txPackets),
			"network.rx_errors":      float64(sample.rxErrors),
			"network.tx_errors":      float64(sample.txErrors),
			"network.rx_dropped":     float64(sample.rxDropped),
			"network.tx_dropped":     float64(sample.txDropped),
			"network.min_speed_mbps": float64(iface.MinSpeedMbps),
		}
		if iface.RequireUp {
			metrics["network.require_up"] = 1
		}

		if previous, ok := c.last[iface.SourceID]; ok {
			elapsed := now.Sub(previous.at).Seconds()
			if elapsed > 0 {
				metrics["network.rx_bytes_per_s"] = float64(sample.rxBytes-previous.rxBytes) / elapsed
				metrics["network.tx_bytes_per_s"] = float64(sample.txBytes-previous.txBytes) / elapsed
				metrics["network.rx_packets_per_s"] = float64(sample.rxPackets-previous.rxPackets) / elapsed
				metrics["network.tx_packets_per_s"] = float64(sample.txPackets-previous.txPackets) / elapsed
			}
			metrics["network.rx_errors_delta"] = float64(saturatingSub(sample.rxErrors, previous.rxErrors))
			metrics["network.tx_errors_delta"] = float64(saturatingSub(sample.txErrors, previous.txErrors))
			metrics["network.rx_dropped_delta"] = float64(saturatingSub(sample.rxDropped, previous.rxDropped))
			metrics["network.tx_dropped_delta"] = float64(saturatingSub(sample.txDropped, previous.txDropped))
		}

		labels := map[string]string{
			"backend":    "sysfs",
			"bus.kind":   "network",
			"interface":  iface.Name,
			"oper_state": info.operState,
			"mac":        info.address,
		}

		observations = append(observations, health.Observation{
			SourceID:    iface.SourceID,
			SourceType:  "network",
			CollectedAt: now,
			Metrics:     metrics,
			Labels:      labels,
		})
		c.last[iface.SourceID] = ifaceSample{
			at:        now,
			rxBytes:   sample.rxBytes,
			txBytes:   sample.txBytes,
			rxPackets: sample.rxPackets,
			txPackets: sample.txPackets,
			rxErrors:  sample.rxErrors,
			txErrors:  sample.txErrors,
			rxDropped: sample.rxDropped,
			txDropped: sample.txDropped,
		}
	}

	return observations, nil
}

type interfaceInfo struct {
	linkUp    bool
	speedMbps int
	mtu       int
	operState string
	address   string
}

func (c *Collector) readInterface(iface config.NetworkInterfaceConfig) (interfaceInfo, error) {
	base := filepath.Join(c.sysfsRoot, iface.Name)
	operState := readString(filepath.Join(base, "operstate"))
	carrier, carrierOK := readInt(filepath.Join(base, "carrier"))
	speed, _ := readInt(filepath.Join(base, "speed"))
	mtu, _ := readInt(filepath.Join(base, "mtu"))
	address := readString(filepath.Join(base, "address"))
	linkUp := carrierOK && carrier > 0
	if !carrierOK {
		linkUp = operState == "up" || operState == "unknown"
	}
	if speed < 0 {
		speed = 0
	}
	return interfaceInfo{
		linkUp:    linkUp,
		speedMbps: speed,
		mtu:       mtu,
		operState: operState,
		address:   address,
	}, nil
}

func readNetDev(path string) (map[string]ifaceSample, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open /proc/net/dev: %w", err)
	}
	defer file.Close()

	out := make(map[string]ifaceSample)
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if lineNo <= 2 {
			continue
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("unexpected /proc/net/dev line %q", line)
		}
		fields := strings.Fields(rest)
		if len(fields) < 16 {
			return nil, fmt.Errorf("unexpected /proc/net/dev field count in %q", line)
		}
		out[strings.TrimSpace(name)] = ifaceSample{
			rxBytes:   parseUintOrZero(fields[0]),
			rxPackets: parseUintOrZero(fields[1]),
			rxErrors:  parseUintOrZero(fields[2]),
			rxDropped: parseUintOrZero(fields[3]),
			txBytes:   parseUintOrZero(fields[8]),
			txPackets: parseUintOrZero(fields[9]),
			txErrors:  parseUintOrZero(fields[10]),
			txDropped: parseUintOrZero(fields[11]),
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan /proc/net/dev: %w", err)
	}
	return out, nil
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readInt(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return value, true
}

func parseUintOrZero(value string) uint64 {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func saturatingSub(current, previous uint64) uint64 {
	if current < previous {
		return 0
	}
	return current - previous
}

func boolMetric(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
