# Source Producer Protocol v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Freeze the existing module-report Unix-datagram path as a documented, machine-verified public "Source Producer Protocol v1," with a self-test CLI, so any process (any language) can be a watchdog health source without forking `internal/`.

**Architecture:** Factor the daemon's report parse/validate into one shared function and expose it as `ValidateReport`; drive a set of conformance fixtures through that exact function in a Go test (drift alarm); ship a `watchdog-report-validate` binary over the same function; and write the normative protocol doc from the verified behavior. No wire-format change, no new importable API.

**Tech Stack:** Go 1.22 stdlib only; existing `internal/adapters/module` decoder; JSON fixtures.

## Global Constraints

- Pure Go, stdlib only. No new dependencies.
- **One validation path.** `ValidateReport` and the daemon's ingest (`decodeReport`) must call the same parse/validate core — "validates" and "the daemon accepts it" cannot diverge.
- **Freeze actual behavior.** The decoder today: rejects non-JSON, rejects empty `source_id`, rejects empty/invalid `severity` (`health.ParseSeverity`), accepts `ok|warn|fail|stale`, defaults `source_type`→`module`, treats `observed_at`/`stale_after_ms`/`metrics`/`labels` as optional. Document what is real; the conformance test is authoritative. If prose intent and code diverge, surface it — do not silently "fix" the wire contract.
- **Severity is required** (empty is rejected). Document it as such (the design spec's "see note" is superseded by verified behavior).
- Fixtures live in `sdk/fixtures/source-protocol/v1/{valid,invalid}/` with a `manifest.json`.
- gofmt-clean; `go vet ./...`; `go test -race ./...` green before each commit.
- **Task 5 (docs wiring) depends on PR #4 (`docs/public-oss-positioning`) merging** — `CONTRIBUTING.md` and the README "Contributing" section come from that PR. Do Task 5 last, after #4 is in `main` and this branch is rebased; keep its edits additive to avoid README conflicts.
- Spec of record: `docs/superpowers/specs/2026-07-13-source-protocol-v1-design.md`.

---

## Task 1: Shared validation core + exported `ValidateReport`

**Files:**
- Modify: `internal/adapters/module/module.go` (`decodeReport`; add `parseIncoming`, `ValidateReport`)
- Test: `internal/adapters/module/validate_test.go` (new)

**Interfaces:**
- Produces: `func ValidateReport(payload []byte) error`; internal `func parseIncoming(payload []byte) (incomingReport, health.Severity, error)`.
- `decodeReport` keeps its signature `(data []byte, defaultStaleAfter time.Duration) (reportState, error)` and now delegates parsing to `parseIncoming`.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapters/module/ -run TestValidateReport -v`
Expected: FAIL — `undefined: ValidateReport`.

- [ ] **Step 3: Implement**

Add to `internal/adapters/module/module.go`:

```go
// ValidateReport reports whether payload is a conformant v1 source report.
// It uses the same parse/validate path the daemon uses to ingest reports, so
// what validates here is exactly what the daemon accepts.
func ValidateReport(payload []byte) error {
	_, _, err := parseIncoming(payload)
	return err
}

// parseIncoming unmarshals and validates a v1 source report. It is the single
// source of truth for acceptance; both decodeReport and ValidateReport use it.
func parseIncoming(payload []byte) (incomingReport, health.Severity, error) {
	var incoming incomingReport
	if err := json.Unmarshal(payload, &incoming); err != nil {
		return incomingReport{}, "", fmt.Errorf("decode report: %w", err)
	}
	if incoming.SourceID == "" {
		return incomingReport{}, "", fmt.Errorf("source_id is required")
	}
	severity, err := health.ParseSeverity(incoming.Severity)
	if err != nil {
		return incomingReport{}, "", err
	}
	return incoming, severity, nil
}
```

Replace the body of `decodeReport` so it delegates to `parseIncoming` (removing the now-duplicated unmarshal / source_id / severity checks) and keeps the `source_type` default, `observed_at`, `stale_after_ms`, and `reportState` assembly:

```go
func decodeReport(data []byte, defaultStaleAfter time.Duration) (reportState, error) {
	incoming, severity, err := parseIncoming(data)
	if err != nil {
		return reportState{}, err
	}
	sourceType := incoming.SourceType
	if sourceType == "" {
		sourceType = "module"
	}
	collectedAt := incoming.ObservedAt
	if collectedAt.IsZero() {
		collectedAt = time.Now()
	}
	staleAfter := defaultStaleAfter
	if incoming.StaleAfterMS > 0 {
		staleAfter = time.Duration(incoming.StaleAfterMS) * time.Millisecond
	}
	return reportState{
		sourceID:    incoming.SourceID,
		sourceType:  sourceType,
		collectedAt: collectedAt,
		severity:    severity,
		reason:      incoming.Reason,
		metrics:     cloneMetrics(incoming.Metrics),
		labels:      cloneLabels(incoming.Labels),
		staleAfter:  staleAfter,
	}, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/adapters/module/ -v`
Expected: PASS — `TestValidateReport` plus all existing module tests (decode behavior is unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/module/module.go internal/adapters/module/validate_test.go
git commit -m "feat: share report validation and export ValidateReport"
```

---

## Task 2: Conformance fixtures + manifest + conformance test

**Files:**
- Create: `sdk/fixtures/source-protocol/v1/valid/{module_minimal,module_full,drive,ethercat}.json`
- Create: `sdk/fixtures/source-protocol/v1/invalid/{missing_source_id,empty_severity,bad_severity,wrong_metric_type,not_json}.json`
- Create: `sdk/fixtures/source-protocol/v1/manifest.json`
- Test: `internal/adapters/module/conformance_test.go` (new)

**Interfaces:**
- Consumes: `ValidateReport` (Task 1).

- [ ] **Step 1: Write the fixtures**

`valid/module_minimal.json`: `{"source_id":"robot-1.main","severity":"ok"}`
`valid/module_full.json`: `{"source_id":"robot-1.main","source_type":"module","severity":"warn","reason":"deadline miss","observed_at":"2026-01-02T15:04:05Z","stale_after_ms":1500,"metrics":{"control_period_us":551},"labels":{"process":"robot_control_node"}}`
`valid/drive.json`: `{"source_id":"robot-1.drive.left_hip","source_type":"drive","severity":"fail","reason":"over temp","metrics":{"drive.motor_temp_c":95.0,"drive.fault_code":12}}`
`valid/ethercat.json`: `{"source_id":"robot-1.ethercat","source_type":"ethercat","severity":"warn","metrics":{"ethercat.wkc_ratio":0.83}}`
`invalid/missing_source_id.json`: `{"severity":"ok"}`
`invalid/empty_severity.json`: `{"source_id":"a"}`
`invalid/bad_severity.json`: `{"source_id":"a","severity":"boom"}`
`invalid/wrong_metric_type.json`: `{"source_id":"a","severity":"ok","metrics":{"x":"not-a-number"}}`
`invalid/not_json.json`: `this is not json`

`manifest.json`:
```json
{
  "protocol_version": 1,
  "cases": [
    {"file": "valid/module_minimal.json", "outcome": "accept"},
    {"file": "valid/module_full.json", "outcome": "accept"},
    {"file": "valid/drive.json", "outcome": "accept"},
    {"file": "valid/ethercat.json", "outcome": "accept"},
    {"file": "invalid/missing_source_id.json", "outcome": "reject", "reason": "source_id"},
    {"file": "invalid/empty_severity.json", "outcome": "reject", "reason": "severity"},
    {"file": "invalid/bad_severity.json", "outcome": "reject", "reason": "severity"},
    {"file": "invalid/wrong_metric_type.json", "outcome": "reject", "reason": "decode report"},
    {"file": "invalid/not_json.json", "outcome": "reject", "reason": "decode report"}
  ]
}
```

- [ ] **Step 2: Write the failing conformance test**

```go
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
```

- [ ] **Step 3: Run test to verify it passes (fixtures now exist)**

Run: `go test -race ./internal/adapters/module/ -run TestSourceProtocolV1Conformance -v`
Expected: PASS for every manifest case. If any case fails, the fixture or manifest is wrong (or documents a behavior the decoder doesn't have) — reconcile against the real decoder, do not loosen the test.

- [ ] **Step 4: Commit**

```bash
git add sdk/fixtures/source-protocol/v1/ internal/adapters/module/conformance_test.go
git commit -m "test: add source protocol v1 conformance fixtures and test"
```

---

## Task 3: `watchdog-report-validate` CLI

**Files:**
- Create: `cmd/watchdog-report-validate/main.go`
- Test: `cmd/watchdog-report-validate/main_test.go`

**Interfaces:**
- Consumes: `module.ValidateReport` (Task 1).

- [ ] **Step 1: Write the CLI with a testable run function**

```go
// Command watchdog-report-validate checks whether a JSON payload is a
// conformant v1 source report, using the same validator the daemon uses.
// Reads from a file argument or, if none, stdin. Exit 0 = valid, 2 = invalid,
// 1 = usage/IO error.
package main

import (
	"fmt"
	"io"
	"os"

	"watchdog/internal/adapters/module"
)

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var data []byte
	var err error
	switch len(args) {
	case 0:
		data, err = io.ReadAll(stdin)
	case 1:
		data, err = os.ReadFile(args[0])
	default:
		fmt.Fprintln(stderr, "usage: watchdog-report-validate [file.json]  (or pipe JSON on stdin)")
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "read input: %v\n", err)
		return 1
	}
	if err := module.ValidateReport(data); err != nil {
		fmt.Fprintf(stderr, "invalid: %v\n", err)
		return 2
	}
	fmt.Fprintln(stdout, "valid: conformant v1 source report")
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
```

- [ ] **Step 2: Write the test**

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunValidAndInvalid(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run(nil, strings.NewReader(`{"source_id":"a","severity":"ok"}`), &out, &errBuf); code != 0 {
		t.Fatalf("valid payload exit = %d, want 0 (stderr=%s)", code, errBuf.String())
	}
	out.Reset()
	errBuf.Reset()
	if code := run(nil, strings.NewReader(`{"severity":"ok"}`), &out, &errBuf); code != 2 {
		t.Fatalf("invalid payload exit = %d, want 2", code)
	}
	if !strings.Contains(errBuf.String(), "source_id") {
		t.Fatalf("stderr = %q, want mention of source_id", errBuf.String())
	}
}
```

- [ ] **Step 3: Run + build**

Run: `go test -race ./cmd/watchdog-report-validate/ -v` (PASS) and `go build ./cmd/watchdog-report-validate` (OK).

- [ ] **Step 4: Commit**

```bash
git add cmd/watchdog-report-validate/
git commit -m "feat: add watchdog-report-validate self-test CLI"
```

---

## Task 4: Normative protocol document

**Files:**
- Create: `docs/source-protocol.md`

**Interfaces:** none (docs). Must reflect the behavior verified in Tasks 1-2.

- [ ] **Step 1: Write `docs/source-protocol.md`**

Write the normative spec with these sections and required statements:
- **Transport & framing:** one JSON object per Unix datagram to the module socket (default `/run/watchdog/module.sock`, configurable via `sources.module_reports.socket_path`); no reply; producer must not block if the socket is absent.
- **Message schema (v1):** the field table from the design spec — `source_id` (string, **required**), `source_type` (string, optional → `module`), `severity` (string, **required**, one of `ok|warn|fail`; `stale` is accepted by the decoder but reserved — `stale` is normally watchdog-derived from `stale_after_ms` expiry and producers should not self-report it), `reason` (string, optional), `observed_at` (RFC3339, optional → receipt time), `stale_after_ms` (integer, optional → `sources.module_reports.default_stale_after`), `metrics` (object of string→number, optional), `labels` (object of string→string, optional).
- **Acceptance rules (normative, matching the conformance fixtures):** reject non-JSON, missing/empty `source_id`, and empty/unknown `severity`; accept otherwise. Point readers to `sdk/fixtures/source-protocol/v1/` as the executable examples and to `watchdog-report-validate` for self-testing.
- **Severity → action semantics** and the debounce/consecutive-count behavior (summarize; link README).
- **`source_type` conventions:** `module` (loop timing e.g. `control_period_us`), `drive` (`drive.*`), `ethercat` (`ethercat.*`), evaluated under `rules.module`.
- **`source_id` identity rules:** stable per component; the grouping key for health/latch/incident/metrics.
- **Compatibility policy:** unknown fields ignored; additive-optional only; breaking change ⇒ v2 with a new fixture set; malformed reports dropped and counted; producer-optionality guarantee.

- [ ] **Step 2: Verify the doc matches the fixtures**

Manually cross-check every acceptance rule in the doc against a fixture in the manifest (each documented reject reason must have a matching `invalid/` fixture). Fix mismatches in the doc.

- [ ] **Step 3: Commit**

```bash
git add docs/source-protocol.md
git commit -m "docs: add normative source producer protocol v1 spec"
```

---

## Task 5: Docs wiring (AFTER PR #4 merges)

**Files:**
- Modify: `README.md`, `CONTRIBUTING.md`, `sdk/cpp/README.md`

**Precondition:** PR #4 (`docs/public-oss-positioning`) is merged into `main` and this branch is rebased onto it, so `CONTRIBUTING.md` and the README "Contributing"/"Roadmap" sections exist. If not yet merged, STOP and report — do not recreate those files here.

- [ ] **Step 1: Link the protocol doc**

Add a one-line pointer to `docs/source-protocol.md` in: the README "Contributing" (or "More Docs") section, `CONTRIBUTING.md` (under Scope or a new "Writing a source producer" line), and `sdk/cpp/README.md` (note the C++ SDK implements protocol v1; point to the normative doc + fixtures + `watchdog-report-validate`).

- [ ] **Step 2: Advance the roadmap marker**

In the README "Roadmap" section, move the source/adapter plugin-contract line from `🧭 exploring` to `✅ shipped` (protocol v1 frozen, documented, conformance-tested, with a validator).

- [ ] **Step 3: Verify + commit**

Run `gofmt -l $(git ls-files '*.go')` (empty), `go build ./...`, `go test -race ./...` (all pass — no code changed here), then:

```bash
git add README.md CONTRIBUTING.md sdk/cpp/README.md
git commit -m "docs: wire source protocol v1 into README, CONTRIBUTING, and C++ SDK"
```

---

## Self-Review

- **Spec coverage:** protocol doc (Task 4) ✓; fixtures (Task 2) ✓; executable conformance test tied to the real decoder (Task 2) ✓; `ValidateReport` + shared validation path (Task 1) ✓; `watchdog-report-validate` CLI (Task 3) ✓; compatibility policy documented (Task 4) ✓; docs wiring + roadmap (Task 5, sequenced after #4) ✓; "one validation implementation" acceptance criterion satisfied by `parseIncoming` shared between `decodeReport` and `ValidateReport` (Task 1) ✓.
- **Placeholder scan:** no TBD/TODO; every code step carries real code; Task 4 is prose with explicit required statements (a doc task's deliverable is the doc).
- **Type consistency:** `ValidateReport(payload []byte) error`, `parseIncoming(payload []byte) (incomingReport, health.Severity, error)`, `module.ValidateReport` used by the CLI, fixture path `../../../sdk/fixtures/source-protocol/v1` from the module test package — consistent across tasks.
- **Freeze-actual honored:** severity documented as required (verified), `stale` accepted-but-reserved noted; conformance test is authoritative.
