package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type report struct {
	SourceID     string             `json:"source_id"`
	Severity     string             `json:"severity"`
	Reason       string             `json:"reason"`
	StaleAfterMS int64              `json:"stale_after_ms,omitempty"`
	ObservedAt   string             `json:"observed_at,omitempty"`
	Metrics      map[string]float64 `json:"metrics,omitempty"`
	Labels       map[string]string  `json:"labels,omitempty"`
}

func main() {
	socketPath := flag.String("socket", "/run/watchdog/module.sock", "unix datagram socket path")
	sourceID := flag.String("source-id", "planner", "module source_id")
	severity := flag.String("severity", "warn", "reported severity")
	reason := flag.String("reason", "deadline miss", "reported reason")
	interval := flag.Duration("interval", time.Second, "heartbeat interval")
	staleAfter := flag.Duration("stale-after", 1500*time.Millisecond, "stale_after duration")
	count := flag.Int("count", 5, "number of heartbeats to send; 0 means forever")
	waitForSocketTimeout := flag.Duration("wait-for-socket", 20*time.Second, "how long to wait for the socket to appear")
	includeObservedAt := flag.Bool("include-observed-at", false, "include observed_at in payload")
	labelFlags := multiKVFlag{}
	metricFlags := multiMetricFlag{}
	flag.Var(&labelFlags, "label", "label in key=value form; repeatable")
	flag.Var(&metricFlags, "metric", "metric in key=value form; repeatable")
	flag.Parse()

	if *interval <= 0 {
		log.Fatalf("interval must be positive")
	}
	if *staleAfter <= 0 {
		log.Fatalf("stale-after must be positive")
	}
	if *count < 0 {
		log.Fatalf("count must be >= 0")
	}

	if _, ok := metricFlags["deadline_miss_ms"]; !ok && *severity == "warn" {
		metricFlags["deadline_miss_ms"] = 18.5
	}
	if _, ok := labelFlags["process"]; !ok {
		labelFlags["process"] = *sourceID + "_main"
	}

	if err := waitForSocket(*socketPath, *waitForSocketTimeout); err != nil {
		log.Fatalf("wait for socket: %v", err)
	}

	conn, err := net.Dial("unixgram", *socketPath)
	if err != nil {
		log.Fatalf("dial socket: %v", err)
	}
	defer conn.Close()

	send := func(seq int) error {
		now := time.Now().UTC()
		payload := report{
			SourceID:     *sourceID,
			Severity:     *severity,
			Reason:       *reason,
			StaleAfterMS: staleAfter.Milliseconds(),
			Metrics:      metricFlags,
			Labels:       labelFlags,
		}
		if *includeObservedAt {
			payload.ObservedAt = now.Format(time.RFC3339Nano)
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := conn.Write(raw); err != nil {
			return err
		}
		log.Printf("sent heartbeat %d source_id=%s severity=%s reason=%q", seq, payload.SourceID, payload.Severity, payload.Reason)
		return nil
	}

	if *count == 0 {
		seq := 1
		for {
			if err := send(seq); err != nil {
				log.Fatalf("send heartbeat: %v", err)
			}
			seq++
			time.Sleep(*interval)
		}
	}

	for seq := 1; seq <= *count; seq++ {
		if err := send(seq); err != nil {
			log.Fatalf("send heartbeat: %v", err)
		}
		if seq < *count {
			time.Sleep(*interval)
		}
	}
	log.Printf("completed %d heartbeat(s); exiting to let watchdog detect staleness", *count)
}

func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		info, err := os.Stat(path)
		if err == nil && (info.Mode()&os.ModeSocket) != 0 {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return err
			}
			return fmt.Errorf("%s exists but is not a socket", path)
		}
		time.Sleep(200 * time.Millisecond)
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

type multiMetricFlag map[string]float64

func (m *multiMetricFlag) String() string {
	if m == nil {
		return ""
	}
	parts := make([]string, 0, len(*m))
	for key, value := range *m {
		parts = append(parts, fmt.Sprintf("%s=%g", key, value))
	}
	return strings.Join(parts, ",")
}

func (m *multiMetricFlag) Set(value string) error {
	key, raw, ok := strings.Cut(value, "=")
	if !ok || strings.TrimSpace(key) == "" {
		return fmt.Errorf("expected key=value, got %q", value)
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return fmt.Errorf("parse %q as float: %w", raw, err)
	}
	if *m == nil {
		*m = make(map[string]float64)
	}
	(*m)[strings.TrimSpace(key)] = parsed
	return nil
}
