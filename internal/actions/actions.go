package actions

import (
	"context"
	"log"

	"watchdog/internal/health"
)

type Sink interface {
	HandleTransition(ctx context.Context, previous *health.Snapshot, next health.Snapshot, incidentPath string) error
}

type TransitionLogger struct {
	logger             *log.Logger
	logTransitionsOnly bool
}

func NewTransitionLogger(logger *log.Logger, logTransitionsOnly bool) *TransitionLogger {
	return &TransitionLogger{
		logger:             logger,
		logTransitionsOnly: logTransitionsOnly,
	}
}

func (t *TransitionLogger) HandleTransition(_ context.Context, previous *health.Snapshot, next health.Snapshot, incidentPath string) error {
	changed := previous == nil || previous.Overall != next.Overall
	if t.logTransitionsOnly && !changed {
		return nil
	}

	t.logger.Printf("health overall=%s components=%d statuses=%d incident=%s", next.Overall, len(next.Components), len(next.Statuses), incidentPath)
	for _, component := range next.Components {
		if component.Severity == health.SeverityOK && t.logTransitionsOnly {
			continue
		}
		t.logger.Printf("component=%s severity=%s reason=%q sources=%s", component.ComponentID, component.Severity, component.Reason, formatSources(component.Sources))
	}
	return nil
}

func formatSources(sources []health.ComponentSource) string {
	if len(sources) == 0 {
		return ""
	}
	out := ""
	for i, source := range sources {
		if i > 0 {
			out += ","
		}
		out += string(source.SourceType) + "=" + string(source.Severity)
	}
	return out
}
