package health

func EquivalentAlertState(left, right Snapshot) bool {
	if left.Overall != right.Overall {
		return false
	}
	if len(left.Errors) != len(right.Errors) {
		return false
	}
	for i := range left.Errors {
		if left.Errors[i] != right.Errors[i] {
			return false
		}
	}

	leftComponents := activeComponents(left.Components)
	rightComponents := activeComponents(right.Components)
	if len(leftComponents) != len(rightComponents) {
		return false
	}
	for i := range leftComponents {
		leftComponent := leftComponents[i]
		rightComponent := rightComponents[i]
		if leftComponent.ComponentID != rightComponent.ComponentID || leftComponent.Severity != rightComponent.Severity {
			return false
		}
		if len(leftComponent.Sources) != len(rightComponent.Sources) {
			return false
		}
		for j := range leftComponent.Sources {
			leftSource := leftComponent.Sources[j]
			rightSource := rightComponent.Sources[j]
			if leftSource.SourceType != rightSource.SourceType || leftSource.Severity != rightSource.Severity {
				return false
			}
		}
	}
	return true
}

func activeComponents(components []ComponentStatus) []ComponentStatus {
	active := make([]ComponentStatus, 0, len(components))
	for _, component := range components {
		if component.Severity == SeverityOK {
			continue
		}
		active = append(active, component)
	}
	return active
}
