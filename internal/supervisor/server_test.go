package supervisor

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"watchdog/internal/actions"
	"watchdog/internal/health"
)

func TestServerProcessesAndDedupesRequests(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := filepath.Join(tempDir, "supervisor.sock")
	auditDir := filepath.Join(tempDir, "audit")
	latestPath := filepath.Join(tempDir, "latest.json")
	statePath := filepath.Join(tempDir, "current_state.json")
	hookOutput := filepath.Join(tempDir, "hook.log")

	cfg := Config{
		SocketPath:  socketPath,
		AuditDir:    auditDir,
		LatestPath:  latestPath,
		StatePath:   statePath,
		HookTimeout: 2 * time.Second,
		Cooldowns: CooldownConfig{
			Degrade: time.Hour,
		},
		Hooks: HookConfig{
			Degrade: []string{os.Args[0], "-test.run=TestSupervisorHookHelper", "--", "--helper-process", hookOutput},
		},
	}

	server := NewServer(log.New(io.Discard, "", 0), cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run(ctx)
	}()
	waitForPath(t, socketPath)

	request := actions.Request{
		SchemaVersion:   1,
		RequestID:       "req-1",
		Event:           actions.EventTransition,
		Timestamp:       time.Now().UTC(),
		Hostname:        "robot-1",
		Overall:         health.SeverityFail,
		RequestedAction: actions.ActionDegrade,
		Reason:          "degrade requested",
		Components: []actions.ComponentRequest{
			{
				ComponentID:     "planner",
				Severity:        health.SeverityFail,
				RequestedAction: actions.ActionDegrade,
				Reason:          "missed deadline",
				SourceTypes:     []string{"module"},
			},
		},
	}

	sendRequest(t, socketPath, request)
	waitForPath(t, filepath.Join(auditDir, "req-1.json"))
	waitForPath(t, latestPath)
	waitForPath(t, statePath)
	waitForPath(t, hookOutput)

	secondRequest := request
	secondRequest.RequestID = "req-2"
	secondRequest.Timestamp = request.Timestamp.Add(5 * time.Second)
	secondRequest.Components[0].Reason = "missed deadline again"
	sendRequest(t, socketPath, secondRequest)
	waitForPath(t, filepath.Join(auditDir, "req-2.json"))

	sendRequest(t, socketPath, request)
	time.Sleep(200 * time.Millisecond)

	content, err := os.ReadFile(hookOutput)
	if err != nil {
		t.Fatalf("read hook output: %v", err)
	}
	lines := strings.Fields(strings.TrimSpace(string(content)))
	if len(lines) != 1 || lines[0] != "req-1" {
		t.Fatalf("hook lines = %q, want single req-1", string(content))
	}

	var current State
	stateContent, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if err := json.Unmarshal(stateContent, &current); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if current.OverallAction != actions.ActionDegrade {
		t.Fatalf("overall action = %s, want degrade", current.OverallAction)
	}
	if len(current.ActiveComponents) != 1 || current.ActiveComponents[0].ComponentID != "planner" {
		t.Fatalf("active components = %+v, want planner", current.ActiveComponents)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("server run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server shutdown")
	}
}

func TestRemoveStaleSocketRemovesClosedSocket(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := filepath.Join(tempDir, "supervisor.sock")
	addr := &net.UnixAddr{Name: socketPath, Net: "unixgram"}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Fatalf("listen unixgram: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close unixgram: %v", err)
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("stat closed socket: %v", err)
	}

	if err := removeStaleSocket(socketPath); err != nil {
		t.Fatalf("removeStaleSocket: %v", err)
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket path still exists after cleanup: %v", err)
	}
}

func TestRemoveStaleSocketRejectsActiveListener(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := filepath.Join(tempDir, "supervisor.sock")
	addr := &net.UnixAddr{Name: socketPath, Net: "unixgram"}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Fatalf("listen unixgram: %v", err)
	}
	defer func() {
		_ = conn.Close()
		_ = os.Remove(socketPath)
	}()

	err = removeStaleSocket(socketPath)
	if err == nil || !strings.Contains(err.Error(), "active listener") {
		t.Fatalf("removeStaleSocket error = %v, want active listener error", err)
	}
}

func TestSupervisorHookHelper(t *testing.T) {
	idx := -1
	for i, arg := range os.Args {
		if arg == "--helper-process" {
			idx = i
			break
		}
	}
	if idx == -1 || idx+1 >= len(os.Args) {
		return
	}

	outputPath := os.Args[idx+1]
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		t.Fatalf("read stdin: %v", err)
	}
	var request actions.Request
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatalf("decode stdin request: %v", err)
	}
	f, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open hook output: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(request.RequestID + "\n"); err != nil {
		t.Fatalf("write hook output: %v", err)
	}
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for path %s", path)
}

func sendRequest(t *testing.T, socketPath string, request actions.Request) {
	t.Helper()
	addr := &net.UnixAddr{Name: socketPath, Net: "unixgram"}
	conn, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		t.Fatalf("dial unixgram: %v", err)
	}
	defer conn.Close()

	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write request: %v", err)
	}
}
