package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"

	"watchdog/internal/health"
)

type UnixDatagramSink struct {
	socketPath      string
	sendResolved    bool
	spoolDir        string
	replayBatchSize int
}

func NewUnixDatagramSink(socketPath string, sendResolved bool, spoolDir string, replayBatchSize int) *UnixDatagramSink {
	return &UnixDatagramSink{
		socketPath:      socketPath,
		sendResolved:    sendResolved,
		spoolDir:        spoolDir,
		replayBatchSize: replayBatchSize,
	}
}

func (s *UnixDatagramSink) HandleTransition(_ context.Context, previous *health.Snapshot, next health.Snapshot, incidentPath string) error {
	request, ok := BuildRequest(previous, next, incidentPath, s.sendResolved)
	if !ok {
		return nil
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal action request: %w", err)
	}

	queued, err := s.hasQueuedRequests()
	if err != nil {
		return err
	}
	if queued {
		if err := s.enqueue(request, payload); err != nil {
			return err
		}
		return s.flushQueue()
	}

	if err := s.send(payload); err != nil {
		if spoolErr := s.enqueue(request, payload); spoolErr != nil {
			return fmt.Errorf("send action request: %w; spool action request: %v", err, spoolErr)
		}
		return nil
	}
	return nil
}

func (s *UnixDatagramSink) send(payload []byte) error {
	addr := &net.UnixAddr{Name: s.socketPath, Net: "unixgram"}
	conn, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		return fmt.Errorf("dial action socket: %w", err)
	}
	defer conn.Close()

	if _, err := conn.Write(payload); err != nil {
		return fmt.Errorf("write action request: %w", err)
	}
	return nil
}

func (s *UnixDatagramSink) enqueue(request Request, payload []byte) error {
	if err := os.MkdirAll(s.spoolDir, 0o755); err != nil {
		return fmt.Errorf("create action spool dir: %w", err)
	}
	name := fmt.Sprintf("%s.json", request.RequestID)
	path := filepath.Join(s.spoolDir, name)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write spooled action request: %w", err)
	}
	return nil
}

func (s *UnixDatagramSink) hasQueuedRequests() (bool, error) {
	entries, err := s.queueEntries()
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

func (s *UnixDatagramSink) flushQueue() error {
	entries, err := s.queueEntries()
	if err != nil {
		return err
	}
	limit := len(entries)
	if s.replayBatchSize > 0 && limit > s.replayBatchSize {
		limit = s.replayBatchSize
	}
	for i := 0; i < limit; i++ {
		path := filepath.Join(s.spoolDir, entries[i].Name())
		payload, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read spooled action request: %w", err)
		}
		if err := s.send(payload); err != nil {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove spooled action request: %w", err)
		}
	}
	return nil
}

func (s *UnixDatagramSink) queueEntries() ([]os.DirEntry, error) {
	entries, err := os.ReadDir(s.spoolDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read action spool dir: %w", err)
	}
	filtered := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		filtered = append(filtered, entry)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Name() < filtered[j].Name()
	})
	return filtered, nil
}
