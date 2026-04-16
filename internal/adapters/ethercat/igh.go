package ethercat

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"watchdog/internal/config"
)

var runEthercatCommand = func(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ethercat", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ethercat %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func probeIgHCLI(ctx context.Context, _ string, master config.EtherCATMasterConfig) (MasterStatus, error) {
	status, err := gatherSlaveStatus(ctx, master)
	if err != nil {
		return MasterStatus{}, err
	}

	if masterIndex, ok := extractMasterIndex(master.Name); ok {
		if output, err := runEthercatCommand(ctx, "master", "--master", masterIndex); err == nil {
			mergeMasterStatus(&status, parseMasterOutput(output))
		}
	}

	return status, nil
}

func gatherSlaveStatus(ctx context.Context, master config.EtherCATMasterConfig) (MasterStatus, error) {
	args := []string{"slaves"}
	if masterIndex, ok := extractMasterIndex(master.Name); ok {
		args = append(args, "--master", masterIndex)
	}
	output, err := runEthercatCommand(ctx, args...)
	if err != nil {
		return MasterStatus{}, err
	}
	return parseSlavesOutput(output)
}

func parseSlavesOutput(raw []byte) (MasterStatus, error) {
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return MasterStatus{}, fmt.Errorf("empty ethercat slaves output")
	}

	status := MasterStatus{
		AdditionalInfo: map[string]string{
			"probe": "igh-cli",
		},
	}

	states := make(map[string]bool)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return MasterStatus{}, fmt.Errorf("unexpected slave line %q", line)
		}
		status.SlavesSeen++
		states[strings.ToLower(fields[2])] = true
		if fields[3] == "E" {
			status.SlaveErrors++
		}
	}

	status.MasterState = deriveState(states)
	if status.MasterState == "" {
		status.MasterState = "unknown"
	}
	return status, nil
}

func parseMasterOutput(raw []byte) MasterStatus {
	status := MasterStatus{}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)

		switch key {
		case "phase", "state":
			status.MasterState = strings.ToLower(value)
		case "slaves responding":
			if n, err := strconv.Atoi(firstField(value)); err == nil {
				status.SlavesSeen = n
			}
		case "link", "main":
			status.LinkKnown = true
			status.LinkUp = parseLinkValue(value)
		case "working counter":
			if n, err := strconv.Atoi(firstField(value)); err == nil {
				status.WorkingCounter = n
			}
		case "expected working counter", "working counter expected":
			if n, err := strconv.Atoi(firstField(value)); err == nil {
				status.WorkingCounterExpected = n
			}
		}
	}
	return status
}

func mergeMasterStatus(dst *MasterStatus, src MasterStatus) {
	if src.LinkKnown {
		dst.LinkKnown = src.LinkKnown
		dst.LinkUp = src.LinkUp
	}
	if src.MasterState != "" {
		dst.MasterState = src.MasterState
	}
	if src.SlavesSeen > 0 {
		dst.SlavesSeen = src.SlavesSeen
	}
	if src.WorkingCounter > 0 {
		dst.WorkingCounter = src.WorkingCounter
	}
	if src.WorkingCounterExpected > 0 {
		dst.WorkingCounterExpected = src.WorkingCounterExpected
	}
}

func extractMasterIndex(name string) (string, bool) {
	value := strings.TrimSpace(strings.ToLower(name))
	if value == "" {
		return "", false
	}
	if idx, ok := strings.CutPrefix(value, "master"); ok && idx != "" {
		if _, err := strconv.Atoi(idx); err == nil {
			return idx, true
		}
	}
	if _, err := strconv.Atoi(value); err == nil {
		return value, true
	}
	return "", false
}

func deriveState(states map[string]bool) string {
	switch {
	case states["init"]:
		return "init"
	case states["preop"]:
		return "preop"
	case states["safeop"]:
		return "safeop"
	case states["op"]:
		return "op"
	default:
		return ""
	}
}

func parseLinkValue(value string) bool {
	switch strings.ToLower(firstField(value)) {
	case "up", "yes", "true":
		return true
	default:
		return false
	}
}

func firstField(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
