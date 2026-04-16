package adapters

import (
	"context"

	"watchdog/internal/health"
)

type Collector interface {
	Name() string
	Collect(ctx context.Context) ([]health.Observation, error)
}

type Starter interface {
	Start(ctx context.Context) error
}

type Stopper interface {
	Stop(ctx context.Context) error
}
