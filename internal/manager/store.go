package manager

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"watchdog/internal/dtc"
	"watchdog/internal/retention"
)

// FileEventSink persists DTC events as a durable JSONL session log: one JSON
// object per line, each append fsynced so events survive a power cut (the
// canonical robot incident). Old session files are bounded by Prune. It is the
// generalized form of the raisin dtc_events.csv / dtc_history.csv store.
type FileEventSink struct {
	mu   sync.Mutex
	dir  string
	file *os.File
	name string
}

// NewFileEventSink creates (or reuses) dir and opens a new timestamp-named
// session file for appending. The timestamp prefix keeps files lexically
// chronological so retention orders them oldest-first.
func NewFileEventSink(dir string, now time.Time) (*FileEventSink, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create event dir: %w", err)
	}
	name := now.UTC().Format("20060102T150405.000000000Z") + ".jsonl"
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open session file: %w", err)
	}
	return &FileEventSink{dir: dir, file: f, name: name}, nil
}

// Record appends one event as a JSON line and fsyncs it.
func (s *FileEventSink) Record(ev dtc.DtcEvent) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	data = append(data, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.file.Write(data); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("fsync event log: %w", err)
	}
	return nil
}

// Prune bounds the session-log directory (rolling history) per the policy. The
// currently-open session file is never removed.
func (s *FileEventSink) Prune(p retention.Policy) (int, error) {
	s.mu.Lock()
	current := s.name
	s.mu.Unlock()
	return retention.Prune(s.dir, func(name string) bool {
		return strings.HasSuffix(name, ".jsonl") && name != current
	}, p)
}

// Close closes the current session file.
func (s *FileEventSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}
