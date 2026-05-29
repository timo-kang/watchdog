package rawlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"watchdog/internal/health"
)

type SegmentManifest struct {
	SchemaVersion  int               `json:"schema_version"`
	SegmentID      string            `json:"segment_id"`
	SourceID       string            `json:"source_id"`
	SourceType     string            `json:"source_type,omitempty"`
	DataType       string            `json:"data_type"`
	Format         string            `json:"format"`
	Path           string            `json:"path"`
	StartedAt      time.Time         `json:"started_at"`
	EndedAt        time.Time         `json:"ended_at"`
	SampleCount    int64             `json:"sample_count"`
	DroppedSamples int64             `json:"dropped_samples,omitempty"`
	Bytes          int64             `json:"bytes"`
	Clock          *ClockInfo        `json:"clock,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
}

type ClockInfo struct {
	TimeBase     string `json:"time_base,omitempty"`
	Synchronized bool   `json:"synchronized"`
}

type IncidentIndex struct {
	SchemaVersion int          `json:"schema_version"`
	IncidentPath  string       `json:"incident_path"`
	IncidentAt    time.Time    `json:"incident_at"`
	Window        TimeWindow   `json:"window"`
	Segments      []SegmentRef `json:"segments"`
	Errors        []string     `json:"errors,omitempty"`
}

type TimeWindow struct {
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
}

type SegmentRef struct {
	SourceID     string    `json:"source_id"`
	SegmentID    string    `json:"segment_id"`
	DataType     string    `json:"data_type"`
	Format       string    `json:"format"`
	Path         string    `json:"path"`
	ManifestPath string    `json:"manifest_path"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at"`
	Bytes        int64     `json:"bytes"`
}

type Linker struct {
	ManifestDir      string
	IncidentIndexDir string
	PreWindow        time.Duration
	PostWindow       time.Duration
}

type matchedSegment struct {
	Manifest     SegmentManifest
	ManifestPath string
}

func (l Linker) LinkIncident(incidentPath string, snapshot health.Snapshot) (string, error) {
	if incidentPath == "" {
		return "", nil
	}
	if strings.TrimSpace(l.ManifestDir) == "" {
		return "", fmt.Errorf("raw log manifest dir must not be empty")
	}
	if strings.TrimSpace(l.IncidentIndexDir) == "" {
		return "", fmt.Errorf("raw log incident index dir must not be empty")
	}
	if l.PreWindow < 0 || l.PostWindow < 0 {
		return "", fmt.Errorf("raw log windows must be >= 0")
	}

	window := TimeWindow{
		StartedAt: snapshot.CollectedAt.Add(-l.PreWindow).UTC(),
		EndedAt:   snapshot.CollectedAt.Add(l.PostWindow).UTC(),
	}
	index := IncidentIndex{
		SchemaVersion: 1,
		IncidentPath:  incidentPath,
		IncidentAt:    snapshot.CollectedAt.UTC(),
		Window:        window,
	}

	segments, errors := l.matchingSegments(window)
	index.Segments = segmentRefs(segments)
	index.Errors = errors

	if err := os.MkdirAll(l.IncidentIndexDir, 0o755); err != nil {
		return "", fmt.Errorf("create raw log incident index dir: %w", err)
	}
	path := filepath.Join(l.IncidentIndexDir, incidentIndexFilename(incidentPath))
	if err := writeJSONFile(path, index); err != nil {
		return "", err
	}
	return path, nil
}

func (l Linker) matchingSegments(window TimeWindow) ([]matchedSegment, []string) {
	var segments []matchedSegment
	var errors []string
	err := filepath.WalkDir(l.ManifestDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		manifest, err := readManifest(path)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if overlaps(manifest.StartedAt, manifest.EndedAt, window.StartedAt, window.EndedAt) {
			segments = append(segments, matchedSegment{
				Manifest:     manifest,
				ManifestPath: path,
			})
		}
		return nil
	})
	if err != nil {
		errors = append(errors, err.Error())
	}
	sort.SliceStable(segments, func(i, j int) bool {
		left := segments[i].Manifest
		right := segments[j].Manifest
		if left.StartedAt.Equal(right.StartedAt) {
			return left.SegmentID < right.SegmentID
		}
		return left.StartedAt.Before(right.StartedAt)
	})
	return segments, errors
}

func readManifest(path string) (SegmentManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SegmentManifest{}, err
	}
	var manifest SegmentManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return SegmentManifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := validateManifest(manifest); err != nil {
		return SegmentManifest{}, err
	}
	return manifest, nil
}

func validateManifest(manifest SegmentManifest) error {
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema_version %d", manifest.SchemaVersion)
	}
	if manifest.SegmentID == "" {
		return fmt.Errorf("segment_id must not be empty")
	}
	if manifest.SourceID == "" {
		return fmt.Errorf("source_id must not be empty")
	}
	if manifest.DataType == "" {
		return fmt.Errorf("data_type must not be empty")
	}
	if manifest.Format == "" {
		return fmt.Errorf("format must not be empty")
	}
	if manifest.Path == "" {
		return fmt.Errorf("path must not be empty")
	}
	if manifest.StartedAt.IsZero() {
		return fmt.Errorf("started_at must not be empty")
	}
	if manifest.EndedAt.IsZero() {
		return fmt.Errorf("ended_at must not be empty")
	}
	if manifest.EndedAt.Before(manifest.StartedAt) {
		return fmt.Errorf("ended_at must be >= started_at")
	}
	if manifest.SampleCount < 0 {
		return fmt.Errorf("sample_count must be >= 0")
	}
	if manifest.Bytes < 0 {
		return fmt.Errorf("bytes must be >= 0")
	}
	return nil
}

func segmentRefs(segments []matchedSegment) []SegmentRef {
	out := make([]SegmentRef, 0, len(segments))
	for _, segment := range segments {
		manifest := segment.Manifest
		out = append(out, SegmentRef{
			SourceID:     manifest.SourceID,
			SegmentID:    manifest.SegmentID,
			DataType:     manifest.DataType,
			Format:       manifest.Format,
			Path:         manifest.Path,
			ManifestPath: segment.ManifestPath,
			StartedAt:    manifest.StartedAt.UTC(),
			EndedAt:      manifest.EndedAt.UTC(),
			Bytes:        manifest.Bytes,
		})
	}
	return out
}

func overlaps(aStart, aEnd, bStart, bEnd time.Time) bool {
	return !aEnd.Before(bStart) && !aStart.After(bEnd)
}

func incidentIndexFilename(incidentPath string) string {
	base := filepath.Base(incidentPath)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if base == "" || base == "." {
		base = "incident"
	}
	return base + ".rawlog-index.json"
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal raw log index: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write raw log index temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename raw log index: %w", err)
	}
	return nil
}
