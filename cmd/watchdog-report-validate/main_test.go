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
	if !strings.Contains(out.String(), "valid") {
		t.Fatalf("stdout = %q, want a valid message", out.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := run(nil, strings.NewReader(`{"severity":"ok"}`), &out, &errBuf); code != 2 {
		t.Fatalf("invalid payload exit = %d, want 2", code)
	}
	if !strings.Contains(errBuf.String(), "source_id") {
		t.Fatalf("stderr = %q, want mention of source_id", errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := run([]string{"a", "b"}, strings.NewReader(""), &out, &errBuf); code != 1 {
		t.Fatalf("too many args exit = %d, want 1", code)
	}
}
