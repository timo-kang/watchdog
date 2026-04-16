package ethercat

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"watchdog/internal/adapters"
	"watchdog/internal/config"
)

var runProbeCommand = adapters.RunCommand

type commandJSONPayload struct {
	CollectedAt            string             `json:"collected_at"`
	LinkKnown              bool               `json:"link_known"`
	LinkUp                 bool               `json:"link_up"`
	MasterState            string             `json:"master_state"`
	SlavesSeen             int                `json:"slaves_seen"`
	SlaveErrors            int                `json:"slave_errors"`
	WorkingCounter         int                `json:"working_counter"`
	WorkingCounterExpected int                `json:"working_counter_expected"`
	Labels                 map[string]string  `json:"labels"`
	Metrics                map[string]float64 `json:"metrics"`
}

func probeCommandJSON(ctx context.Context, _ string, master config.EtherCATMasterConfig) (MasterStatus, error) {
	raw, err := runProbeCommand(ctx, master.ProbeCommand)
	if err != nil {
		return MasterStatus{}, err
	}
	return parseCommandJSONOutput(raw)
}

func parseCommandJSONOutput(raw []byte) (MasterStatus, error) {
	var payload commandJSONPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return MasterStatus{}, fmt.Errorf("decode command JSON: %w", err)
	}
	return masterStatusFromCommandPayload(payload)
}

func masterStatusFromCommandPayload(payload commandJSONPayload) (MasterStatus, error) {
	status := MasterStatus{
		LinkKnown:              payload.LinkKnown,
		LinkUp:                 payload.LinkUp,
		MasterState:            payload.MasterState,
		SlavesSeen:             payload.SlavesSeen,
		SlaveErrors:            payload.SlaveErrors,
		WorkingCounter:         payload.WorkingCounter,
		WorkingCounterExpected: payload.WorkingCounterExpected,
		AdditionalInfo:         payload.Labels,
		AdditionalMetrics:      payload.Metrics,
	}

	if payload.CollectedAt != "" {
		collectedAt, err := time.Parse(time.RFC3339Nano, payload.CollectedAt)
		if err != nil {
			return MasterStatus{}, fmt.Errorf("parse collected_at: %w", err)
		}
		status.CollectedAt = collectedAt
	}

	return status, nil
}
