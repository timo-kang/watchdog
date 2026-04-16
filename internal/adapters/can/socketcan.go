package can

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"watchdog/internal/config"
)

var runIPLinkShow = func(ctx context.Context, iface string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ip", "-details", "-statistics", "link", "show", "dev", iface)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ip link show dev %s: %w: %s", iface, err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func probeSocketCAN(ctx context.Context, _ string, iface config.CANInterfaceConfig) (InterfaceStatus, error) {
	output, err := runIPLinkShow(ctx, iface.Name)
	if err != nil {
		return InterfaceStatus{}, err
	}
	return parseSocketCANOutput(output)
}

func parseSocketCANOutput(raw []byte) (InterfaceStatus, error) {
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return InterfaceStatus{}, fmt.Errorf("empty ip output")
	}

	status := InterfaceStatus{
		AdditionalInfo: map[string]string{
			"probe": "iproute2",
		},
	}

	firstFields := strings.Fields(strings.TrimSpace(lines[0]))
	if len(firstFields) < 3 {
		return InterfaceStatus{}, fmt.Errorf("unexpected header line %q", lines[0])
	}
	flags := strings.Trim(firstFields[2], "<>")
	status.LinkUp = containsCSV(flags, "UP")
	status.AdditionalInfo["flags"] = strings.ToLower(flags)
	if operState, ok := valueAfterToken(firstFields, "state"); ok {
		status.AdditionalInfo["oper_state"] = strings.ToLower(operState)
	}

	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "can "):
			if busState, ok := parseCANState(line); ok {
				status.State = busState
			}
		case strings.HasPrefix(line, "bitrate "):
			if bitrate, ok := parseIntAfterToken(strings.Fields(line), "bitrate"); ok {
				status.Bitrate = bitrate
			}
		case strings.Contains(line, "re-started") && strings.Contains(line, "bus-off"):
			if i+1 >= len(lines) {
				return InterfaceStatus{}, fmt.Errorf("missing counter row after %q", line)
			}
			counters := strings.Fields(strings.TrimSpace(lines[i+1]))
			if len(counters) < 6 {
				return InterfaceStatus{}, fmt.Errorf("unexpected counter row %q", lines[i+1])
			}
			status.RestartCount = parseUintOrZero(counters[0])
			status.BusOffCount = parseUintOrZero(counters[5])
			i++
		case strings.HasPrefix(line, "RX:"):
			if i+1 >= len(lines) {
				return InterfaceStatus{}, fmt.Errorf("missing RX values after %q", line)
			}
			values := strings.Fields(strings.TrimSpace(lines[i+1]))
			if len(values) >= 3 {
				status.RXErrors = parseUintOrZero(values[2])
			}
			i++
		case strings.HasPrefix(line, "TX:"):
			if i+1 >= len(lines) {
				return InterfaceStatus{}, fmt.Errorf("missing TX values after %q", line)
			}
			values := strings.Fields(strings.TrimSpace(lines[i+1]))
			if len(values) >= 3 {
				status.TXErrors = parseUintOrZero(values[2])
			}
			i++
		}
	}

	if status.State == "" {
		status.State = "unknown"
	}
	status.OnlineNodesKnown = false
	return status, nil
}

func containsCSV(values string, want string) bool {
	for _, value := range strings.Split(values, ",") {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

func valueAfterToken(fields []string, token string) (string, bool) {
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == token {
			return fields[i+1], true
		}
	}
	return "", false
}

func parseCANState(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return "", false
	}
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "state" {
			return strings.ToLower(strings.Trim(fields[i+1], "()")), true
		}
	}
	return "", false
}

func parseIntAfterToken(fields []string, token string) (int, bool) {
	value, ok := valueAfterToken(fields, token)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func parseUintOrZero(value string) uint64 {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}
