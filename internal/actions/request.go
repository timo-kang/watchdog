package actions

import (
	"fmt"
	"sort"
	"time"

	"watchdog/internal/health"
)

type Kind string

const (
	ActionNone     Kind = ""
	ActionNotify   Kind = "notify"
	ActionDegrade  Kind = "degrade"
	ActionSafeStop Kind = "safe_stop"
	ActionResolve  Kind = "resolve"
)

type Event string

const (
	EventTransition Event = "transition"
	EventResolved   Event = "resolved"
)

type Request struct {
	SchemaVersion   int                `json:"schema_version"`
	RequestID       string             `json:"request_id"`
	Event           Event              `json:"event"`
	Timestamp       time.Time          `json:"timestamp"`
	Hostname        string             `json:"hostname"`
	Overall         health.Severity    `json:"overall"`
	PreviousOverall health.Severity    `json:"previous_overall,omitempty"`
	RequestedAction Kind               `json:"requested_action"`
	Reason          string             `json:"reason,omitempty"`
	IncidentPath    string             `json:"incident_path,omitempty"`
	Components      []ComponentRequest `json:"components,omitempty"`
	Resolved        []string           `json:"resolved_components,omitempty"`
	Errors          []string           `json:"errors,omitempty"`
}

type ComponentRequest struct {
	ComponentID     string          `json:"component_id"`
	Severity        health.Severity `json:"severity"`
	RequestedAction Kind            `json:"requested_action"`
	Reason          string          `json:"reason,omitempty"`
	SourceTypes     []string        `json:"source_types,omitempty"`
}

type derivedState struct {
	Overall    health.Severity
	Action     Kind
	Components []ComponentRequest
}

func BuildRequest(previous *health.Snapshot, next health.Snapshot, incidentPath string, sendResolved bool) (Request, bool) {
	current := deriveState(next)
	var previousState derivedState
	if previous != nil {
		previousState = deriveState(*previous)
	}

	if current.Action == ActionNone {
		if previousState.Action == ActionNone || !sendResolved {
			return Request{}, false
		}
		resolved := diffResolvedComponents(previousState.Components, current.Components)
		return Request{
			SchemaVersion:   1,
			RequestID:       buildRequestID(next.CollectedAt, EventResolved, ActionResolve, resolved),
			Event:           EventResolved,
			Timestamp:       next.CollectedAt,
			Hostname:        next.Hostname,
			Overall:         next.Overall,
			PreviousOverall: previousState.Overall,
			RequestedAction: ActionResolve,
			Reason:          "health returned to ok",
			IncidentPath:    incidentPath,
			Resolved:        resolved,
			Errors:          append([]string(nil), next.Errors...),
		}, true
	}

	if statesEqual(previousState, current) {
		return Request{}, false
	}

	return Request{
		SchemaVersion:   1,
		RequestID:       buildRequestID(next.CollectedAt, EventTransition, current.Action, componentIDs(current.Components)),
		Event:           EventTransition,
		Timestamp:       next.CollectedAt,
		Hostname:        next.Hostname,
		Overall:         next.Overall,
		PreviousOverall: previousState.Overall,
		RequestedAction: current.Action,
		Reason:          summarizeReason(current),
		IncidentPath:    incidentPath,
		Components:      current.Components,
		Errors:          append([]string(nil), next.Errors...),
	}, true
}

func deriveState(snapshot health.Snapshot) derivedState {
	components := make([]ComponentRequest, 0, len(snapshot.Components))
	action := ActionNone

	for _, component := range snapshot.Components {
		componentAction := recommendAction(component, matchingStatuses(snapshot.Statuses, component.ComponentID))
		if componentAction == ActionNone {
			continue
		}

		entry := ComponentRequest{
			ComponentID:     component.ComponentID,
			Severity:        component.Severity,
			RequestedAction: componentAction,
			Reason:          component.Reason,
			SourceTypes:     uniqueSourceTypes(component.Sources),
		}
		components = append(components, entry)
		if compareKinds(componentAction, action) > 0 {
			action = componentAction
		}
	}

	sort.SliceStable(components, func(i, j int) bool {
		if compareKinds(components[i].RequestedAction, components[j].RequestedAction) != 0 {
			return compareKinds(components[i].RequestedAction, components[j].RequestedAction) > 0
		}
		if health.CompareSeverity(components[i].Severity, components[j].Severity) != 0 {
			return health.CompareSeverity(components[i].Severity, components[j].Severity) > 0
		}
		return components[i].ComponentID < components[j].ComponentID
	})

	return derivedState{
		Overall:    snapshot.Overall,
		Action:     action,
		Components: components,
	}
}

func recommendAction(component health.ComponentStatus, statuses []health.Status) Kind {
	if component.Severity == health.SeverityOK {
		return ActionNone
	}
	if hasSourceType(statuses, "ethercat") {
		if anyMetricAbove(statuses, "ethercat.slaves_lost", 0) || isRequiredLinkDown(statuses, "ethercat") {
			return ActionSafeStop
		}
		return ActionDegrade
	}
	if hasSourceType(statuses, "can") {
		switch {
		case anyMetricAbove(statuses, "can.bus_off_count", 0):
			return ActionDegrade
		case isRequiredLinkDown(statuses, "can"):
			return ActionDegrade
		case component.Severity == health.SeverityWarn:
			return ActionNotify
		default:
			return ActionDegrade
		}
	}

	switch component.Severity {
	case health.SeverityWarn:
		return ActionNotify
	case health.SeverityFail, health.SeverityStale:
		return ActionDegrade
	default:
		return ActionNone
	}
}

func matchingStatuses(statuses []health.Status, componentID string) []health.Status {
	var out []health.Status
	for _, status := range statuses {
		if status.SourceID == componentID {
			out = append(out, status)
		}
	}
	return out
}

func hasSourceType(statuses []health.Status, sourceType string) bool {
	for _, status := range statuses {
		if status.SourceType == sourceType {
			return true
		}
	}
	return false
}

func anyMetricAbove(statuses []health.Status, key string, threshold float64) bool {
	for _, status := range statuses {
		if status.Metrics != nil && status.Metrics[key] > threshold {
			return true
		}
	}
	return false
}

func isRequiredLinkDown(statuses []health.Status, prefix string) bool {
	requireKey := prefix + ".require_link"
	if prefix == "can" {
		requireKey = "can.require_up"
	}
	linkKnownKey := prefix + ".link_known"
	linkUpKey := prefix + ".link_up"
	for _, status := range statuses {
		required := metric(status.Metrics, requireKey) > 0
		if !required {
			continue
		}
		if prefix == "ethercat" && metric(status.Metrics, linkKnownKey) <= 0 {
			continue
		}
		if metric(status.Metrics, linkUpKey) <= 0 {
			return true
		}
	}
	return false
}

func uniqueSourceTypes(sources []health.ComponentSource) []string {
	seen := make(map[string]struct{}, len(sources))
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		if _, ok := seen[source.SourceType]; ok {
			continue
		}
		seen[source.SourceType] = struct{}{}
		out = append(out, source.SourceType)
	}
	sort.Strings(out)
	return out
}

func summarizeReason(state derivedState) string {
	if len(state.Components) == 0 {
		return ""
	}
	primary := state.Components[0]
	if len(state.Components) == 1 {
		return fmt.Sprintf("%s requested for %s: %s", primary.RequestedAction, primary.ComponentID, primary.Reason)
	}
	return fmt.Sprintf("%s requested for %s (%d affected components)", primary.RequestedAction, primary.ComponentID, len(state.Components))
}

func diffResolvedComponents(previous, current []ComponentRequest) []string {
	currentIDs := make(map[string]struct{}, len(current))
	for _, component := range current {
		currentIDs[component.ComponentID] = struct{}{}
	}

	var resolved []string
	for _, component := range previous {
		if _, ok := currentIDs[component.ComponentID]; ok {
			continue
		}
		resolved = append(resolved, component.ComponentID)
	}
	sort.Strings(resolved)
	return resolved
}

func componentIDs(components []ComponentRequest) []string {
	out := make([]string, 0, len(components))
	for _, component := range components {
		out = append(out, component.ComponentID)
	}
	sort.Strings(out)
	return out
}

func statesEqual(left, right derivedState) bool {
	if left.Overall != right.Overall || left.Action != right.Action {
		return false
	}
	if len(left.Components) != len(right.Components) {
		return false
	}
	for i := range left.Components {
		l := left.Components[i]
		r := right.Components[i]
		if l.ComponentID != r.ComponentID || l.Severity != r.Severity || l.RequestedAction != r.RequestedAction {
			return false
		}
		if len(l.SourceTypes) != len(r.SourceTypes) {
			return false
		}
		for j := range l.SourceTypes {
			if l.SourceTypes[j] != r.SourceTypes[j] {
				return false
			}
		}
	}
	return true
}

func compareKinds(left, right Kind) int {
	return kindRank(left) - kindRank(right)
}

func kindRank(value Kind) int {
	switch value {
	case ActionResolve:
		return 4
	case ActionSafeStop:
		return 3
	case ActionDegrade:
		return 2
	case ActionNotify:
		return 1
	default:
		return 0
	}
}

func metric(values map[string]float64, key string) float64 {
	if values == nil {
		return 0
	}
	return values[key]
}

func buildRequestID(timestamp time.Time, event Event, action Kind, componentIDs []string) string {
	base := timestamp.UTC().Format("20060102T150405.000000000Z")
	if len(componentIDs) == 0 {
		return fmt.Sprintf("%s-%s-%s", base, event, action)
	}
	return fmt.Sprintf("%s-%s-%s-%s", base, event, action, sanitizeIDFragment(componentIDs[0]))
}

func sanitizeIDFragment(value string) string {
	if value == "" {
		return "none"
	}
	out := make([]rune, 0, len(value))
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z':
			out = append(out, ch)
		case ch >= 'A' && ch <= 'Z':
			out = append(out, ch)
		case ch >= '0' && ch <= '9':
			out = append(out, ch)
		case ch == '-', ch == '_':
			out = append(out, ch)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}
