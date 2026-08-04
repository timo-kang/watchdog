package module

import (
	"strings"
	"testing"
)

func TestValidateReport(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		wantErr string // "" = expect nil
	}{
		{"minimal ok", `{"source_id":"a","severity":"ok"}`, ""},
		{"full", `{"source_id":"a","source_type":"drive","severity":"warn","reason":"hot","stale_after_ms":1500,"metrics":{"x":1.5},"labels":{"k":"v"}}`, ""},
		{"missing source_id", `{"severity":"ok"}`, "source_id"},
		{"empty severity", `{"source_id":"a"}`, "severity"},
		{"bad severity", `{"source_id":"a","severity":"boom"}`, "severity"},
		{"not json", `not json`, "decode report"},
		{"wrong metric type", `{"source_id":"a","severity":"ok","metrics":{"x":"nope"}}`, "decode report"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateReport([]byte(tc.payload))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateReport(%s) = %v, want nil", tc.payload, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateReport(%s) = %v, want error containing %q", tc.payload, err, tc.wantErr)
			}
		})
	}
}
