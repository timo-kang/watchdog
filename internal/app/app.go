package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"watchdog/internal/actions"
	"watchdog/internal/adapters"
	"watchdog/internal/health"
	"watchdog/internal/incident"
	"watchdog/internal/rules"
)

type App struct {
	logger       *log.Logger
	interval     time.Duration
	collectors   []adapters.Collector
	evaluator    *rules.Evaluator
	incident     *incident.Writer
	actionSink   actions.Sink
	observer     Observer
	lastSnapshot *health.Snapshot
	hostname     string
	started      bool
}

type Observer interface {
	ObserveCollectorResult(name string, duration time.Duration, err error)
	ObserveSnapshot(snapshot health.Snapshot)
	ObserveIncidentWrite(written bool, err error)
	ObserveActionSink(err error)
}

func New(
	logger *log.Logger,
	interval time.Duration,
	collectors []adapters.Collector,
	evaluator *rules.Evaluator,
	incidentWriter *incident.Writer,
	actionSink actions.Sink,
	observer Observer,
) *App {
	hostname, _ := os.Hostname()
	return &App{
		logger:     logger,
		interval:   interval,
		collectors: collectors,
		evaluator:  evaluator,
		incident:   incidentWriter,
		actionSink: actionSink,
		observer:   observer,
		hostname:   hostname,
	}
}

func (a *App) Run(ctx context.Context) error {
	if err := a.startCollectors(ctx); err != nil {
		return err
	}
	defer a.stopCollectors(context.Background())

	if err := a.tick(ctx); err != nil {
		a.logger.Printf("initial tick failed: %v", err)
	}

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := a.tick(ctx); err != nil {
				a.logger.Printf("tick failed: %v", err)
			}
		}
	}
}

func (a *App) startCollectors(ctx context.Context) error {
	if a.started {
		return nil
	}
	for _, collector := range a.collectors {
		starter, ok := collector.(adapters.Starter)
		if !ok {
			continue
		}
		if err := starter.Start(ctx); err != nil {
			return fmt.Errorf("start %s: %w", collector.Name(), err)
		}
	}
	a.started = true
	return nil
}

func (a *App) stopCollectors(ctx context.Context) {
	for _, collector := range a.collectors {
		stopper, ok := collector.(adapters.Stopper)
		if !ok {
			continue
		}
		if err := stopper.Stop(ctx); err != nil {
			a.logger.Printf("stop %s: %v", collector.Name(), err)
		}
	}
}

func (a *App) tick(ctx context.Context) error {
	var statuses []health.Status
	var errors []string

	for _, collector := range a.collectors {
		startedAt := time.Now()
		observations, err := collector.Collect(ctx)
		duration := time.Since(startedAt)
		if a.observer != nil {
			a.observer.ObserveCollectorResult(collector.Name(), duration, err)
		}
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", collector.Name(), err))
			statuses = append(statuses, health.Status{
				SourceID:   collector.Name(),
				SourceType: "collector",
				Severity:   health.SeverityStale,
				Reason:     err.Error(),
				ObservedAt: time.Now(),
			})
			continue
		}

		for _, observation := range observations {
			statuses = append(statuses, a.evaluator.Evaluate(observation))
		}
	}

	snapshot := health.Snapshot{
		Hostname:    a.hostname,
		CollectedAt: time.Now(),
		Statuses:    statuses,
		Components:  health.BuildComponents(statuses),
		Errors:      errors,
	}
	snapshot.Overall = health.OverallFromComponents(snapshot.Components)
	if a.observer != nil {
		a.observer.ObserveSnapshot(snapshot)
	}

	incidentPath, err := a.incident.MaybeWrite(a.lastSnapshot, snapshot)
	if a.observer != nil {
		a.observer.ObserveIncidentWrite(incidentPath != "", err)
	}
	if err != nil {
		a.logger.Printf("write incident: %v", err)
	}

	if err := a.actionSink.HandleTransition(ctx, a.lastSnapshot, snapshot, incidentPath); err != nil {
		if a.observer != nil {
			a.observer.ObserveActionSink(err)
		}
		a.logger.Printf("action sink: %v", err)
	} else if a.observer != nil {
		a.observer.ObserveActionSink(nil)
	}

	a.lastSnapshot = &snapshot
	return nil
}
