package adapters

import (
	"context"
	"testing"
)

func TestRunCommand(t *testing.T) {
	output, err := RunCommand(context.Background(), []string{"printf", "{\"ok\":true}"})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if string(output) != "{\"ok\":true}" {
		t.Fatalf("output = %q, want %q", string(output), "{\"ok\":true}")
	}
}

func TestRunCommandRequiresArgv(t *testing.T) {
	_, err := RunCommand(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
