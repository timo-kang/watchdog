package systemd

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"watchdog/internal/adapters"
	"watchdog/internal/config"
	"watchdog/internal/health"
)

var _ adapters.Collector = (*Collector)(nil)

type showRunner func(ctx context.Context, unit string) ([]byte, error)

type Collector struct {
	cfg  config.SystemdSourceConfig
	show showRunner
}

type unitState struct {
	ID            string
	LoadState     string
	ActiveState   string
	SubState      string
	UnitFileState string
	ExecMainPID   int64
	NRestarts     uint64
	Result        string
	InvocationID  string
}

func New(cfg config.SystemdSourceConfig) *Collector {
	return &Collector{
		cfg:  cfg,
		show: runShow,
	}
}

func (c *Collector) Name() string {
	return "systemd"
}

func (c *Collector) Collect(ctx context.Context) ([]health.Observation, error) {
	observations := make([]health.Observation, 0, len(c.cfg.Units))
	collectedAt := time.Now()

	for _, unit := range c.cfg.Units {
		raw, err := c.show(ctx, unit.Name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", unit.Name, err)
		}
		state, err := parseShowOutput(unit.Name, raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", unit.Name, err)
		}

		metrics := map[string]float64{
			"process.main_pid": float64(state.ExecMainPID),
			"process.restarts": float64(state.NRestarts),
		}
		if unit.RequireMainPID {
			metrics["process.require_main_pid"] = 1
		}

		labels := map[string]string{
			"supervisor":      "systemd",
			"unit":            unit.Name,
			"load_state":      state.LoadState,
			"active_state":    state.ActiveState,
			"sub_state":       state.SubState,
			"unit_file_state": state.UnitFileState,
			"result":          state.Result,
			"invocation_id":   state.InvocationID,
		}

		observations = append(observations, health.Observation{
			SourceID:    unit.SourceID,
			SourceType:  "process",
			CollectedAt: collectedAt,
			Metrics:     metrics,
			Labels:      labels,
		})
	}

	return observations, nil
}

func runShow(ctx context.Context, unit string) ([]byte, error) {
	cmd := exec.CommandContext(
		ctx,
		"systemctl",
		"show",
		"--property=Id,LoadState,ActiveState,SubState,UnitFileState,ExecMainPID,NRestarts,Result,InvocationID",
		unit,
	)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return output, nil
}

func parseShowOutput(unit string, raw []byte) (unitState, error) {
	state := unitState{ID: unit}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return unitState{}, fmt.Errorf("unexpected systemctl output line %q", line)
		}
		switch key {
		case "Id":
			if value != "" {
				state.ID = value
			}
		case "LoadState":
			state.LoadState = value
		case "ActiveState":
			state.ActiveState = value
		case "SubState":
			state.SubState = value
		case "UnitFileState":
			state.UnitFileState = value
		case "ExecMainPID":
			pid, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return unitState{}, fmt.Errorf("parse ExecMainPID: %w", err)
			}
			state.ExecMainPID = pid
		case "NRestarts":
			restarts, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return unitState{}, fmt.Errorf("parse NRestarts: %w", err)
			}
			state.NRestarts = restarts
		case "Result":
			state.Result = value
		case "InvocationID":
			state.InvocationID = value
		}
	}

	if state.LoadState == "" {
		return unitState{}, fmt.Errorf("missing LoadState")
	}
	if state.ActiveState == "" {
		return unitState{}, fmt.Errorf("missing ActiveState")
	}
	return state, nil
}
