package health

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func BuildComponents(statuses []Status) []ComponentStatus {
	byID := make(map[string][]Status)
	for _, status := range statuses {
		byID[status.SourceID] = append(byID[status.SourceID], cloneStatus(status))
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	components := make([]ComponentStatus, 0, len(ids))
	for _, id := range ids {
		group := byID[id]
		sort.SliceStable(group, func(i, j int) bool {
			left := group[i]
			right := group[j]
			if CompareSeverity(left.Severity, right.Severity) != 0 {
				return CompareSeverity(left.Severity, right.Severity) > 0
			}
			if sourceTypeRank(left.SourceType) != sourceTypeRank(right.SourceType) {
				return sourceTypeRank(left.SourceType) < sourceTypeRank(right.SourceType)
			}
			if !left.ObservedAt.Equal(right.ObservedAt) {
				return left.ObservedAt.After(right.ObservedAt)
			}
			return left.SourceType < right.SourceType
		})

		component := ComponentStatus{
			ComponentID: id,
			Severity:    SeverityOK,
			ObservedAt:  latestObservedAt(group),
			Sources:     make([]ComponentSource, 0, len(group)),
		}
		for _, status := range group {
			component.Severity = MaxSeverity(component.Severity, status.Severity)
			component.Sources = append(component.Sources, ComponentSource{
				SourceType: status.SourceType,
				Severity:   status.Severity,
				Reason:     status.Reason,
				ObservedAt: status.ObservedAt,
			})
		}
		component.Reason = componentReason(group)
		components = append(components, component)
	}

	return components
}

func OverallFromComponents(components []ComponentStatus) Severity {
	out := SeverityOK
	for _, component := range components {
		out = MaxSeverity(out, component.Severity)
	}
	return out
}

func cloneStatus(status Status) Status {
	out := status
	if status.Metrics != nil {
		out.Metrics = make(map[string]float64, len(status.Metrics))
		for key, value := range status.Metrics {
			out.Metrics[key] = value
		}
	}
	if status.Labels != nil {
		out.Labels = make(map[string]string, len(status.Labels))
		for key, value := range status.Labels {
			out.Labels[key] = value
		}
	}
	return out
}

func latestObservedAt(statuses []Status) time.Time {
	var out time.Time
	for _, status := range statuses {
		if status.ObservedAt.After(out) {
			out = status.ObservedAt
		}
	}
	return out
}

func componentReason(statuses []Status) string {
	parts := make([]string, 0, len(statuses))
	for _, status := range statuses {
		if status.Severity == SeverityOK {
			continue
		}
		if status.Reason == "" {
			parts = append(parts, fmt.Sprintf("%s %s", status.SourceType, status.Severity))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s: %s", status.SourceType, status.Severity, status.Reason))
	}
	return strings.Join(parts, "; ")
}

func sourceTypeRank(value string) int {
	switch value {
	case "process":
		return 0
	case "module":
		return 1
	case "host":
		return 2
	case "collector":
		return 3
	default:
		return 4
	}
}
