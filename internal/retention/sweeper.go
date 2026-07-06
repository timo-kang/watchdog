package retention

import (
	"context"
	"log"
	"time"
)

type Target struct {
	Dir    string
	Match  func(name string) bool
	Policy Policy
}

type Sweeper struct {
	logger   *log.Logger
	interval time.Duration
	targets  []Target
}

func NewSweeper(logger *log.Logger, interval time.Duration, targets ...Target) *Sweeper {
	return &Sweeper{logger: logger, interval: interval, targets: targets}
}

// SweepOnce prunes every target once. Per-target errors are logged, never fatal.
func (s *Sweeper) SweepOnce() {
	for _, t := range s.targets {
		removed, err := Prune(t.Dir, t.Match, t.Policy)
		if err != nil && s.logger != nil {
			s.logger.Printf("retention: prune %s error: %v", t.Dir, err)
		}
		if removed > 0 && s.logger != nil {
			s.logger.Printf("retention: pruned %d files from %s", removed, t.Dir)
		}
	}
}

// Run prunes immediately, then every interval, until ctx is cancelled.
// If interval <= 0, it prunes once and returns (test/one-shot mode).
func (s *Sweeper) Run(ctx context.Context) {
	s.SweepOnce()
	if s.interval <= 0 {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SweepOnce()
		}
	}
}
