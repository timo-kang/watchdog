package module

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"watchdog/internal/adapters"
	"watchdog/internal/config"
	"watchdog/internal/health"
)

var (
	_ adapters.Collector = (*Collector)(nil)
	_ adapters.Starter   = (*Collector)(nil)
	_ adapters.Stopper   = (*Collector)(nil)
)

type Collector struct {
	cfg config.ModuleReportSourceConfig

	mu        sync.RWMutex
	reports   map[string]reportState
	conn      *net.UnixConn
	startOnce sync.Once
	startErr  error
}

type reportState struct {
	sourceID    string
	collectedAt time.Time
	severity    health.Severity
	reason      string
	metrics     map[string]float64
	labels      map[string]string
	staleAfter  time.Duration
}

type incomingReport struct {
	SourceID     string             `json:"source_id"`
	Severity     string             `json:"severity"`
	Reason       string             `json:"reason"`
	ObservedAt   time.Time          `json:"observed_at"`
	Metrics      map[string]float64 `json:"metrics"`
	Labels       map[string]string  `json:"labels"`
	StaleAfterMS int64              `json:"stale_after_ms"`
}

func New(cfg config.ModuleReportSourceConfig) *Collector {
	return &Collector{
		cfg:     cfg,
		reports: make(map[string]reportState),
	}
}

func (c *Collector) Name() string {
	return "module_reports"
}

func (c *Collector) Start(ctx context.Context) error {
	c.startOnce.Do(func() {
		c.startErr = c.start(ctx)
	})
	return c.startErr
}

func (c *Collector) start(ctx context.Context) error {
	if c.cfg.SocketPath == "" {
		return fmt.Errorf("socket path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(c.cfg.SocketPath), 0o755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	if err := os.Remove(c.cfg.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: c.cfg.SocketPath, Net: "unixgram"})
	if err != nil {
		return fmt.Errorf("listen on %s: %w", c.cfg.SocketPath, err)
	}
	if err := os.Chmod(c.cfg.SocketPath, 0o660); err != nil {
		_ = conn.Close()
		_ = os.Remove(c.cfg.SocketPath)
		return fmt.Errorf("chmod socket: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	go c.readLoop(ctx, conn)
	return nil
}

func (c *Collector) Stop(context.Context) error {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()

	if conn != nil {
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			return err
		}
	}
	if err := os.Remove(c.cfg.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (c *Collector) Collect(context.Context) ([]health.Observation, error) {
	c.mu.RLock()
	conn := c.conn
	states := make(map[string]reportState, len(c.reports))
	for key, value := range c.reports {
		states[key] = reportState{
			sourceID:    value.sourceID,
			collectedAt: value.collectedAt,
			severity:    value.severity,
			reason:      value.reason,
			metrics:     cloneMetrics(value.metrics),
			labels:      cloneLabels(value.labels),
			staleAfter:  value.staleAfter,
		}
	}
	c.mu.RUnlock()

	if conn == nil {
		return nil, fmt.Errorf("ingest socket is not running")
	}

	ids := make([]string, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	now := time.Now()
	observations := make([]health.Observation, 0, len(ids))
	for _, id := range ids {
		state := states[id]
		metrics := cloneMetrics(state.metrics)
		if metrics == nil {
			metrics = make(map[string]float64)
		}
		metrics["age_s"] = now.Sub(state.collectedAt).Seconds()
		metrics["stale_after_s"] = state.staleAfter.Seconds()

		observations = append(observations, health.Observation{
			SourceID:         state.sourceID,
			SourceType:       "module",
			CollectedAt:      state.collectedAt,
			Metrics:          metrics,
			Labels:           cloneLabels(state.labels),
			ReportedSeverity: state.severity,
			ReportedReason:   state.reason,
			StaleAfter:       state.staleAfter,
		})
	}

	return observations, nil
}

func (c *Collector) readLoop(ctx context.Context, conn *net.UnixConn) {
	buf := make([]byte, c.cfg.MaxMessageBytes)
	for {
		n, _, err := conn.ReadFromUnix(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}

		report, err := decodeReport(buf[:n], c.cfg.DefaultStaleAfter)
		if err != nil {
			continue
		}

		c.mu.Lock()
		c.reports[report.sourceID] = report
		c.mu.Unlock()
	}
}

func decodeReport(data []byte, defaultStaleAfter time.Duration) (reportState, error) {
	var incoming incomingReport
	if err := json.Unmarshal(data, &incoming); err != nil {
		return reportState{}, fmt.Errorf("decode report: %w", err)
	}
	if incoming.SourceID == "" {
		return reportState{}, fmt.Errorf("source_id is required")
	}

	severity, err := health.ParseSeverity(incoming.Severity)
	if err != nil {
		return reportState{}, err
	}

	collectedAt := incoming.ObservedAt
	if collectedAt.IsZero() {
		collectedAt = time.Now()
	}

	staleAfter := defaultStaleAfter
	if incoming.StaleAfterMS > 0 {
		staleAfter = time.Duration(incoming.StaleAfterMS) * time.Millisecond
	}

	return reportState{
		sourceID:    incoming.SourceID,
		collectedAt: collectedAt,
		severity:    severity,
		reason:      incoming.Reason,
		metrics:     cloneMetrics(incoming.Metrics),
		labels:      cloneLabels(incoming.Labels),
		staleAfter:  staleAfter,
	}, nil
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
