package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"watchdog/internal/actions"
	"watchdog/internal/health"
)

type Manager struct {
	path      string
	cooldowns CooldownConfig
	state     State
}

type State struct {
	SchemaVersion    int              `json:"schema_version"`
	UpdatedAt        time.Time        `json:"updated_at"`
	LastRequestID    string           `json:"last_request_id,omitempty"`
	OverallAction    actions.Kind     `json:"overall_action,omitempty"`
	LastHookAt       HookTimes        `json:"last_hook_at"`
	ActiveComponents []ComponentState `json:"active_components,omitempty"`
}

type HookTimes struct {
	Notify   time.Time `json:"notify,omitempty"`
	Degrade  time.Time `json:"degrade,omitempty"`
	SafeStop time.Time `json:"safe_stop,omitempty"`
	Resolve  time.Time `json:"resolve,omitempty"`
}

type ComponentState struct {
	ComponentID         string          `json:"component_id"`
	ActiveAction        actions.Kind    `json:"active_action"`
	Severity            health.Severity `json:"severity"`
	Reason              string          `json:"reason,omitempty"`
	SourceTypes         []string        `json:"source_types,omitempty"`
	Latched             bool            `json:"latched"`
	FirstActivatedAt    time.Time       `json:"first_activated_at"`
	LastRequestAt       time.Time       `json:"last_request_at"`
	LastRequestID       string          `json:"last_request_id"`
	LastRequestedAction actions.Kind    `json:"last_requested_action,omitempty"`
}

type ApplyResult struct {
	State             State        `json:"state"`
	HookAction        actions.Kind `json:"hook_action,omitempty"`
	ShouldExecuteHook bool         `json:"should_execute_hook"`
	SuppressionReason string       `json:"suppression_reason,omitempty"`
	ChangedComponents []string     `json:"changed_components,omitempty"`
	ClearedComponents []string     `json:"cleared_components,omitempty"`
}

func LoadManager(path string, cooldowns CooldownConfig) (*Manager, error) {
	manager := &Manager{
		path:      path,
		cooldowns: cooldowns,
		state: State{
			SchemaVersion: 1,
		},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			manager.rebuild()
			return manager, nil
		}
		return nil, fmt.Errorf("read supervisor state: %w", err)
	}

	if err := json.Unmarshal(data, &manager.state); err != nil {
		return nil, fmt.Errorf("decode supervisor state: %w", err)
	}
	manager.normalize()
	return manager, nil
}

func (m *Manager) Write() error {
	return writeJSONFile(m.path, m.state)
}

func (m *Manager) Snapshot() State {
	return m.state
}

func (m *Manager) Apply(request actions.Request) (ApplyResult, error) {
	var result ApplyResult
	switch request.Event {
	case actions.EventTransition:
		result = m.applyTransition(request)
	case actions.EventResolved:
		result = m.applyResolve(request)
	default:
		return ApplyResult{}, fmt.Errorf("unsupported event %q", request.Event)
	}

	m.state.LastRequestID = request.RequestID
	m.state.UpdatedAt = request.Timestamp.UTC()
	m.rebuild()

	result.State = m.state
	if err := m.Write(); err != nil {
		return ApplyResult{}, err
	}
	return result, nil
}

func (m *Manager) applyTransition(request actions.Request) ApplyResult {
	components := m.componentMap()
	now := request.Timestamp.UTC()

	changedIDs := make([]string, 0, len(request.Components))
	hasImmediateHook := false
	hasLowerThanLatched := false
	hasReminderCandidate := false

	for _, component := range request.Components {
		current, exists := components[component.ComponentID]
		updated := current
		if !exists {
			updated = ComponentState{
				ComponentID:      component.ComponentID,
				FirstActivatedAt: now,
			}
		}

		updated.LastRequestAt = now
		updated.LastRequestID = request.RequestID
		updated.LastRequestedAction = component.RequestedAction

		switch compareAction(component.RequestedAction, current.ActiveAction) {
		case 1:
			updated.ActiveAction = component.RequestedAction
			updated.Severity = component.Severity
			updated.Reason = component.Reason
			updated.SourceTypes = cloneStrings(component.SourceTypes)
			updated.Latched = isLatched(component.RequestedAction)
			hasImmediateHook = true
			changedIDs = appendUnique(changedIDs, component.ComponentID)
		case 0:
			if !exists {
				updated.ActiveAction = component.RequestedAction
				updated.Severity = component.Severity
				updated.Reason = component.Reason
				updated.SourceTypes = cloneStrings(component.SourceTypes)
				updated.Latched = isLatched(component.RequestedAction)
				hasImmediateHook = true
				changedIDs = appendUnique(changedIDs, component.ComponentID)
			} else {
				hasReminderCandidate = true
				if updated.Severity != component.Severity ||
					updated.Reason != component.Reason ||
					!equalStrings(updated.SourceTypes, component.SourceTypes) {
					updated.Severity = component.Severity
					updated.Reason = component.Reason
					updated.SourceTypes = cloneStrings(component.SourceTypes)
					changedIDs = appendUnique(changedIDs, component.ComponentID)
				}
			}
		case -1:
			hasLowerThanLatched = true
		}

		components[component.ComponentID] = updated
	}

	m.state.ActiveComponents = mapToSortedComponents(components)

	triggerHook := false
	suppressionReason := ""
	if request.RequestedAction != actions.ActionNone {
		switch {
		case hasImmediateHook:
			triggerHook = true
		case hasReminderCandidate && m.cooldownElapsed(request.RequestedAction, now):
			triggerHook = true
		case hasLowerThanLatched:
			suppressionReason = "latched higher action already active"
		case hasReminderCandidate:
			suppressionReason = fmt.Sprintf("%s cooldown active", request.RequestedAction)
		default:
			suppressionReason = "no active component changes"
		}
	}
	if triggerHook {
		m.setLastHookAt(request.RequestedAction, now)
	}

	return ApplyResult{
		HookAction:        request.RequestedAction,
		ShouldExecuteHook: triggerHook,
		SuppressionReason: suppressionReason,
		ChangedComponents: changedIDs,
	}
}

func (m *Manager) applyResolve(request actions.Request) ApplyResult {
	components := m.componentMap()
	resolvedIDs := resolvedComponentIDs(request, components)
	cleared := make([]string, 0, len(resolvedIDs))

	for _, componentID := range resolvedIDs {
		if _, ok := components[componentID]; !ok {
			continue
		}
		delete(components, componentID)
		cleared = append(cleared, componentID)
	}

	m.state.ActiveComponents = mapToSortedComponents(components)

	triggerHook := len(cleared) > 0
	suppressionReason := ""
	if triggerHook {
		m.setLastHookAt(actions.ActionResolve, request.Timestamp.UTC())
	} else {
		suppressionReason = "no active components matched resolve request"
	}

	return ApplyResult{
		HookAction:        actions.ActionResolve,
		ShouldExecuteHook: triggerHook,
		SuppressionReason: suppressionReason,
		ClearedComponents: cleared,
	}
}

func (m *Manager) normalize() {
	if m.state.SchemaVersion == 0 {
		m.state.SchemaVersion = 1
	}
	for i := range m.state.ActiveComponents {
		m.state.ActiveComponents[i].SourceTypes = normalizeStrings(m.state.ActiveComponents[i].SourceTypes)
		m.state.ActiveComponents[i].Latched = isLatched(m.state.ActiveComponents[i].ActiveAction)
	}
	m.rebuild()
}

func (m *Manager) rebuild() {
	m.state.SchemaVersion = 1
	m.state.ActiveComponents = mapToSortedComponents(m.componentMap())
	m.state.OverallAction = overallAction(m.state.ActiveComponents)
}

func (m *Manager) componentMap() map[string]ComponentState {
	out := make(map[string]ComponentState, len(m.state.ActiveComponents))
	for _, component := range m.state.ActiveComponents {
		component.SourceTypes = normalizeStrings(component.SourceTypes)
		out[component.ComponentID] = component
	}
	return out
}

func (m *Manager) cooldownElapsed(action actions.Kind, now time.Time) bool {
	last := m.lastHookAt(action)
	if last.IsZero() {
		return true
	}
	return now.Sub(last) >= m.cooldownFor(action)
}

func (m *Manager) cooldownFor(action actions.Kind) time.Duration {
	switch action {
	case actions.ActionNotify:
		return m.cooldowns.Notify
	case actions.ActionDegrade:
		return m.cooldowns.Degrade
	case actions.ActionSafeStop:
		return m.cooldowns.SafeStop
	case actions.ActionResolve:
		return m.cooldowns.Resolve
	default:
		return 0
	}
}

func (m *Manager) lastHookAt(action actions.Kind) time.Time {
	switch action {
	case actions.ActionNotify:
		return m.state.LastHookAt.Notify
	case actions.ActionDegrade:
		return m.state.LastHookAt.Degrade
	case actions.ActionSafeStop:
		return m.state.LastHookAt.SafeStop
	case actions.ActionResolve:
		return m.state.LastHookAt.Resolve
	default:
		return time.Time{}
	}
}

func (m *Manager) setLastHookAt(action actions.Kind, at time.Time) {
	switch action {
	case actions.ActionNotify:
		m.state.LastHookAt.Notify = at
	case actions.ActionDegrade:
		m.state.LastHookAt.Degrade = at
	case actions.ActionSafeStop:
		m.state.LastHookAt.SafeStop = at
	case actions.ActionResolve:
		m.state.LastHookAt.Resolve = at
	}
}

func resolvedComponentIDs(request actions.Request, active map[string]ComponentState) []string {
	if len(request.Resolved) > 0 {
		return normalizeStrings(request.Resolved)
	}
	if len(request.Components) > 0 {
		ids := make([]string, 0, len(request.Components))
		for _, component := range request.Components {
			ids = append(ids, component.ComponentID)
		}
		return normalizeStrings(ids)
	}
	ids := make([]string, 0, len(active))
	for componentID := range active {
		ids = append(ids, componentID)
	}
	return normalizeStrings(ids)
}

func mapToSortedComponents(components map[string]ComponentState) []ComponentState {
	out := make([]ComponentState, 0, len(components))
	for _, component := range components {
		component.SourceTypes = normalizeStrings(component.SourceTypes)
		out = append(out, component)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if compareAction(out[i].ActiveAction, out[j].ActiveAction) != 0 {
			return compareAction(out[i].ActiveAction, out[j].ActiveAction) > 0
		}
		return out[i].ComponentID < out[j].ComponentID
	})
	return out
}

func overallAction(components []ComponentState) actions.Kind {
	overall := actions.ActionNone
	for _, component := range components {
		if compareAction(component.ActiveAction, overall) > 0 {
			overall = component.ActiveAction
		}
	}
	return overall
}

func compareAction(left, right actions.Kind) int {
	switch {
	case actionRank(left) > actionRank(right):
		return 1
	case actionRank(left) < actionRank(right):
		return -1
	default:
		return 0
	}
}

func actionRank(action actions.Kind) int {
	switch action {
	case actions.ActionNotify:
		return 1
	case actions.ActionDegrade:
		return 2
	case actions.ActionSafeStop:
		return 3
	default:
		return 0
	}
}

func isLatched(action actions.Kind) bool {
	return action == actions.ActionDegrade || action == actions.ActionSafeStop
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func cloneStrings(values []string) []string {
	return append([]string(nil), normalizeStrings(values)...)
}

func equalStrings(left, right []string) bool {
	left = normalizeStrings(left)
	right = normalizeStrings(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func ensureStateDir(path string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create supervisor state dir: %w", err)
	}
	return nil
}
