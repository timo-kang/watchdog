package timesync

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"watchdog/internal/adapters"
	"watchdog/internal/config"
	"watchdog/internal/health"
)

var _ adapters.Collector = (*Collector)(nil)

type Collector struct {
	cfg  config.TimeSyncSourceConfig
	show func(context.Context) ([]byte, error)
}

type syncState struct {
	timezone        string
	localRTC        bool
	canNTP          bool
	ntpEnabled      bool
	ntpSynchronized bool
	systemTime      time.Time
	rtcTime         time.Time
}

func New(cfg config.TimeSyncSourceConfig) *Collector {
	return &Collector{
		cfg:  cfg,
		show: runTimedatectlShow,
	}
}

func (c *Collector) Name() string {
	return "time_sync"
}

func (c *Collector) Collect(ctx context.Context) ([]health.Observation, error) {
	raw, err := c.show(ctx)
	if err != nil {
		return nil, err
	}
	state, err := parseTimedatectlShow(raw)
	if err != nil {
		return nil, err
	}

	metrics := map[string]float64{
		"time.can_ntp":           boolMetric(state.canNTP),
		"time.ntp_enabled":       boolMetric(state.ntpEnabled),
		"time.ntp_synchronized":  boolMetric(state.ntpSynchronized),
		"time.local_rtc":         boolMetric(state.localRTC),
		"time.require_sync":      boolMetric(c.cfg.RequireSynchronized),
		"time.warn_on_local_rtc": boolMetric(c.cfg.WarnOnLocalRTC),
	}
	if !state.systemTime.IsZero() && !state.rtcTime.IsZero() {
		delta := state.systemTime.Sub(state.rtcTime).Seconds()
		if delta < 0 {
			delta = -delta
		}
		metrics["time.rtc_delta_s"] = delta
	}

	labels := map[string]string{
		"backend":  "timedatectl",
		"timezone": state.timezone,
	}

	return []health.Observation{{
		SourceID:    c.cfg.SourceID,
		SourceType:  "time_sync",
		CollectedAt: time.Now(),
		Metrics:     metrics,
		Labels:      labels,
	}}, nil
}

func runTimedatectlShow(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "timedatectl", "show")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("timedatectl show: %w", err)
	}
	return output, nil
}

func parseTimedatectlShow(raw []byte) (syncState, error) {
	var state syncState
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return syncState{}, fmt.Errorf("unexpected timedatectl line %q", line)
		}
		switch key {
		case "Timezone":
			state.timezone = value
		case "LocalRTC":
			state.localRTC = parseYesNo(value)
		case "CanNTP":
			state.canNTP = parseYesNo(value)
		case "NTP":
			state.ntpEnabled = parseYesNo(value)
		case "NTPSynchronized":
			state.ntpSynchronized = parseYesNo(value)
		case "TimeUSec":
			state.systemTime = parseTimeValue(value)
		case "RTCTimeUSec":
			state.rtcTime = parseTimeValue(value)
		}
	}
	return state, nil
}

func parseYesNo(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "yes")
}

func parseTimeValue(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse("Mon 2006-01-02 15:04:05 MST", value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func boolMetric(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
