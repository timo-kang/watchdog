package rules

import (
	"fmt"
	"strings"
	"time"

	"watchdog/internal/config"
	"watchdog/internal/health"
)

type Evaluator struct {
	cfg config.RulesConfig
}

func New(cfg config.RulesConfig) *Evaluator {
	return &Evaluator{cfg: cfg}
}

func (e *Evaluator) Evaluate(observation health.Observation) health.Status {
	status := health.Status{
		SourceID:   observation.SourceID,
		SourceType: observation.SourceType,
		Severity:   health.SeverityOK,
		ObservedAt: observation.CollectedAt,
		Metrics:    cloneMetrics(observation.Metrics),
		Labels:     cloneLabels(observation.Labels),
	}
	if status.Metrics == nil {
		status.Metrics = make(map[string]float64)
	}

	switch observation.SourceType {
	case "host":
		return e.evaluateHost(status)
	case "module":
		return e.evaluateModule(status, observation)
	case "process":
		return e.evaluateProcess(status)
	case "can":
		return e.evaluateCAN(status)
	case "ethercat":
		return e.evaluateEtherCAT(status)
	default:
		return status
	}
}

func (e *Evaluator) evaluateHost(status health.Status) health.Status {
	rules := e.cfg.Host
	var reasons []string

	loadRatio := metric(status.Metrics, "load.1m") / max(metric(status.Metrics, "cpu.count"), 1)
	status.Metrics["load.ratio"] = loadRatio

	update := func(next health.Severity, reason string) {
		if health.CompareSeverity(next, status.Severity) > 0 {
			status.Severity = next
		}
		reasons = append(reasons, reason)
	}

	if temp := metric(status.Metrics, "temp.cpu_max_c"); temp > 0 {
		switch {
		case temp >= rules.MaxCPUTempCriticalC:
			update(health.SeverityFail, fmt.Sprintf("cpu temp %.1fC >= critical %.1fC", temp, rules.MaxCPUTempCriticalC))
		case temp >= rules.MaxCPUTempWarnC:
			update(health.SeverityWarn, fmt.Sprintf("cpu temp %.1fC >= warn %.1fC", temp, rules.MaxCPUTempWarnC))
		}
	}

	if temp := metric(status.Metrics, "temp.max_c"); temp > 0 {
		switch {
		case temp >= rules.MaxTempCriticalC:
			update(health.SeverityFail, fmt.Sprintf("max temp %.1fC >= critical %.1fC", temp, rules.MaxTempCriticalC))
		case temp >= rules.MaxTempWarnC:
			update(health.SeverityWarn, fmt.Sprintf("max temp %.1fC >= warn %.1fC", temp, rules.MaxTempWarnC))
		}
	}

	if memAvail := metric(status.Metrics, "mem.available_mb"); memAvail > 0 {
		switch {
		case memAvail <= rules.MemAvailableCriticalMB:
			update(health.SeverityFail, fmt.Sprintf("available memory %.0fMB <= critical %.0fMB", memAvail, rules.MemAvailableCriticalMB))
		case memAvail <= rules.MemAvailableWarnMB:
			update(health.SeverityWarn, fmt.Sprintf("available memory %.0fMB <= warn %.0fMB", memAvail, rules.MemAvailableWarnMB))
		}
	}

	switch {
	case loadRatio >= rules.LoadRatioCritical:
		update(health.SeverityFail, fmt.Sprintf("load ratio %.2f >= critical %.2f", loadRatio, rules.LoadRatioCritical))
	case loadRatio >= rules.LoadRatioWarn:
		update(health.SeverityWarn, fmt.Sprintf("load ratio %.2f >= warn %.2f", loadRatio, rules.LoadRatioWarn))
	}

	status.Reason = strings.Join(reasons, "; ")
	return status
}

func (e *Evaluator) evaluateModule(status health.Status, observation health.Observation) health.Status {
	age := time.Since(observation.CollectedAt)
	status.Metrics["age.s"] = age.Seconds()
	if observation.StaleAfter > 0 {
		status.Metrics["stale_after.s"] = observation.StaleAfter.Seconds()
	}

	if observation.StaleAfter > 0 && age > observation.StaleAfter {
		status.Severity = health.SeverityStale
		status.Reason = fmt.Sprintf("last report %.2fs ago > stale_after %.2fs", age.Seconds(), observation.StaleAfter.Seconds())
		if observation.ReportedSeverity != "" && observation.ReportedSeverity != health.SeverityOK {
			status.Reason = fmt.Sprintf("%s; last reported %s: %s", status.Reason, observation.ReportedSeverity, observation.ReportedReason)
		}
		return status
	}

	if observation.ReportedSeverity != "" {
		status.Severity = observation.ReportedSeverity
	}
	status.Reason = observation.ReportedReason
	return status
}

func (e *Evaluator) evaluateProcess(status health.Status) health.Status {
	rules := e.cfg.Process
	var reasons []string

	update := func(next health.Severity, reason string) {
		if health.CompareSeverity(next, status.Severity) > 0 {
			status.Severity = next
		}
		reasons = append(reasons, reason)
	}

	loadState := label(status.Labels, "load_state")
	activeState := label(status.Labels, "active_state")
	subState := label(status.Labels, "sub_state")

	if loadState != "" && loadState != "loaded" {
		update(health.SeverityFail, fmt.Sprintf("load state %q != loaded", loadState))
	}

	switch activeState {
	case "active":
		// ok
	case "activating", "reloading":
		update(health.SeverityWarn, fmt.Sprintf("active state %q sub_state %q", activeState, subState))
	case "":
		update(health.SeverityFail, "missing active state")
	default:
		update(health.SeverityFail, fmt.Sprintf("active state %q sub_state %q", activeState, subState))
	}

	if metric(status.Metrics, "process.require_main_pid") > 0 && metric(status.Metrics, "process.main_pid") <= 0 {
		update(health.SeverityFail, "main pid missing")
	}

	restarts := uint64(metric(status.Metrics, "process.restarts"))
	switch {
	case rules.RestartFail > 0 && restarts >= rules.RestartFail:
		update(health.SeverityFail, fmt.Sprintf("restart count %d >= fail %d", restarts, rules.RestartFail))
	case rules.RestartWarn > 0 && restarts >= rules.RestartWarn:
		update(health.SeverityWarn, fmt.Sprintf("restart count %d >= warn %d", restarts, rules.RestartWarn))
	}

	status.Reason = strings.Join(reasons, "; ")
	return status
}

func (e *Evaluator) evaluateCAN(status health.Status) health.Status {
	rules := e.cfg.CAN
	var reasons []string

	update := func(next health.Severity, reason string) {
		if health.CompareSeverity(next, status.Severity) > 0 {
			status.Severity = next
		}
		reasons = append(reasons, reason)
	}

	if metric(status.Metrics, "can.require_up") > 0 && metric(status.Metrics, "can.link_up") <= 0 {
		update(health.SeverityFail, "link is down")
	}

	expectedBitrate := metric(status.Metrics, "can.expected_bitrate")
	bitrate := metric(status.Metrics, "can.bitrate")
	if expectedBitrate > 0 && bitrate > 0 && bitrate != expectedBitrate {
		update(health.SeverityWarn, fmt.Sprintf("bitrate %.0f != expected %.0f", bitrate, expectedBitrate))
	}

	if busOffCount := metric(status.Metrics, "can.bus_off_count"); busOffCount > 0 {
		update(health.SeverityFail, fmt.Sprintf("bus off count %.0f > 0", busOffCount))
	}

	restarts := uint64(metric(status.Metrics, "can.restart_count"))
	switch {
	case rules.RestartFail > 0 && restarts >= rules.RestartFail:
		update(health.SeverityFail, fmt.Sprintf("restart count %d >= fail %d", restarts, rules.RestartFail))
	case rules.RestartWarn > 0 && restarts >= rules.RestartWarn:
		update(health.SeverityWarn, fmt.Sprintf("restart count %d >= warn %d", restarts, rules.RestartWarn))
	}

	if metric(status.Metrics, "can.online_nodes_known") > 0 {
		missingNodes := int(max(metric(status.Metrics, "can.expected_nodes")-metric(status.Metrics, "can.online_nodes"), 0))
		switch {
		case rules.MissingNodesFail > 0 && missingNodes >= rules.MissingNodesFail:
			update(health.SeverityFail, fmt.Sprintf("missing nodes %d >= fail %d", missingNodes, rules.MissingNodesFail))
		case rules.MissingNodesWarn > 0 && missingNodes >= rules.MissingNodesWarn:
			update(health.SeverityWarn, fmt.Sprintf("missing nodes %d >= warn %d", missingNodes, rules.MissingNodesWarn))
		}
	}

	status.Reason = strings.Join(reasons, "; ")
	return status
}

func (e *Evaluator) evaluateEtherCAT(status health.Status) health.Status {
	rules := e.cfg.EtherCAT
	var reasons []string

	update := func(next health.Severity, reason string) {
		if health.CompareSeverity(next, status.Severity) > 0 {
			status.Severity = next
		}
		reasons = append(reasons, reason)
	}

	if metric(status.Metrics, "ethercat.require_link") > 0 && metric(status.Metrics, "ethercat.link_known") > 0 && metric(status.Metrics, "ethercat.link_up") <= 0 {
		update(health.SeverityFail, "link is down")
	}

	state := strings.ToLower(label(status.Labels, "master_state"))
	expectedState := strings.ToLower(label(status.Labels, "expected_state"))
	if expectedState == "" {
		expectedState = "op"
	}
	switch {
	case state == "":
		update(health.SeverityFail, "missing master state")
	case state == expectedState:
		// ok
	case state == "safeop" || state == "preop":
		update(health.SeverityWarn, fmt.Sprintf("state %q != expected %q", state, expectedState))
	default:
		update(health.SeverityFail, fmt.Sprintf("state %q != expected %q", state, expectedState))
	}

	missingSlaves := int(max(metric(status.Metrics, "ethercat.expected_slaves")-metric(status.Metrics, "ethercat.slaves_seen"), 0))
	switch {
	case rules.MissingSlavesFail > 0 && missingSlaves >= rules.MissingSlavesFail:
		update(health.SeverityFail, fmt.Sprintf("missing slaves %d >= fail %d", missingSlaves, rules.MissingSlavesFail))
	case rules.MissingSlavesWarn > 0 && missingSlaves >= rules.MissingSlavesWarn:
		update(health.SeverityWarn, fmt.Sprintf("missing slaves %d >= warn %d", missingSlaves, rules.MissingSlavesWarn))
	}

	if slaveErrors := int(metric(status.Metrics, "ethercat.slave_errors")); slaveErrors > 0 {
		update(health.SeverityWarn, fmt.Sprintf("slave errors %d > 0", slaveErrors))
	}
	if slavesLost := int(metric(status.Metrics, "ethercat.slaves_lost")); slavesLost > 0 {
		update(health.SeverityFail, fmt.Sprintf("lost slaves %d > 0", slavesLost))
	}
	if slavesNotOp := int(metric(status.Metrics, "ethercat.slaves_not_op")); slavesNotOp > 0 {
		update(health.SeverityWarn, fmt.Sprintf("non-operational slaves %d > 0", slavesNotOp))
	}

	expectedWKC := metric(status.Metrics, "ethercat.working_counter_goal")
	if expectedWKC > 0 {
		ratio := metric(status.Metrics, "ethercat.working_counter") / expectedWKC
		status.Metrics["ethercat.wkc_ratio"] = ratio
		switch {
		case rules.WKCFailRatio > 0 && ratio < rules.WKCFailRatio:
			update(health.SeverityFail, fmt.Sprintf("wkc ratio %.2f < fail %.2f", ratio, rules.WKCFailRatio))
		case rules.WKCWarnRatio > 0 && ratio < rules.WKCWarnRatio:
			update(health.SeverityWarn, fmt.Sprintf("wkc ratio %.2f < warn %.2f", ratio, rules.WKCWarnRatio))
		}
	}

	status.Reason = strings.Join(reasons, "; ")
	return status
}

func metric(values map[string]float64, key string) float64 {
	if values == nil {
		return 0
	}
	return values[key]
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func label(values map[string]string, key string) string {
	if values == nil {
		return ""
	}
	return values[key]
}

func cloneMetrics(values map[string]float64) map[string]float64 {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]float64, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func cloneLabels(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
