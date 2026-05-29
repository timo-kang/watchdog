package ethercat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"watchdog/internal/adapters"
	"watchdog/internal/config"
	"watchdog/internal/health"
)

var _ adapters.Collector = (*Collector)(nil)

type probeFunc func(ctx context.Context, backend string, master config.EtherCATMasterConfig) (MasterStatus, error)

type Collector struct {
	cfg   config.EtherCATSourceConfig
	probe probeFunc
}

type MasterStatus struct {
	CollectedAt            time.Time
	LinkKnown              bool
	LinkUp                 bool
	MasterState            string
	SlavesSeen             int
	SlaveErrors            int
	WorkingCounter         int
	WorkingCounterExpected int
	Slaves                 []SlaveStatus
	AdditionalInfo         map[string]string
	AdditionalMetrics      map[string]float64
}

type SlaveStatus struct {
	Position       int
	Name           string
	ConfiguredName string
	VendorID       string
	ProductCode    string
	State          string
	ExpectedState  string
	OnlineKnown    bool
	Online         bool
	Lost           bool
	Criticality    string
	Error          string
}

func New(cfg config.EtherCATSourceConfig) *Collector {
	return &Collector{
		cfg:   cfg,
		probe: selectProbe(cfg.Backend),
	}
}

func (c *Collector) Name() string {
	return "ethercat"
}

func (c *Collector) Collect(ctx context.Context) ([]health.Observation, error) {
	observations := make([]health.Observation, 0, len(c.cfg.Masters))
	for _, master := range c.cfg.Masters {
		status, err := c.probe(ctx, c.cfg.Backend, master)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", master.Name, err)
		}
		collectedAt := status.CollectedAt
		if collectedAt.IsZero() {
			collectedAt = time.Now()
		}

		metrics := cloneMetrics(status.AdditionalMetrics)
		if metrics == nil {
			metrics = make(map[string]float64)
		}
		metrics["ethercat.link_known"] = boolMetric(status.LinkKnown)
		metrics["ethercat.link_up"] = boolMetric(status.LinkUp)
		metrics["ethercat.slaves_seen"] = float64(status.SlavesSeen)
		metrics["ethercat.slave_errors"] = float64(status.SlaveErrors)
		metrics["ethercat.expected_slaves"] = float64(master.ExpectedSlaves)
		metrics["ethercat.working_counter"] = float64(status.WorkingCounter)
		metrics["ethercat.working_counter_goal"] = float64(status.WorkingCounterExpected)
		if master.RequireLink {
			metrics["ethercat.require_link"] = 1
		}

		labels := cloneLabels(status.AdditionalInfo)
		if labels == nil {
			labels = make(map[string]string)
		}
		labels["backend"] = c.cfg.Backend
		labels["bus.kind"] = "ethercat"
		labels["master"] = master.Name
		labels["master_state"] = strings.ToLower(status.MasterState)
		labels["expected_state"] = strings.ToLower(master.ExpectedState)

		addSlaveTopology(metrics, labels, master, status)

		observations = append(observations, health.Observation{
			SourceID:    master.SourceID,
			SourceType:  "ethercat",
			CollectedAt: collectedAt,
			Metrics:     metrics,
			Labels:      labels,
		})
	}
	return observations, nil
}

func selectProbe(backend string) probeFunc {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "igh":
		return probeIgHCLI
	case "command-json", "command_json", "cmdjson":
		return probeCommandJSON
	case "soem":
		return probeSOEM
	default:
		return unsupportedProbe
	}
}

func unsupportedProbe(_ context.Context, backend string, _ config.EtherCATMasterConfig) (MasterStatus, error) {
	return MasterStatus{}, fmt.Errorf("EtherCAT backend %q is not supported", backend)
}

func boolMetric(value bool) float64 {
	if value {
		return 1
	}
	return 0
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

type slaveFaultCounts struct {
	configured       int
	seen             int
	lost             map[string]int
	notOp            map[string]int
	errors           map[string]int
	faultedPositions map[string][]string
	faultedNames     map[string][]string
}

func addSlaveTopology(metrics map[string]float64, labels map[string]string, master config.EtherCATMasterConfig, status MasterStatus) {
	if len(master.Slaves) == 0 && len(status.Slaves) == 0 {
		return
	}
	if len(master.Slaves) == 0 && !hasReportedCriticality(status.Slaves) {
		return
	}

	counts := slaveFaultCounts{
		configured:       len(master.Slaves),
		lost:             map[string]int{"critical": 0, "important": 0, "optional": 0},
		notOp:            map[string]int{"critical": 0, "important": 0, "optional": 0},
		errors:           map[string]int{"critical": 0, "important": 0, "optional": 0},
		faultedPositions: map[string][]string{"critical": nil, "important": nil, "optional": nil},
		faultedNames:     map[string][]string{"critical": nil, "important": nil, "optional": nil},
	}

	byPosition := make(map[int]config.EtherCATSlaveConfig, len(master.Slaves))
	byName := make(map[string]config.EtherCATSlaveConfig, len(master.Slaves))
	seenConfigured := make(map[int]bool, len(master.Slaves))
	for _, slave := range master.Slaves {
		byPosition[slave.Position] = slave
		if slave.Name != "" {
			byName[strings.ToLower(slave.Name)] = slave
		}
	}

	expectedState := normalizeALState(master.ExpectedState)
	if expectedState == "" {
		expectedState = "op"
	}

	for _, slave := range status.Slaves {
		counts.seen++
		cfg, matched := byPosition[slave.Position]
		if !matched && slave.Name != "" {
			cfg, matched = byName[strings.ToLower(slave.Name)]
		}
		if matched {
			seenConfigured[cfg.Position] = true
		}
		evaluateSlaveFault(slave, cfg, matched, expectedState, &counts)
	}

	if len(status.Slaves) > 0 {
		for _, cfg := range master.Slaves {
			if seenConfigured[cfg.Position] {
				continue
			}
			evaluateSlaveFault(SlaveStatus{
				Position:    cfg.Position,
				Name:        cfg.Name,
				Lost:        true,
				Criticality: cfg.Criticality,
			}, cfg, true, expectedState, &counts)
		}
	}

	metrics["ethercat.criticality_known"] = 1
	metrics["ethercat.slaves_configured"] = float64(counts.configured)
	metrics["ethercat.slaves_reported"] = float64(counts.seen)
	for _, criticality := range []string{"critical", "important", "optional"} {
		metrics["ethercat."+criticality+"_slaves_lost"] = float64(counts.lost[criticality])
		metrics["ethercat."+criticality+"_slaves_not_op"] = float64(counts.notOp[criticality])
		metrics["ethercat."+criticality+"_slave_errors"] = float64(counts.errors[criticality])
		if len(counts.faultedPositions[criticality]) > 0 {
			labels["faulted_"+criticality+"_slave_positions"] = strings.Join(counts.faultedPositions[criticality], ",")
		}
		if len(counts.faultedNames[criticality]) > 0 {
			labels["faulted_"+criticality+"_slave_names"] = strings.Join(counts.faultedNames[criticality], ",")
		}
	}
}

func hasReportedCriticality(slaves []SlaveStatus) bool {
	for _, slave := range slaves {
		if normalizeCriticality(slave.Criticality) != "" {
			return true
		}
	}
	return false
}

func evaluateSlaveFault(slave SlaveStatus, cfg config.EtherCATSlaveConfig, matched bool, masterExpectedState string, counts *slaveFaultCounts) {
	criticality := normalizeCriticality(slave.Criticality)
	if criticality == "" && matched {
		criticality = normalizeCriticality(cfg.Criticality)
	}
	if criticality == "" {
		criticality = "important"
	}

	expectedState := normalizeALState(slave.ExpectedState)
	if expectedState == "" && matched {
		expectedState = normalizeALState(cfg.ExpectedState)
	}
	if expectedState == "" {
		expectedState = masterExpectedState
	}
	if expectedState == "" {
		expectedState = "op"
	}

	state := normalizeALState(slave.State)
	lost := slave.Lost || (slave.OnlineKnown && !slave.Online)
	hasError := strings.TrimSpace(slave.Error) != ""
	notOp := !lost && state != "" && state != expectedState

	if lost {
		counts.lost[criticality]++
	}
	if notOp {
		counts.notOp[criticality]++
	}
	if hasError {
		counts.errors[criticality]++
	}
	if lost || notOp || hasError {
		if slave.Position >= 0 {
			counts.faultedPositions[criticality] = append(counts.faultedPositions[criticality], fmt.Sprintf("%d", slave.Position))
		}
		name := firstNonEmpty(slave.ConfiguredName, slave.Name, cfg.Name)
		if name != "" {
			counts.faultedNames[criticality] = append(counts.faultedNames[criticality], name)
		}
	}
}

func normalizeCriticality(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical", "important", "optional":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
