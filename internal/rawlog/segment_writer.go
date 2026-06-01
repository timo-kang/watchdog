package rawlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type SegmentWriterConfig struct {
	SegmentDir  string
	ManifestDir string
	SourceID    string
	SourceType  string
	DataType    string
	Format      string
	Clock       ClockInfo
	Labels      map[string]string
}

type SegmentWriter struct {
	cfg SegmentWriterConfig
}

type SegmentHandle struct {
	cfg          SegmentWriterConfig
	segmentID    string
	finalPath    string
	manifestPath string
	tempPath     string
	file         *os.File
	startedAt    time.Time
	sampleCount  int64
	dropped      int64
	bytes        int64
	closed       bool
}

func NewSegmentWriter(cfg SegmentWriterConfig) (*SegmentWriter, error) {
	if strings.TrimSpace(cfg.SegmentDir) == "" {
		return nil, fmt.Errorf("segment_dir must not be empty")
	}
	if strings.TrimSpace(cfg.ManifestDir) == "" {
		return nil, fmt.Errorf("manifest_dir must not be empty")
	}
	if strings.TrimSpace(cfg.SourceID) == "" {
		return nil, fmt.Errorf("source_id must not be empty")
	}
	if strings.TrimSpace(cfg.DataType) == "" {
		return nil, fmt.Errorf("data_type must not be empty")
	}
	if strings.TrimSpace(cfg.SourceType) == "" {
		cfg.SourceType = "sensor_raw"
	}
	if strings.TrimSpace(cfg.Format) == "" {
		cfg.Format = "jsonl"
	}
	cfg.Labels = cloneStringMap(cfg.Labels)
	return &SegmentWriter{cfg: cfg}, nil
}

func (w *SegmentWriter) Open(startedAt time.Time) (*SegmentHandle, error) {
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	startedAt = startedAt.UTC()
	segmentID := segmentID(w.cfg.SourceID, startedAt)
	segmentDir := filepath.Join(w.cfg.SegmentDir, startedAt.Format("2006-01-02"))
	if err := os.MkdirAll(segmentDir, 0o755); err != nil {
		return nil, fmt.Errorf("create segment dir: %w", err)
	}
	if err := os.MkdirAll(w.cfg.ManifestDir, 0o755); err != nil {
		return nil, fmt.Errorf("create manifest dir: %w", err)
	}

	finalPath := filepath.Join(segmentDir, segmentID+"."+safeFileToken(w.cfg.Format))
	temp, err := os.CreateTemp(segmentDir, "."+segmentID+".*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temp segment: %w", err)
	}

	return &SegmentHandle{
		cfg:          w.cfg,
		segmentID:    segmentID,
		finalPath:    finalPath,
		manifestPath: filepath.Join(w.cfg.ManifestDir, segmentID+".json"),
		tempPath:     temp.Name(),
		file:         temp,
		startedAt:    startedAt,
	}, nil
}

func (h *SegmentHandle) WriteJSON(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal sample: %w", err)
	}
	return h.WriteLine(data)
}

func (h *SegmentHandle) WriteLine(data []byte) error {
	if h == nil || h.file == nil || h.closed {
		return fmt.Errorf("segment is closed")
	}
	if _, err := h.file.Write(data); err != nil {
		return fmt.Errorf("write sample: %w", err)
	}
	written := int64(len(data))
	if len(data) == 0 || data[len(data)-1] != '\n' {
		if _, err := h.file.Write([]byte{'\n'}); err != nil {
			return fmt.Errorf("write sample newline: %w", err)
		}
		written++
	}
	h.sampleCount++
	h.bytes += written
	return nil
}

func (h *SegmentHandle) DropSamples(count int64) {
	if count > 0 {
		h.dropped += count
	}
}

func (h *SegmentHandle) Close(endedAt time.Time) (SegmentManifest, string, error) {
	if h == nil || h.file == nil {
		return SegmentManifest{}, "", fmt.Errorf("segment is not open")
	}
	if h.closed {
		return SegmentManifest{}, "", fmt.Errorf("segment is closed")
	}
	h.closed = true
	if endedAt.IsZero() {
		endedAt = time.Now().UTC()
	}
	endedAt = endedAt.UTC()
	if endedAt.Before(h.startedAt) {
		_ = h.file.Close()
		_ = os.Remove(h.tempPath)
		return SegmentManifest{}, "", fmt.Errorf("ended_at must be >= started_at")
	}

	if err := h.file.Close(); err != nil {
		_ = os.Remove(h.tempPath)
		return SegmentManifest{}, "", fmt.Errorf("close segment: %w", err)
	}
	if h.bytes == 0 {
		if info, err := os.Stat(h.tempPath); err == nil {
			h.bytes = info.Size()
		}
	}
	if err := os.Rename(h.tempPath, h.finalPath); err != nil {
		_ = os.Remove(h.tempPath)
		return SegmentManifest{}, "", fmt.Errorf("rename segment: %w", err)
	}

	manifest := SegmentManifest{
		SchemaVersion:  1,
		SegmentID:      h.segmentID,
		SourceID:       h.cfg.SourceID,
		SourceType:     h.cfg.SourceType,
		DataType:       h.cfg.DataType,
		Format:         h.cfg.Format,
		Path:           h.finalPath,
		StartedAt:      h.startedAt,
		EndedAt:        endedAt,
		SampleCount:    h.sampleCount,
		DroppedSamples: h.dropped,
		Bytes:          h.bytes,
		Clock:          &h.cfg.Clock,
		Labels:         cloneStringMap(h.cfg.Labels),
	}
	if err := validateManifest(manifest); err != nil {
		return SegmentManifest{}, "", err
	}
	if err := writeJSONFile(h.manifestPath, manifest); err != nil {
		return SegmentManifest{}, "", err
	}
	return manifest, h.manifestPath, nil
}

func (h *SegmentHandle) Abort() error {
	if h == nil || h.file == nil || h.closed {
		return nil
	}
	h.closed = true
	closeErr := h.file.Close()
	removeErr := os.Remove(h.tempPath)
	if closeErr != nil {
		return closeErr
	}
	if removeErr != nil && !os.IsNotExist(removeErr) {
		return removeErr
	}
	return nil
}

func segmentID(sourceID string, startedAt time.Time) string {
	return safeFileToken(sourceID) + "." + startedAt.UTC().Format("20060102T150405.000000000Z")
}

func safeFileToken(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), ".-_")
	if out == "" {
		return "segment"
	}
	return out
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
