package can

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
	CollectedAt      string             `json:"collected_at"`
	LinkUp           bool               `json:"link_up"`
	Bitrate          int                `json:"bitrate"`
	OnlineNodes      int                `json:"online_nodes"`
	OnlineNodesKnown bool               `json:"online_nodes_known"`
	RXErrors         uint64             `json:"rx_errors"`
	TXErrors         uint64             `json:"tx_errors"`
	BusOffCount      uint64             `json:"bus_off_count"`
	RestartCount     uint64             `json:"restart_count"`
	State            string             `json:"state"`
	Labels           map[string]string  `json:"labels"`
	Metrics          map[string]float64 `json:"metrics"`
}

func probeCommandJSON(ctx context.Context, _ string, iface config.CANInterfaceConfig) (InterfaceStatus, error) {
	raw, err := runProbeCommand(ctx, iface.ProbeCommand)
	if err != nil {
		return InterfaceStatus{}, err
	}
	return parseCommandJSONOutput(raw)
}

func parseCommandJSONOutput(raw []byte) (InterfaceStatus, error) {
	var payload commandJSONPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return InterfaceStatus{}, fmt.Errorf("decode command JSON: %w", err)
	}

	status := InterfaceStatus{
		LinkUp:            payload.LinkUp,
		Bitrate:           payload.Bitrate,
		OnlineNodes:       payload.OnlineNodes,
		OnlineNodesKnown:  payload.OnlineNodesKnown,
		RXErrors:          payload.RXErrors,
		TXErrors:          payload.TXErrors,
		BusOffCount:       payload.BusOffCount,
		RestartCount:      payload.RestartCount,
		State:             payload.State,
		AdditionalInfo:    payload.Labels,
		AdditionalMetrics: payload.Metrics,
	}

	if payload.CollectedAt != "" {
		collectedAt, err := time.Parse(time.RFC3339Nano, payload.CollectedAt)
		if err != nil {
			return InterfaceStatus{}, fmt.Errorf("parse collected_at: %w", err)
		}
		status.CollectedAt = collectedAt
	}

	return status, nil
}
