package dtc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFaultReportConformance(t *testing.T) {
	root := filepath.Join("..", "..", "sdk", "fixtures", "dtc", "v1")
	manifestBytes, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		Cases []struct {
			File    string `json:"file"`
			Outcome string `json:"outcome"`
			Reason  string `json:"reason"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if len(manifest.Cases) == 0 {
		t.Fatal("manifest has no cases")
	}
	for _, c := range manifest.Cases {
		t.Run(c.File, func(t *testing.T) {
			payload, err := os.ReadFile(filepath.Join(root, c.File))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			var report FaultReport
			decodeErr := json.Unmarshal(payload, &report)
			var err2 error
			if decodeErr != nil {
				err2 = decodeErr
			} else {
				err2 = report.Validate()
			}
			switch c.Outcome {
			case "accept":
				if err2 != nil {
					t.Fatalf("expected accept, got %v", err2)
				}
			case "reject":
				if err2 == nil {
					t.Fatalf("expected reject (%s), got nil", c.Reason)
				}
				if c.Reason != "" && !strings.Contains(err2.Error(), c.Reason) {
					t.Fatalf("reject reason = %v, want contains %q", err2, c.Reason)
				}
			default:
				t.Fatalf("unknown outcome %q", c.Outcome)
			}
		})
	}
}

func TestFaultReportRoundTrip(t *testing.T) {
	in := FaultReport{
		SchemaVersion: SchemaVersion,
		SourceID:      "robot-1.main",
		Sequence:      7,
		PublishedAt:   time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
		DeadlineMS:    1500,
		Active: []Fault{{
			Code:     "P1310",
			Severity: SeverityWarn,
			Since:    time.Date(2026, 1, 2, 15, 4, 4, 0, time.UTC),
			Units:    []FaultUnit{{Part: "pc.control_loop"}},
		}},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out FaultReport
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.SourceID != in.SourceID || out.Sequence != in.Sequence || len(out.Active) != 1 ||
		out.Active[0].Code != "P1310" || out.Active[0].Severity != SeverityWarn ||
		!out.PublishedAt.Equal(in.PublishedAt) {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func TestFaultReportForwardCompatibleUnknownFields(t *testing.T) {
	// A v1 consumer must ignore fields a future producer adds.
	payload := `{"schema_version":1,"source_id":"a","sequence":0,"published_at":"2026-01-02T15:04:05Z","deadline_ms":1000,"active":[],"future_field":{"x":1}}`
	var r FaultReport
	if err := json.Unmarshal([]byte(payload), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestFaultReportIsStale(t *testing.T) {
	pub := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	r := FaultReport{PublishedAt: pub, DeadlineMS: 1000}
	if r.IsStale(pub.Add(900 * time.Millisecond)) {
		t.Fatal("within deadline should not be stale")
	}
	if !r.IsStale(pub.Add(1100 * time.Millisecond)) {
		t.Fatal("past deadline should be stale")
	}
	// deadline 0 disables liveness.
	r0 := FaultReport{PublishedAt: pub, DeadlineMS: 0}
	if r0.IsStale(pub.Add(time.Hour)) {
		t.Fatal("deadline 0 should never be stale")
	}
}

func TestSeverityAndTransitionValid(t *testing.T) {
	for _, s := range []Severity{SeverityInfo, SeverityWarn, SeverityFatal} {
		if !s.Valid() {
			t.Fatalf("%q should be valid", s)
		}
	}
	if Severity("BAD").Valid() {
		t.Fatal("BAD should be invalid")
	}
	if !TransitionOpened.Valid() || !TransitionClosed.Valid() || Transition("x").Valid() {
		t.Fatal("transition validity wrong")
	}
}
