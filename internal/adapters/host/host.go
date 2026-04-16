package host

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"watchdog/internal/config"
	"watchdog/internal/health"
)

type Adapter struct {
	cfg config.HostSourceConfig
}

type tempReading struct {
	Label  string
	Path   string
	Bucket string
	ValueC float64
}

func New(cfg config.HostSourceConfig) *Adapter {
	return &Adapter{cfg: cfg}
}

func (a *Adapter) Name() string {
	return "host"
}

func (a *Adapter) Collect(context.Context) ([]health.Observation, error) {
	load1, err := readLoad1()
	if err != nil {
		return nil, fmt.Errorf("read loadavg: %w", err)
	}

	memTotalMB, memAvailMB, err := readMemInfo()
	if err != nil {
		return nil, fmt.Errorf("read meminfo: %w", err)
	}

	readings, err := discoverTemperatures()
	if err != nil {
		return nil, fmt.Errorf("discover temperatures: %w", err)
	}

	maxTemp := hottest(readings, func(r tempReading) bool { return true })
	maxCPUTemp := hottest(readings, func(r tempReading) bool { return r.Bucket == "cpu" })
	if maxCPUTemp == nil {
		maxCPUTemp = maxTemp
	}

	metrics := map[string]float64{
		"load.1m":          load1,
		"cpu.count":        float64(runtime.NumCPU()),
		"mem.total_mb":     memTotalMB,
		"mem.available_mb": memAvailMB,
	}
	labels := map[string]string{}

	if maxTemp != nil {
		metrics["temp.max_c"] = maxTemp.ValueC
		labels["temp.max_label"] = maxTemp.Label
		labels["temp.max_path"] = maxTemp.Path
		labels["temp.max_bucket"] = maxTemp.Bucket
	}
	if maxCPUTemp != nil {
		metrics["temp.cpu_max_c"] = maxCPUTemp.ValueC
		labels["temp.cpu_label"] = maxCPUTemp.Label
		labels["temp.cpu_path"] = maxCPUTemp.Path
	}

	observation := health.Observation{
		SourceID:    "host.local",
		SourceType:  "host",
		CollectedAt: time.Now(),
		Metrics:     metrics,
		Labels:      labels,
	}
	return []health.Observation{observation}, nil
}

func readLoad1() (float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, fmt.Errorf("unexpected /proc/loadavg format")
	}
	return strconv.ParseFloat(fields[0], 64)
}

func readMemInfo() (float64, float64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	var totalKB float64
	var availKB float64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			totalKB = parseMemInfoLine(line)
		}
		if strings.HasPrefix(line, "MemAvailable:") {
			availKB = parseMemInfoLine(line)
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	return totalKB / 1024, availKB / 1024, nil
}

func parseMemInfoLine(line string) float64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	value, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0
	}
	return value
}

func discoverTemperatures() ([]tempReading, error) {
	var readings []tempReading

	for _, zone := range mustGlob("/sys/class/thermal/thermal_zone*") {
		path := filepath.Join(zone, "temp")
		valueC, ok := readMilliC(path)
		if !ok {
			continue
		}
		label := readString(filepath.Join(zone, "type"))
		if label == "" {
			label = filepath.Base(zone)
		}
		readings = append(readings, tempReading{
			Label:  label,
			Path:   path,
			Bucket: detectSensorBucket(label),
			ValueC: valueC,
		})
	}

	for _, hwmon := range mustGlob("/sys/class/hwmon/hwmon*") {
		name := readString(filepath.Join(hwmon, "name"))
		for _, path := range mustGlob(filepath.Join(hwmon, "temp*_input")) {
			valueC, ok := readMilliC(path)
			if !ok {
				continue
			}
			index := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "temp"), "_input")
			label := readString(filepath.Join(hwmon, fmt.Sprintf("temp%s_label", index)))
			if label == "" {
				label = fmt.Sprintf("%s_temp%s", name, index)
			}
			readings = append(readings, tempReading{
				Label:  label,
				Path:   path,
				Bucket: detectSensorBucket(label),
				ValueC: valueC,
			})
		}
	}

	return readings, nil
}

func mustGlob(pattern string) []string {
	values, _ := filepath.Glob(pattern)
	return values
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readMilliC(path string) (float64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	raw, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return 0, false
	}
	return raw / 1000.0, true
}

func hottest(readings []tempReading, keep func(tempReading) bool) *tempReading {
	var best *tempReading
	for i := range readings {
		if !keep(readings[i]) {
			continue
		}
		if best == nil || readings[i].ValueC > best.ValueC {
			best = &readings[i]
		}
	}
	return best
}

func detectSensorBucket(label string) string {
	lower := strings.ToLower(label)
	switch {
	case matchesAny(lower,
		"cpu", "package", "package id", "x86_pkg_temp", "coretemp", "p-core", "e-core", "tdie", "tctl", "tcpu"):
		return "cpu"
	case strings.HasPrefix(lower, "core "):
		return "cpu"
	case matchesAny(lower, "soc", "gpu", "mem", "vdd", "tj", "ape", "cv", "thermal-fan-est"):
		return "soc"
	case matchesAny(lower, "board", "ambient", "acpi", "pch", "system"):
		return "board"
	default:
		return "other"
	}
}

func matchesAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
