package logagent

import (
	"context"
	"fmt"
	"time"

	"watchdog/internal/rawlog"
)

type Config struct {
	SegmentDir        string
	ManifestDir       string
	SourceID          string
	SourceType        string
	DataType          string
	Format            string
	HealthSourceID    string
	SegmentDuration   time.Duration
	SampleInterval    time.Duration
	SamplesPerSegment int
	MaxSegments       int
	StaleAfter        time.Duration
	Labels            map[string]string
}

type Agent struct {
	cfg          Config
	writer       *rawlog.SegmentWriter
	reporter     Reporter
	nextSequence uint64
	totalBytes   int64
	totalDropped int64
	totalErrors  int64
}

type SyntheticSample struct {
	SchemaVersion int                `json:"schema_version"`
	SourceID      string             `json:"source_id"`
	DataType      string             `json:"data_type"`
	Sequence      uint64             `json:"sequence"`
	ObservedAt    time.Time          `json:"observed_at"`
	Values        map[string]float64 `json:"values"`
	Labels        map[string]string  `json:"labels,omitempty"`
}

func New(cfg Config, reporter Reporter) (*Agent, error) {
	cfg = normalizeConfig(cfg)
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	writer, err := rawlog.NewSegmentWriter(rawlog.SegmentWriterConfig{
		SegmentDir:  cfg.SegmentDir,
		ManifestDir: cfg.ManifestDir,
		SourceID:    cfg.SourceID,
		SourceType:  cfg.SourceType,
		DataType:    cfg.DataType,
		Format:      cfg.Format,
		Clock: rawlog.ClockInfo{
			TimeBase:     "system",
			Synchronized: true,
		},
		Labels: cfg.Labels,
	})
	if err != nil {
		return nil, err
	}
	return &Agent{
		cfg:      cfg,
		writer:   writer,
		reporter: reporter,
	}, nil
}

func (a *Agent) Run(ctx context.Context) error {
	segmentsWritten := 0
	for {
		if a.cfg.MaxSegments > 0 && segmentsWritten >= a.cfg.MaxSegments {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if err := a.writeSegment(ctx); err != nil {
			a.totalErrors++
			_ = a.report(ErrorHealthReport(a.cfg.HealthSourceID, err.Error(), time.Now(), a.cfg.StaleAfter, a.totalErrors))
			if a.cfg.MaxSegments > 0 {
				return err
			}
		} else {
			segmentsWritten++
		}
	}
}

func (a *Agent) writeSegment(ctx context.Context) error {
	startedAt := time.Now().UTC()
	handle, err := a.writer.Open(startedAt)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = handle.Abort()
		}
	}()

	if a.cfg.SamplesPerSegment > 0 {
	sampleLoop:
		for i := 0; i < a.cfg.SamplesPerSegment; i++ {
			if err := a.writeSample(handle); err != nil {
				return err
			}
			if i+1 < a.cfg.SamplesPerSegment && !sleep(ctx, a.cfg.SampleInterval) {
				break sampleLoop
			}
		}
	} else {
		deadline := startedAt.Add(a.cfg.SegmentDuration)
		for time.Now().Before(deadline) {
			if err := a.writeSample(handle); err != nil {
				return err
			}
			if !sleep(ctx, a.cfg.SampleInterval) {
				break
			}
		}
	}

	manifest, manifestPath, err := handle.Close(time.Now().UTC())
	closed = true
	if err != nil {
		return err
	}
	a.totalBytes += manifest.Bytes
	a.totalDropped += manifest.DroppedSamples
	_ = a.report(SegmentHealthReport(
		a.cfg.HealthSourceID,
		manifest,
		manifestPath,
		a.cfg.StaleAfter,
		a.totalBytes,
		a.totalDropped,
		a.totalErrors,
	))
	return nil
}

func (a *Agent) writeSample(handle *rawlog.SegmentHandle) error {
	a.nextSequence++
	now := time.Now().UTC()
	return handle.WriteJSON(SyntheticSample{
		SchemaVersion: 1,
		SourceID:      a.cfg.SourceID,
		DataType:      a.cfg.DataType,
		Sequence:      a.nextSequence,
		ObservedAt:    now,
		Values: map[string]float64{
			"sequence":         float64(a.nextSequence),
			"sample_time_unix": float64(now.UnixNano()) / 1e9,
		},
		Labels: cloneLabels(a.cfg.Labels),
	})
}

func (a *Agent) report(report HealthReport) error {
	if a.reporter == nil {
		return nil
	}
	return a.reporter.Send(report)
}

func normalizeConfig(cfg Config) Config {
	if cfg.SourceType == "" {
		cfg.SourceType = "sensor_raw"
	}
	if cfg.Format == "" {
		cfg.Format = "jsonl"
	}
	if cfg.HealthSourceID == "" {
		cfg.HealthSourceID = DefaultHealthSourceID(cfg.SourceID)
	}
	if cfg.SegmentDuration == 0 {
		cfg.SegmentDuration = 5 * time.Second
	}
	if cfg.SampleInterval == 0 && cfg.SamplesPerSegment == 0 {
		cfg.SampleInterval = 100 * time.Millisecond
	}
	if cfg.StaleAfter == 0 {
		cfg.StaleAfter = 3 * time.Second
	}
	cfg.Labels = cloneLabels(cfg.Labels)
	return cfg
}

func validateConfig(cfg Config) error {
	if cfg.SourceID == "" {
		return fmt.Errorf("source_id must not be empty")
	}
	if cfg.DataType == "" {
		return fmt.Errorf("data_type must not be empty")
	}
	if cfg.SegmentDir == "" {
		return fmt.Errorf("segment_dir must not be empty")
	}
	if cfg.ManifestDir == "" {
		return fmt.Errorf("manifest_dir must not be empty")
	}
	if cfg.SegmentDuration <= 0 {
		return fmt.Errorf("segment_duration must be positive")
	}
	if cfg.SampleInterval < 0 {
		return fmt.Errorf("sample_interval must be >= 0")
	}
	if cfg.SamplesPerSegment < 0 {
		return fmt.Errorf("samples_per_segment must be >= 0")
	}
	if cfg.MaxSegments < 0 {
		return fmt.Errorf("max_segments must be >= 0")
	}
	if cfg.StaleAfter <= 0 {
		return fmt.Errorf("stale_after must be positive")
	}
	return nil
}

func sleep(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
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
