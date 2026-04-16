package ethercat

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"watchdog/internal/config"
)

type soemJSONPayload struct {
	commandJSONPayload
	Interface string            `json:"interface"`
	Slaves    []soemSlaveStatus `json:"slaves"`
}

type soemSlaveStatus struct {
	Position int    `json:"position"`
	Name     string `json:"name"`
	State    string `json:"state"`
	Lost     bool   `json:"lost"`
	Error    string `json:"error"`
}

func probeSOEM(ctx context.Context, _ string, master config.EtherCATMasterConfig) (MasterStatus, error) {
	raw, err := runProbeCommand(ctx, master.ProbeCommand)
	if err != nil {
		return MasterStatus{}, err
	}
	return parseSOEMJSONOutput(raw, master.ExpectedState)
}

func parseSOEMJSONOutput(raw []byte, expectedState string) (MasterStatus, error) {
	var payload soemJSONPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return MasterStatus{}, fmt.Errorf("decode SOEM JSON: %w", err)
	}

	status, err := masterStatusFromCommandPayload(payload.commandJSONPayload)
	if err != nil {
		return MasterStatus{}, err
	}
	if status.AdditionalInfo == nil {
		status.AdditionalInfo = make(map[string]string)
	}
	if status.AdditionalMetrics == nil {
		status.AdditionalMetrics = make(map[string]float64)
	}

	status.AdditionalInfo["ethercat.stack"] = "soem"
	if payload.Interface != "" {
		status.AdditionalInfo["soem.interface"] = payload.Interface
	}

	if status.SlavesSeen == 0 && len(payload.Slaves) > 0 {
		status.SlavesSeen = len(payload.Slaves)
	}
	if status.MasterState == "" {
		status.MasterState = deriveSOEMMasterState(payload.Slaves, expectedState)
	}

	lostCount := 0
	notOperationalCount := 0
	faultedCount := 0
	faultedPositions := make([]string, 0)
	faultedNames := make([]string, 0)
	normalizedExpected := normalizeALState(expectedState)
	if normalizedExpected == "" {
		normalizedExpected = "op"
	}

	for _, slave := range payload.Slaves {
		normalizedState := normalizeALState(slave.State)
		hasFault := false
		if slave.Lost {
			lostCount++
			hasFault = true
		}
		if slave.Error != "" {
			hasFault = true
		}
		if normalizedState != "" && normalizedState != normalizedExpected {
			notOperationalCount++
			hasFault = true
		}
		if hasFault {
			faultedCount++
			if slave.Position >= 0 {
				faultedPositions = append(faultedPositions, strconv.Itoa(slave.Position))
			}
			if slave.Name != "" {
				faultedNames = append(faultedNames, slave.Name)
			}
		}
	}

	status.AdditionalMetrics["ethercat.slaves_lost"] = float64(lostCount)
	status.AdditionalMetrics["ethercat.slaves_not_op"] = float64(notOperationalCount)
	status.AdditionalMetrics["ethercat.slaves_faulted"] = float64(faultedCount)

	if status.SlaveErrors == 0 {
		status.SlaveErrors = faultedCount
	}
	if len(faultedPositions) > 0 {
		status.AdditionalInfo["faulted_slave_positions"] = strings.Join(faultedPositions, ",")
	}
	if len(faultedNames) > 0 {
		status.AdditionalInfo["faulted_slave_names"] = strings.Join(faultedNames, ",")
	}

	return status, nil
}

func deriveSOEMMasterState(slaves []soemSlaveStatus, fallback string) string {
	expected := normalizeALState(fallback)
	if expected == "" {
		expected = "op"
	}
	if len(slaves) == 0 {
		return expected
	}

	derived := expected
	for _, slave := range slaves {
		state := normalizeALState(slave.State)
		switch state {
		case "init":
			return "init"
		case "preop":
			if derived != "init" {
				derived = "preop"
			}
		case "boot":
			if derived == "op" {
				derived = "boot"
			}
		case "safeop":
			if derived == "op" || derived == "boot" {
				derived = "safeop"
			}
		case "op":
			// keep current derived state
		}
	}
	return derived
}

func normalizeALState(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if base, _, ok := strings.Cut(value, "+"); ok {
		value = base
	}
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, "_", "")
	switch value {
	case "init":
		return "init"
	case "preop", "preoperational":
		return "preop"
	case "boot":
		return "boot"
	case "safeop", "safeoperational":
		return "safeop"
	case "op", "operational":
		return "op"
	default:
		return value
	}
}
