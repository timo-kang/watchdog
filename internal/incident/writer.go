package incident

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"watchdog/internal/atomicwrite"
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
	if err := atomicwrite.WriteDurable(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write incident: %w", err)
	}
	return path, nil
}

func NowUTC() time.Time {
	return time.Now().UTC()
}
