package supervisor

import (
	"path/filepath"
	"time"

	"watchdog/internal/actions"
	"watchdog/internal/health"
)

type ShadowFSMResult struct {
	Enabled           bool         `json:"enabled"`
	Action            actions.Kind `json:"action,omitempty"`
	Written           bool         `json:"written"`
	Suppressed        bool         `json:"suppressed,omitempty"`
	SuppressionReason string       `json:"suppression_reason,omitempty"`
	Path              string       `json:"path,omitempty"`
	LatestPath        string       `json:"latest_path,omitempty"`
	Error             string       `json:"error,omitempty"`
}

type RobotFSMRequest struct {
	SchemaVersion      int                        `json:"schema_version"`
	Mode               string                     `json:"mode"`
	CreatedAt          time.Time                  `json:"created_at"`
	WatchdogRequestID  string                     `json:"watchdog_request_id"`
	Event              actions.Event              `json:"event"`
	RequestedAction    actions.Kind               `json:"requested_action"`
	SuggestedCommand   string                     `json:"suggested_command"`
	Hostname           string                     `json:"hostname"`
	Overall            health.Severity            `json:"overall"`
	PreviousOverall    health.Severity            `json:"previous_overall,omitempty"`
	Reason             string                     `json:"reason,omitempty"`
	IncidentPath       string                     `json:"incident_path,omitempty"`
	Components         []actions.ComponentRequest `json:"components,omitempty"`
	ResolvedComponents []string                   `json:"resolved_components,omitempty"`
}

func (s *Server) writeShadowFSMRequest(request actions.Request, decision ApplyResult) *ShadowFSMResult {
	if !s.cfg.ShadowFSM.Enabled {
		return nil
	}

	result := &ShadowFSMResult{
		Enabled: true,
		Action:  decision.HookAction,
	}
	if !decision.ShouldExecuteHook {
		result.Suppressed = true
		result.SuppressionReason = decision.SuppressionReason
		return result
	}

	robotRequest := buildRobotFSMRequest(request, decision)
	path := filepath.Join(s.cfg.ShadowFSM.RequestDir, request.RequestID+".json")
	result.Path = path
	if err := writeJSONDurable(path, robotRequest); err != nil {
		result.Error = err.Error()
		return result
	}
	result.Written = true

	if s.cfg.ShadowFSM.LatestPath != "" {
		result.LatestPath = s.cfg.ShadowFSM.LatestPath
		if err := writeJSONAtomic(s.cfg.ShadowFSM.LatestPath, robotRequest); err != nil {
			result.Error = err.Error()
			return result
		}
	}
	return result
}

func buildRobotFSMRequest(request actions.Request, decision ApplyResult) RobotFSMRequest {
	action := decision.HookAction
	if action == actions.ActionNone {
		action = request.RequestedAction
	}
	return RobotFSMRequest{
		SchemaVersion:      1,
		Mode:               "shadow",
		CreatedAt:          time.Now().UTC(),
		WatchdogRequestID:  request.RequestID,
		Event:              request.Event,
		RequestedAction:    action,
		SuggestedCommand:   suggestedFSMCommand(action),
		Hostname:           request.Hostname,
		Overall:            request.Overall,
		PreviousOverall:    request.PreviousOverall,
		Reason:             request.Reason,
		IncidentPath:       request.IncidentPath,
		Components:         append([]actions.ComponentRequest(nil), request.Components...),
		ResolvedComponents: append([]string(nil), request.Resolved...),
	}
}

func suggestedFSMCommand(action actions.Kind) string {
	switch action {
	case actions.ActionNotify:
		return "notify_operator"
	case actions.ActionDegrade:
		return "enter_degraded_mode"
	case actions.ActionSafeStop:
		return "request_safe_stop"
	case actions.ActionResolve:
		return "clear_watchdog_condition"
	default:
		return ""
	}
}
