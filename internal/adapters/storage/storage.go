package storage

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"watchdog/internal/adapters"
	"watchdog/internal/config"
	"watchdog/internal/health"
)

var _ adapters.Collector = (*Collector)(nil)

type Collector struct {
	cfg           config.StorageSourceConfig
	mountInfoPath string
	diskStatsPath string
	last          map[string]diskSample
}

type diskSample struct {
	at           time.Time
	readIOs      uint64
	writeIOs     uint64
	ioTimeMillis uint64
}

type mountInfo struct {
	path     string
	source   string
	readOnly bool
}

type diskStats struct {
	readIOs      uint64
	writeIOs     uint64
	ioInProgress uint64
	ioTimeMillis uint64
}

func New(cfg config.StorageSourceConfig) *Collector {
	return &Collector{
		cfg:           cfg,
		mountInfoPath: "/proc/self/mountinfo",
		diskStatsPath: "/proc/diskstats",
		last:          make(map[string]diskSample),
	}
}

func (c *Collector) Name() string {
	return "storage"
}

func (c *Collector) Collect(context.Context) ([]health.Observation, error) {
	mounts, err := readMountInfo(c.mountInfoPath)
	if err != nil {
		return nil, err
	}
	disks, err := readDiskStats(c.diskStatsPath)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	observations := make([]health.Observation, 0, len(c.cfg.Mounts))
	for _, mount := range c.cfg.Mounts {
		stats, err := statFS(mount.Path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", mount.Path, err)
		}
		info, ok := mounts[mount.Path]
		if !ok {
			info = mountInfo{path: mount.Path}
		}

		metrics := map[string]float64{
			"storage.total_bytes":  float64(stats.totalBytes),
			"storage.avail_bytes":  float64(stats.availBytes),
			"storage.used_bytes":   float64(stats.usedBytes),
			"storage.used_percent": stats.usedPercent,
			"storage.readonly":     boolMetric(info.readOnly),
			"storage.total_inodes": float64(stats.totalInodes),
			"storage.free_inodes":  float64(stats.freeInodes),
		}
		if mount.RequireWritable {
			metrics["storage.require_writable"] = 1
		}

		deviceName := mount.Device
		if deviceName == "" {
			deviceName = info.source
		}
		deviceKey := normalizeDevice(deviceName)
		if deviceKey != "" {
			if disk, ok := disks[deviceKey]; ok {
				metrics["storage.read_ios"] = float64(disk.readIOs)
				metrics["storage.write_ios"] = float64(disk.writeIOs)
				metrics["storage.io_in_progress"] = float64(disk.ioInProgress)
				metrics["storage.io_time_ms"] = float64(disk.ioTimeMillis)
				if previous, ok := c.last[mount.SourceID]; ok {
					elapsed := now.Sub(previous.at).Seconds()
					if elapsed > 0 {
						metrics["storage.read_iops"] = float64(saturatingSub(disk.readIOs, previous.readIOs)) / elapsed
						metrics["storage.write_iops"] = float64(saturatingSub(disk.writeIOs, previous.writeIOs)) / elapsed
					}
					elapsedMillis := now.Sub(previous.at).Milliseconds()
					if elapsedMillis > 0 {
						metrics["storage.busy_percent"] = float64(saturatingSub(disk.ioTimeMillis, previous.ioTimeMillis)) / float64(elapsedMillis) * 100
					}
				}
				c.last[mount.SourceID] = diskSample{
					at:           now,
					readIOs:      disk.readIOs,
					writeIOs:     disk.writeIOs,
					ioTimeMillis: disk.ioTimeMillis,
				}
			}
		}

		labels := map[string]string{
			"bus.kind":     "storage",
			"mount_path":   mount.Path,
			"device":       deviceName,
			"filesystem":   stats.fsType,
			"mount_source": info.source,
		}

		observations = append(observations, health.Observation{
			SourceID:    mount.SourceID,
			SourceType:  "storage",
			CollectedAt: now,
			Metrics:     metrics,
			Labels:      labels,
		})
	}

	return observations, nil
}

type fsStats struct {
	totalBytes  uint64
	availBytes  uint64
	usedBytes   uint64
	usedPercent float64
	totalInodes uint64
	freeInodes  uint64
	fsType      string
}

func statFS(path string) (fsStats, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return fsStats{}, err
	}
	total := stat.Blocks * uint64(stat.Bsize)
	avail := stat.Bavail * uint64(stat.Bsize)
	used := total - avail
	usedPct := 0.0
	if total > 0 {
		usedPct = float64(used) / float64(total) * 100
	}
	return fsStats{
		totalBytes:  total,
		availBytes:  avail,
		usedBytes:   used,
		usedPercent: usedPct,
		totalInodes: stat.Files,
		freeInodes:  stat.Ffree,
		fsType:      fmt.Sprintf("%x", stat.Type),
	}, nil
}

func readMountInfo(path string) (map[string]mountInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open mountinfo: %w", err)
	}
	defer file.Close()

	out := make(map[string]mountInfo)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		left, right, ok := strings.Cut(line, " - ")
		if !ok {
			return nil, fmt.Errorf("unexpected mountinfo line %q", line)
		}
		leftFields := strings.Fields(left)
		rightFields := strings.Fields(right)
		if len(leftFields) < 6 || len(rightFields) < 2 {
			return nil, fmt.Errorf("unexpected mountinfo fields %q", line)
		}
		path := leftFields[4]
		out[path] = mountInfo{
			path:     path,
			source:   rightFields[1],
			readOnly: strings.Contains(","+leftFields[5]+",", ",ro,"),
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan mountinfo: %w", err)
	}
	return out, nil
}

func readDiskStats(path string) (map[string]diskStats, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open diskstats: %w", err)
	}
	defer file.Close()

	out := make(map[string]diskStats)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 14 {
			continue
		}
		name := fields[2]
		out[name] = diskStats{
			readIOs:      parseUintOrZero(fields[3]),
			writeIOs:     parseUintOrZero(fields[7]),
			ioInProgress: parseUintOrZero(fields[11]),
			ioTimeMillis: parseUintOrZero(fields[12]),
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan diskstats: %w", err)
	}
	return out, nil
}

func normalizeDevice(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "/dev/") {
		return filepath.Base(value)
	}
	return value
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
