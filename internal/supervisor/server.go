package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"watchdog/internal/actions"
)

type Server struct {
	logger *log.Logger
	cfg    Config
	state  *Manager
}

type AuditRecord struct {
	ReceivedAt time.Time       `json:"received_at"`
	Request    actions.Request `json:"request"`
	Decision   ApplyResult     `json:"decision"`
	Hook       *HookResult     `json:"hook,omitempty"`
	Duplicate  bool            `json:"duplicate,omitempty"`
}

type HookResult struct {
	Command           []string     `json:"command,omitempty"`
	Executed          bool         `json:"executed"`
	Suppressed        bool         `json:"suppressed,omitempty"`
	SuppressionReason string       `json:"suppression_reason,omitempty"`
	DurationMs        int64        `json:"duration_ms"`
	ExitCode          int          `json:"exit_code,omitempty"`
	Stdout            string       `json:"stdout,omitempty"`
	Stderr            string       `json:"stderr,omitempty"`
	Error             string       `json:"error,omitempty"`
	Action            actions.Kind `json:"action"`
}

func NewServer(logger *log.Logger, cfg Config) *Server {
	return &Server{
		logger: logger,
		cfg:    cfg,
	}
}

func (s *Server) Run(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.cfg.SocketPath), 0o755); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	if err := os.MkdirAll(s.cfg.AuditDir, 0o755); err != nil {
		return fmt.Errorf("create audit dir: %w", err)
	}
	if s.cfg.LatestPath != "" {
		if err := os.MkdirAll(filepath.Dir(s.cfg.LatestPath), 0o755); err != nil {
			return fmt.Errorf("create latest dir: %w", err)
		}
	}
	if err := ensureStateDir(s.cfg.StatePath); err != nil {
		return err
	}

	state, err := LoadManager(s.cfg.StatePath, s.cfg.Cooldowns)
	if err != nil {
		return err
	}
	if err := state.Write(); err != nil {
		return err
	}
	s.state = state

	if err := removeStaleSocket(s.cfg.SocketPath); err != nil {
		return err
	}

	addr := &net.UnixAddr{Name: s.cfg.SocketPath, Net: "unixgram"}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		return fmt.Errorf("listen unixgram: %w", err)
	}
	defer func() {
		conn.Close()
		_ = os.Remove(s.cfg.SocketPath)
	}()
	_ = os.Chmod(s.cfg.SocketPath, 0o660)

	buf := make([]byte, 64*1024)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := conn.ReadFromUnix(buf)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				continue
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read action request: %w", err)
		}

		payload := append([]byte(nil), buf[:n]...)
		if err := s.handlePayload(ctx, payload); err != nil {
			s.logger.Printf("handle request: %v", err)
		}
	}
}

func (s *Server) handlePayload(ctx context.Context, payload []byte) error {
	var request actions.Request
	if err := json.Unmarshal(payload, &request); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if err := validateRequest(request); err != nil {
		return err
	}

	recordPath := filepath.Join(s.cfg.AuditDir, request.RequestID+".json")
	if _, err := os.Stat(recordPath); err == nil {
		s.logger.Printf("duplicate request_id=%s action=%s", request.RequestID, request.RequestedAction)
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat audit record: %w", err)
	}

	decision, err := s.state.Apply(request)
	if err != nil {
		return err
	}

	result, err := s.dispatchHook(ctx, request, payload, decision)
	record := AuditRecord{
		ReceivedAt: time.Now().UTC(),
		Request:    request,
		Decision:   decision,
		Hook:       result,
	}
	if writeErr := writeJSONFile(recordPath, record); writeErr != nil {
		return writeErr
	}
	if s.cfg.LatestPath != "" {
		if writeErr := writeJSONFile(s.cfg.LatestPath, record); writeErr != nil {
			return writeErr
		}
	}

	if err != nil {
		s.logger.Printf("request_id=%s action=%s hook_error=%v", request.RequestID, request.RequestedAction, err)
		return nil
	}

	if result != nil && result.Suppressed {
		s.logger.Printf("request_id=%s action=%s event=%s suppressed=%s", request.RequestID, request.RequestedAction, request.Event, result.SuppressionReason)
		return nil
	}

	s.logger.Printf("request_id=%s action=%s event=%s", request.RequestID, request.RequestedAction, request.Event)
	return nil
}

func (s *Server) dispatchHook(ctx context.Context, request actions.Request, payload []byte, decision ApplyResult) (*HookResult, error) {
	if !decision.ShouldExecuteHook {
		return &HookResult{
			Action:            decision.HookAction,
			Executed:          false,
			Suppressed:        true,
			SuppressionReason: decision.SuppressionReason,
		}, nil
	}

	command := s.commandFor(decision.HookAction)
	if len(command) == 0 {
		return &HookResult{
			Action:   decision.HookAction,
			Executed: false,
		}, nil
	}

	hookCtx, cancel := context.WithTimeout(ctx, s.cfg.HookTimeout)
	defer cancel()

	cmd := exec.CommandContext(hookCtx, command[0], command[1:]...)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(os.Environ(),
		"WATCHDOG_REQUEST_ID="+request.RequestID,
		"WATCHDOG_EVENT="+string(request.Event),
		"WATCHDOG_ACTION="+string(request.RequestedAction),
		"WATCHDOG_OVERALL="+string(request.Overall),
		"WATCHDOG_HOSTNAME="+request.Hostname,
		"WATCHDOG_INCIDENT_PATH="+request.IncidentPath,
		"WATCHDOG_COMPONENT_IDS="+strings.Join(componentIDs(request.Components), ","),
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &HookResult{
		Command:    append([]string(nil), command...),
		Executed:   true,
		DurationMs: duration.Milliseconds(),
		Stdout:     strings.TrimSpace(stdout.String()),
		Stderr:     strings.TrimSpace(stderr.String()),
		Action:     decision.HookAction,
	}

	if err == nil {
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	}
	result.Error = err.Error()
	return result, err
}

func (s *Server) commandFor(action actions.Kind) []string {
	switch action {
	case actions.ActionNotify:
		return s.cfg.Hooks.Notify
	case actions.ActionDegrade:
		return s.cfg.Hooks.Degrade
	case actions.ActionSafeStop:
		return s.cfg.Hooks.SafeStop
	case actions.ActionResolve:
		return s.cfg.Hooks.Resolve
	default:
		return nil
	}
}

func validateRequest(request actions.Request) error {
	if request.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema_version %d", request.SchemaVersion)
	}
	if request.RequestID == "" {
		return fmt.Errorf("request_id must not be empty")
	}
	switch request.Event {
	case actions.EventTransition, actions.EventResolved:
	default:
		return fmt.Errorf("unsupported event %q", request.Event)
	}
	if request.Event == actions.EventTransition && len(request.Components) == 0 {
		return fmt.Errorf("transition event must include at least one component")
	}
	switch request.RequestedAction {
	case actions.ActionNotify, actions.ActionDegrade, actions.ActionSafeStop, actions.ActionResolve:
	default:
		return fmt.Errorf("unsupported requested_action %q", request.RequestedAction)
	}
	return nil
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat socket path: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("socket path exists and is not a socket: %s", path)
	}
	addr := &net.UnixAddr{Name: path, Net: "unixgram"}
	conn, dialErr := net.DialUnix("unixgram", nil, addr)
	if dialErr == nil {
		_ = conn.Close()
		return fmt.Errorf("socket path already has an active listener: %s", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json file: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write json temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename json file: %w", err)
	}
	return nil
}

func componentIDs(components []actions.ComponentRequest) []string {
	out := make([]string, 0, len(components))
	for _, component := range components {
		out = append(out, component.ComponentID)
	}
	return out
}
