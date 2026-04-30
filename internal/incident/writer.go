package incident

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"watchdog/internal/health"
)

type Writer struct {
	dir             string
	transitionsOnly bool
}

func New(dir string, transitionsOnly bool) *Writer {
	return &Writer{dir: dir, transitionsOnly: transitionsOnly}
}

func (w *Writer) MaybeWrite(previous *health.Snapshot, next health.Snapshot) (string, error) {
	if next.Overall == health.SeverityOK {
		return "", nil
	}
	if w.transitionsOnly && previous != nil && health.EquivalentAlertState(*previous, next) {
		return "", nil
	}

	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return "", fmt.Errorf("create incident dir: %w", err)
	}

	filename := fmt.Sprintf("%s_%s.json", next.CollectedAt.UTC().Format("20060102T150405Z"), next.Overall)
	path := filepath.Join(w.dir, filename)

	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal incident: %w", err)
	}

	temp, err := os.CreateTemp(w.dir, ".incident-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp incident: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("write temp incident: %w", err)
	}
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("chmod temp incident: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close temp incident: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return "", fmt.Errorf("rename incident: %w", err)
	}
	return path, nil
}

func NowUTC() time.Time {
	return time.Now().UTC()
}
