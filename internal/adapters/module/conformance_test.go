package module

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceProtocolV1Conformance(t *testing.T) {
	root := filepath.Join("..", "..", "..", "sdk", "fixtures", "source-protocol", "v1")
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
			err = ValidateReport(payload)
			switch c.Outcome {
			case "accept":
				if err != nil {
					t.Fatalf("expected accept, got %v", err)
				}
			case "reject":
				if err == nil {
					t.Fatalf("expected reject (%s), got nil", c.Reason)
				}
				if c.Reason != "" && !strings.Contains(err.Error(), c.Reason) {
					t.Fatalf("reject reason = %v, want contains %q", err, c.Reason)
				}
			default:
				t.Fatalf("unknown outcome %q", c.Outcome)
			}
		})
	}
}
