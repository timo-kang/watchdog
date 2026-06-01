package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"watchdog/internal/logagent"
)

func main() {
	cfg := logagent.Config{}
	labels := multiKVFlag{}
	moduleSocket := flag.String("module-socket", "/run/watchdog/module.sock", "watchdog module report unix datagram socket path; empty disables health reports")
	flag.StringVar(&cfg.SegmentDir, "segment-dir", "/var/lib/watchdog/logs/segments", "raw segment output directory")
	flag.StringVar(&cfg.ManifestDir, "manifest-dir", "/var/lib/watchdog/logs/manifests", "raw segment manifest directory")
	flag.StringVar(&cfg.SourceID, "source-id", "demo.imu", "raw data source_id")
	flag.StringVar(&cfg.SourceType, "source-type", "sensor_raw", "raw data source_type in manifests")
	flag.StringVar(&cfg.DataType, "data-type", "imu", "raw data type")
	flag.StringVar(&cfg.Format, "format", "jsonl", "segment file format")
	flag.StringVar(&cfg.HealthSourceID, "health-source-id", "", "health source_id reported to watchdog; default derives from source-id")
	flag.DurationVar(&cfg.SegmentDuration, "segment-duration", 5*time.Second, "segment rotation duration")
	flag.DurationVar(&cfg.SampleInterval, "sample-interval", 100*time.Millisecond, "synthetic sample interval")
	flag.IntVar(&cfg.SamplesPerSegment, "samples-per-segment", 0, "fixed samples per segment; 0 uses segment-duration")
	flag.IntVar(&cfg.MaxSegments, "segments", 0, "number of segments to write; 0 means forever")
	flag.DurationVar(&cfg.StaleAfter, "stale-after", 3*time.Second, "watchdog stale_after for log-agent health reports")
	flag.Var(&labels, "label", "manifest/sample label in key=value form; repeatable")
	flag.Parse()
	cfg.Labels = labels

	logger := log.New(os.Stdout, "watchdog-log-agent ", log.LstdFlags|log.Lmicroseconds)
	agent, err := logagent.New(cfg, logagent.ModuleReporter{SocketPath: *moduleSocket})
	if err != nil {
		logger.Fatalf("configure: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := agent.Run(ctx); err != nil {
		logger.Fatalf("run: %v", err)
	}
}

type multiKVFlag map[string]string

func (m *multiKVFlag) String() string {
	if m == nil {
		return ""
	}
	parts := make([]string, 0, len(*m))
	for key, value := range *m {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, ",")
}

func (m *multiKVFlag) Set(value string) error {
	key, raw, ok := strings.Cut(value, "=")
	if !ok || strings.TrimSpace(key) == "" {
		return fmt.Errorf("expected key=value, got %q", value)
	}
	if *m == nil {
		*m = make(map[string]string)
	}
	(*m)[strings.TrimSpace(key)] = strings.TrimSpace(raw)
	return nil
}
