package actions

import (
	"context"
	"errors"

	"watchdog/internal/health"
)

type MultiSink struct {
	sinks []Sink
}

func NewMultiSink(sinks ...Sink) *MultiSink {
	filtered := make([]Sink, 0, len(sinks))
	for _, sink := range sinks {
		if sink == nil {
			continue
		}
		filtered = append(filtered, sink)
	}
	return &MultiSink{sinks: filtered}
}

func (m *MultiSink) HandleTransition(ctx context.Context, previous *health.Snapshot, next health.Snapshot, incidentPath string) error {
	var errs []error
	for _, sink := range m.sinks {
		if err := sink.HandleTransition(ctx, previous, next, incidentPath); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
