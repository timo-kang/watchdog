package rules

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"watchdog/internal/config"
	"watchdog/internal/health"
)

type Evaluator struct {
	cfg          config.RulesConfig
	mu           sync.Mutex
	moduleTiming map[string]moduleTimingState
}

type moduleTimingState struct {
	severity     health.Severity
	warnCount    int
	failCount    int
	recoverCount int
	observedAt   time.Time
}

func New(cfg config.RulesConfig) *Evaluator {
	return &Evaluator{
		cfg:          cfg,
		moduleTiming: make(map[string]moduleTimingState),
	}
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
	if e.applyStaleness(&status, observation) {
		return status
	}

	switch observation.SourceType {
	case "host":
		return e.evaluateHost(status)
	case "module", "drive":
		return e.evaluateModule(status, observation)
	case "process":
		return e.evaluateProcess(status)
	case "can":
		return e.evaluateCAN(status)
	case "ethercat":
		return e.evaluateEtherCAT(status)
	case "network":
		return e.evaluateNetwork(status)
	case "power":
		return e.evaluatePower(status)
	case "storage":
		return e.evaluateStorage(status)
	case "time_sync":
		return e.evaluateTimeSync(status)
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
	rules := e.cfg.Module
	var reasons []string

	update := func(next health.Severity, reason string) {
		if health.CompareSeverity(next, status.Severity) > 0 {
			status.Severity = next
		}
		reasons = append(reasons, reason)
	}

	if observation.ReportedSeverity != "" {
		status.Severity = observation.ReportedSeverity
	}
	if observation.ReportedReason != "" {
		reasons = append(reasons, observation.ReportedReason)
	}

	if severity, reason := e.evaluateModuleControlPeriod(&status, rules); severity != health.SeverityOK {
		update(severity, reason)
	}
	for _, finding := range evaluateDriveDiagnostics(&status, rules) {
		update(finding.severity, finding.reason)
	}

	status.Reason = strings.Join(reasons, "; ")
	return status
}

type driveDiagnosticFinding struct {
	severity health.Severity
	reason   string
}

func evaluateDriveDiagnostics(status *health.Status, rules config.ModuleRules) []driveDiagnosticFinding {
	if status == nil || len(status.Metrics) == 0 {
		return nil
	}
	if currentLimit := metric(status.Metrics, "drive.current_limit_a"); currentLimit > 0 {
		if _, ok := status.Metrics["drive.current_ratio"]; !ok {
			if current := metric(status.Metrics, "drive.current_a"); current > 0 {
				status.Metrics["drive.current_ratio"] = current / currentLimit
			}
		}
		if peak := metric(status.Metrics, "drive.current_peak_a"); peak > 0 {
			status.Metrics["drive.current_peak_ratio"] = peak / currentLimit
		}
	}

	var findings []driveDiagnosticFinding
	addHighThresholdFinding(&findings, metric(status.Metrics, "drive.current_ratio"), rules.DriveCurrentRatioWarn, rules.DriveCurrentRatioFail, "drive current ratio", "%.2f")
	addHighThresholdFinding(&findings, metric(status.Metrics, "drive.current_peak_ratio"), rules.DriveCurrentRatioWarn, rules.DriveCurrentRatioFail, "drive peak current ratio", "%.2f")
	addHighThresholdFinding(&findings, metric(status.Metrics, "drive.motor_temp_c"), rules.DriveMotorTempWarnC, rules.DriveMotorTempFailC, "drive motor temp", "%.1fC")
	addHighThresholdFinding(&findings, metric(status.Metrics, "drive.driver_temp_c"), rules.DriveDriverTempWarnC, rules.DriveDriverTempFailC, "drive driver temp", "%.1fC")
	addHighThresholdFinding(&findings, metric(status.Metrics, "drive.thermal_load_pct"), rules.DriveThermalLoadWarnPct, rules.DriveThermalLoadFailPct, "drive thermal load", "%.1f%%")
	if busVoltage, ok := status.Metrics["drive.bus_voltage_v"]; ok {
		addLowThresholdFinding(&findings, busVoltage, rules.DriveBusVoltageMinWarnV, rules.DriveBusVoltageMinFailV, "drive bus voltage", "%.1fV")
	}

	if rules.DriveFaultCodeFail {
		if faultCode, ok := status.Metrics["drive.fault_code"]; ok && faultCode != 0 {
			findings = append(findings, driveDiagnosticFinding{
				severity: health.SeverityFail,
				reason:   fmt.Sprintf("drive fault code %.0f != 0", faultCode),
			})
		}
	}
	return findings
}

func addHighThresholdFinding(findings *[]driveDiagnosticFinding, value, warn, fail float64, name, format string) {
	if value == 0 {
		return
	}
	switch {
	case fail > 0 && value >= fail:
		*findings = append(*findings, driveDiagnosticFinding{
			severity: health.SeverityFail,
			reason:   fmt.Sprintf(name+" "+format+" >= fail "+format, value, fail),
		})
	case warn > 0 && value >= warn:
		*findings = append(*findings, driveDiagnosticFinding{
			severity: health.SeverityWarn,
			reason:   fmt.Sprintf(name+" "+format+" >= warn "+format, value, warn),
		})
	}
}

func addLowThresholdFinding(findings *[]driveDiagnosticFinding, value, warn, fail float64, name, format string) {
	switch {
	case fail > 0 && value <= fail:
		*findings = append(*findings, driveDiagnosticFinding{
			severity: health.SeverityFail,
			reason:   fmt.Sprintf(name+" "+format+" <= fail "+format, value, fail),
		})
	case warn > 0 && value <= warn:
		*findings = append(*findings, driveDiagnosticFinding{
			severity: health.SeverityWarn,
			reason:   fmt.Sprintf(name+" "+format+" <= warn "+format, value, warn),
		})
	}
}

func (e *Evaluator) evaluateModuleControlPeriod(status *health.Status, rules config.ModuleRules) (health.Severity, string) {
	if rules.ControlPeriodWarnUS <= 0 && rules.ControlPeriodFailUS <= 0 {
		return health.SeverityOK, ""
	}

	controlPeriodUS, ok := status.Metrics["control_period_us"]
	if !ok {
		e.resetModuleTiming(status.SourceID)
		return health.SeverityOK, ""
	}

	warnRequired := positiveOrDefault(rules.ControlPeriodWarnConsecutive, 3)
	failRequired := positiveOrDefault(rules.ControlPeriodFailConsecutive, 5)
	recoverRequired := positiveOrDefault(rules.ControlPeriodRecoverConsecutive, 3)

	e.mu.Lock()
	defer e.mu.Unlock()

	state := e.moduleTiming[status.SourceID]
	if state.severity == "" {
		state.severity = health.SeverityOK
	}
	if !state.observedAt.IsZero() && state.observedAt.Equal(status.ObservedAt) {
		setModuleTimingMetrics(status, state)
		if state.severity == health.SeverityOK {
			return health.SeverityOK, ""
		}
		return state.severity, moduleTimingReason(controlPeriodUS, rules, state, warnRequired, failRequired, recoverRequired)
	}
	state.observedAt = status.ObservedAt

	switch {
	case rules.ControlPeriodFailUS > 0 && controlPeriodUS >= rules.ControlPeriodFailUS:
		state.failCount++
		state.warnCount++
		state.recoverCount = 0
		if state.failCount >= failRequired {
			state.severity = health.SeverityFail
		} else if rules.ControlPeriodWarnUS > 0 && state.warnCount >= warnRequired && state.severity != health.SeverityFail {
			state.severity = health.SeverityWarn
		}
	case rules.ControlPeriodWarnUS > 0 && controlPeriodUS >= rules.ControlPeriodWarnUS:
		state.warnCount++
		state.failCount = 0
		state.recoverCount = 0
		if state.severity != health.SeverityFail && state.warnCount >= warnRequired {
			state.severity = health.SeverityWarn
		}
	default:
		state.warnCount = 0
		state.failCount = 0
		if state.severity == health.SeverityOK {
			delete(e.moduleTiming, status.SourceID)
			setModuleTimingMetrics(status, moduleTimingState{severity: health.SeverityOK})
			return health.SeverityOK, ""
		}
		state.recoverCount++
		if state.recoverCount >= recoverRequired {
			delete(e.moduleTiming, status.SourceID)
			setModuleTimingMetrics(status, moduleTimingState{severity: health.SeverityOK})
			return health.SeverityOK, ""
		}
	}

	e.moduleTiming[status.SourceID] = state
	setModuleTimingMetrics(status, state)
	return state.severity, moduleTimingReason(controlPeriodUS, rules, state, warnRequired, failRequired, recoverRequired)
}

func (e *Evaluator) resetModuleTiming(sourceID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.moduleTiming, sourceID)
}

func setModuleTimingMetrics(status *health.Status, state moduleTimingState) {
	status.Metrics["control_period_warn_count"] = float64(state.warnCount)
	status.Metrics["control_period_fail_count"] = float64(state.failCount)
	status.Metrics["control_period_recover_count"] = float64(state.recoverCount)
	status.Metrics["control_period_timing_state"] = severityMetric(state.severity)
}

func moduleTimingReason(controlPeriodUS float64, rules config.ModuleRules, state moduleTimingState, warnRequired, failRequired, recoverRequired int) string {
	if state.recoverCount > 0 {
		limitName := "warn"
		limit := rules.ControlPeriodWarnUS
		if limit <= 0 {
			limitName = "fail"
			limit = rules.ControlPeriodFailUS
		}
		return fmt.Sprintf(
			"control period %.0fus below %s %.0fus; recovering %d/%d after %s",
			controlPeriodUS,
			limitName,
			limit,
			state.recoverCount,
			recoverRequired,
			state.severity,
		)
	}

	if state.severity == health.SeverityFail {
		if rules.ControlPeriodFailUS > 0 && controlPeriodUS >= rules.ControlPeriodFailUS {
			return fmt.Sprintf(
				"control period %.0fus >= fail %.0fus (%d/%d consecutive)",
				controlPeriodUS,
				rules.ControlPeriodFailUS,
				state.failCount,
				failRequired,
			)
		}
		if rules.ControlPeriodWarnUS > 0 && controlPeriodUS >= rules.ControlPeriodWarnUS {
			return fmt.Sprintf(
				"control period %.0fus remains above warn %.0fus after fail",
				controlPeriodUS,
				rules.ControlPeriodWarnUS,
			)
		}
	}
	if state.severity == health.SeverityWarn {
		return fmt.Sprintf(
			"control period %.0fus >= warn %.0fus (%d/%d consecutive)",
			controlPeriodUS,
			rules.ControlPeriodWarnUS,
			state.warnCount,
			warnRequired,
		)
	}
	return ""
}

func positiveOrDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func severityMetric(severity health.Severity) float64 {
	switch severity {
	case health.SeverityWarn:
		return 1
	case health.SeverityFail:
		return 2
	case health.SeverityStale:
		return 3
	default:
		return 0
	}
}

func (e *Evaluator) applyStaleness(status *health.Status, observation health.Observation) bool {
	if observation.StaleAfter <= 0 {
		return false
	}

	age := time.Since(observation.CollectedAt)
	status.Metrics["age.s"] = age.Seconds()
	status.Metrics["stale_after.s"] = observation.StaleAfter.Seconds()
	if age <= observation.StaleAfter {
		return false
	}

	status.Severity = health.SeverityStale
	status.Reason = fmt.Sprintf("last report %.2fs ago > stale_after %.2fs", age.Seconds(), observation.StaleAfter.Seconds())
	if observation.ReportedSeverity != "" && observation.ReportedSeverity != health.SeverityOK {
		status.Reason = fmt.Sprintf("%s; last reported %s: %s", status.Reason, observation.ReportedSeverity, observation.ReportedReason)
	}
	if observation.SourceType == "module" {
		e.resetModuleTiming(observation.SourceID)
	}
	return true
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

	if metric(status.Metrics, "ethercat.criticality_known") > 0 {
		if criticalLost := int(metric(status.Metrics, "ethercat.critical_slaves_lost")); criticalLost > 0 {
			update(health.SeverityFail, fmt.Sprintf("critical lost slaves %d > 0", criticalLost))
		}
		if importantLost := int(metric(status.Metrics, "ethercat.important_slaves_lost")); importantLost > 0 {
			update(health.SeverityFail, fmt.Sprintf("important lost slaves %d > 0", importantLost))
		}
		if optionalLost := int(metric(status.Metrics, "ethercat.optional_slaves_lost")); optionalLost > 0 {
			update(health.SeverityWarn, fmt.Sprintf("optional lost slaves %d > 0", optionalLost))
		}
		if criticalNotOp := int(metric(status.Metrics, "ethercat.critical_slaves_not_op")); criticalNotOp > 0 {
			update(health.SeverityFail, fmt.Sprintf("critical non-operational slaves %d > 0", criticalNotOp))
		}
		if importantNotOp := int(metric(status.Metrics, "ethercat.important_slaves_not_op")); importantNotOp > 0 {
			update(health.SeverityWarn, fmt.Sprintf("important non-operational slaves %d > 0", importantNotOp))
		}
		if optionalNotOp := int(metric(status.Metrics, "ethercat.optional_slaves_not_op")); optionalNotOp > 0 {
			update(health.SeverityWarn, fmt.Sprintf("optional non-operational slaves %d > 0", optionalNotOp))
		}
		if criticalErrors := int(metric(status.Metrics, "ethercat.critical_slave_errors")); criticalErrors > 0 {
			update(health.SeverityFail, fmt.Sprintf("critical slave errors %d > 0", criticalErrors))
		}
		if importantErrors := int(metric(status.Metrics, "ethercat.important_slave_errors")); importantErrors > 0 {
			update(health.SeverityWarn, fmt.Sprintf("important slave errors %d > 0", importantErrors))
		}
		if optionalErrors := int(metric(status.Metrics, "ethercat.optional_slave_errors")); optionalErrors > 0 {
			update(health.SeverityWarn, fmt.Sprintf("optional slave errors %d > 0", optionalErrors))
		}
	} else {
		if slaveErrors := int(metric(status.Metrics, "ethercat.slave_errors")); slaveErrors > 0 {
			update(health.SeverityWarn, fmt.Sprintf("slave errors %d > 0", slaveErrors))
		}
		if slavesLost := int(metric(status.Metrics, "ethercat.slaves_lost")); slavesLost > 0 {
			update(health.SeverityFail, fmt.Sprintf("lost slaves %d > 0", slavesLost))
		}
		if slavesNotOp := int(metric(status.Metrics, "ethercat.slaves_not_op")); slavesNotOp > 0 {
			update(health.SeverityWarn, fmt.Sprintf("non-operational slaves %d > 0", slavesNotOp))
		}
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

func (e *Evaluator) evaluateNetwork(status health.Status) health.Status {
	rules := e.cfg.Network
	var reasons []string

	update := func(next health.Severity, reason string) {
		if health.CompareSeverity(next, status.Severity) > 0 {
			status.Severity = next
		}
		reasons = append(reasons, reason)
	}

	if metric(status.Metrics, "network.require_up") > 0 && metric(status.Metrics, "network.link_up") <= 0 {
		update(health.SeverityFail, "link is down")
	}

	minSpeed := metric(status.Metrics, "network.min_speed_mbps")
	speed := metric(status.Metrics, "network.speed_mbps")
	if minSpeed > 0 && speed > 0 && speed < minSpeed {
		update(health.SeverityWarn, fmt.Sprintf("speed %.0fMbps < min %.0fMbps", speed, minSpeed))
	}

	errorDelta := metric(status.Metrics, "network.rx_errors_delta") + metric(status.Metrics, "network.tx_errors_delta")
	if rules.ErrorDeltaWarn > 0 && errorDelta >= rules.ErrorDeltaWarn {
		update(health.SeverityWarn, fmt.Sprintf("error delta %.0f >= warn %.0f", errorDelta, rules.ErrorDeltaWarn))
	}
	dropDelta := metric(status.Metrics, "network.rx_dropped_delta") + metric(status.Metrics, "network.tx_dropped_delta")
	if rules.DropDeltaWarn > 0 && dropDelta >= rules.DropDeltaWarn {
		update(health.SeverityWarn, fmt.Sprintf("drop delta %.0f >= warn %.0f", dropDelta, rules.DropDeltaWarn))
	}

	status.Reason = strings.Join(reasons, "; ")
	return status
}

func (e *Evaluator) evaluatePower(status health.Status) health.Status {
	rules := e.cfg.Power
	var reasons []string

	update := func(next health.Severity, reason string) {
		if health.CompareSeverity(next, status.Severity) > 0 {
			status.Severity = next
		}
		reasons = append(reasons, reason)
	}

	if metric(status.Metrics, "power.require_present") > 0 && metric(status.Metrics, "power.present") <= 0 {
		update(health.SeverityFail, "power supply not present")
	}
	if metric(status.Metrics, "power.require_online") > 0 && metric(status.Metrics, "power.online") <= 0 {
		update(health.SeverityFail, "power supply offline")
	}

	capacity := metric(status.Metrics, "power.capacity_pct")
	switch {
	case rules.CapacityFailPct > 0 && capacity > 0 && capacity <= rules.CapacityFailPct:
		update(health.SeverityFail, fmt.Sprintf("capacity %.1f%% <= fail %.1f%%", capacity, rules.CapacityFailPct))
	case rules.CapacityWarnPct > 0 && capacity > 0 && capacity <= rules.CapacityWarnPct:
		update(health.SeverityWarn, fmt.Sprintf("capacity %.1f%% <= warn %.1f%%", capacity, rules.CapacityWarnPct))
	}

	tempC := metric(status.Metrics, "power.temp_c")
	switch {
	case rules.TempFailC > 0 && tempC >= rules.TempFailC:
		update(health.SeverityFail, fmt.Sprintf("temperature %.1fC >= fail %.1fC", tempC, rules.TempFailC))
	case rules.TempWarnC > 0 && tempC >= rules.TempWarnC:
		update(health.SeverityWarn, fmt.Sprintf("temperature %.1fC >= warn %.1fC", tempC, rules.TempWarnC))
	}

	switch healthLabel := strings.ToLower(label(status.Labels, "health")); healthLabel {
	case "", "good", "ok":
	case "unknown":
		update(health.SeverityWarn, `health "unknown"`)
	default:
		update(health.SeverityFail, fmt.Sprintf("health %q", healthLabel))
	}

	status.Reason = strings.Join(reasons, "; ")
	return status
}

func (e *Evaluator) evaluateStorage(status health.Status) health.Status {
	rules := e.cfg.Storage
	var reasons []string

	update := func(next health.Severity, reason string) {
		if health.CompareSeverity(next, status.Severity) > 0 {
			status.Severity = next
		}
		reasons = append(reasons, reason)
	}

	if metric(status.Metrics, "storage.require_writable") > 0 && metric(status.Metrics, "storage.readonly") > 0 {
		update(health.SeverityFail, "filesystem is read-only")
	}

	usedPct := metric(status.Metrics, "storage.used_percent")
	switch {
	case rules.UsedPercentFail > 0 && usedPct >= rules.UsedPercentFail:
		update(health.SeverityFail, fmt.Sprintf("used %.1f%% >= fail %.1f%%", usedPct, rules.UsedPercentFail))
	case rules.UsedPercentWarn > 0 && usedPct >= rules.UsedPercentWarn:
		update(health.SeverityWarn, fmt.Sprintf("used %.1f%% >= warn %.1f%%", usedPct, rules.UsedPercentWarn))
	}

	availMB := metric(status.Metrics, "storage.avail_bytes") / (1024 * 1024)
	switch {
	case rules.AvailFailMB > 0 && availMB <= rules.AvailFailMB:
		update(health.SeverityFail, fmt.Sprintf("available %.0fMB <= fail %.0fMB", availMB, rules.AvailFailMB))
	case rules.AvailWarnMB > 0 && availMB <= rules.AvailWarnMB:
		update(health.SeverityWarn, fmt.Sprintf("available %.0fMB <= warn %.0fMB", availMB, rules.AvailWarnMB))
	}

	busyPct := metric(status.Metrics, "storage.busy_percent")
	switch {
	case rules.BusyPercentFail > 0 && busyPct >= rules.BusyPercentFail:
		update(health.SeverityFail, fmt.Sprintf("busy %.1f%% >= fail %.1f%%", busyPct, rules.BusyPercentFail))
	case rules.BusyPercentWarn > 0 && busyPct >= rules.BusyPercentWarn:
		update(health.SeverityWarn, fmt.Sprintf("busy %.1f%% >= warn %.1f%%", busyPct, rules.BusyPercentWarn))
	}

	status.Reason = strings.Join(reasons, "; ")
	return status
}

func (e *Evaluator) evaluateTimeSync(status health.Status) health.Status {
	rules := e.cfg.TimeSync
	var reasons []string

	update := func(next health.Severity, reason string) {
		if health.CompareSeverity(next, status.Severity) > 0 {
			status.Severity = next
		}
		reasons = append(reasons, reason)
	}

	if metric(status.Metrics, "time.require_sync") > 0 && metric(status.Metrics, "time.ntp_synchronized") <= 0 {
		grace := metric(status.Metrics, "time.sync_grace_s")
		unsynchronizedFor := metric(status.Metrics, "time.unsynchronized_for_s")
		switch {
		case grace > 0 && unsynchronizedFor < grace:
			update(health.SeverityWarn, fmt.Sprintf("clock is not synchronized; %.0fs grace remaining", grace-unsynchronizedFor))
		case grace > 0:
			update(health.SeverityFail, fmt.Sprintf("clock is not synchronized for %.0fs >= grace %.0fs", unsynchronizedFor, grace))
		default:
			update(health.SeverityFail, "clock is not synchronized")
		}
	}
	if metric(status.Metrics, "time.require_sync") > 0 && metric(status.Metrics, "time.can_ntp") > 0 && metric(status.Metrics, "time.ntp_enabled") <= 0 {
		update(health.SeverityWarn, "NTP is disabled")
	}
	if metric(status.Metrics, "time.warn_on_local_rtc") > 0 && metric(status.Metrics, "time.local_rtc") > 0 {
		update(health.SeverityWarn, "LocalRTC is enabled")
	}

	rtcDelta := metric(status.Metrics, "time.rtc_delta_s")
	switch {
	case rules.RTCDeltaFailS > 0 && rtcDelta >= rules.RTCDeltaFailS:
		update(health.SeverityFail, fmt.Sprintf("RTC delta %.1fs >= fail %.1fs", rtcDelta, rules.RTCDeltaFailS))
	case rules.RTCDeltaWarnS > 0 && rtcDelta >= rules.RTCDeltaWarnS:
		update(health.SeverityWarn, fmt.Sprintf("RTC delta %.1fs >= warn %.1fs", rtcDelta, rules.RTCDeltaWarnS))
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
