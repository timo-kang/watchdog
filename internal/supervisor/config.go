package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Config struct {
	SocketPath  string
	AuditDir    string
	LatestPath  string
	StatePath   string
	HookTimeout time.Duration
	Cooldowns   CooldownConfig
	Hooks       HookConfig
}

type CooldownConfig struct {
	Notify   time.Duration
	Degrade  time.Duration
	SafeStop time.Duration
	Resolve  time.Duration
}

type HookConfig struct {
	Notify   []string `json:"notify"`
	Degrade  []string `json:"degrade"`
	SafeStop []string `json:"safe_stop"`
	Resolve  []string `json:"resolve"`
}

type fileConfig struct {
	SocketPath  string             `json:"socket_path"`
	AuditDir    string             `json:"audit_dir"`
	LatestPath  string             `json:"latest_path"`
	StatePath   string             `json:"state_path"`
	HookTimeout string             `json:"hook_timeout"`
	Cooldowns   fileCooldownConfig `json:"cooldowns"`
	Hooks       HookConfig         `json:"hooks"`
}

type fileCooldownConfig struct {
	Notify   string `json:"notify"`
	Degrade  string `json:"degrade"`
	SafeStop string `json:"safe_stop"`
	Resolve  string `json:"resolve"`
}

func LoadConfig(path string) (Config, error) {
	raw := fileConfig{
		SocketPath:  "./var/run/watchdog/supervisor.sock",
		AuditDir:    "./var/lib/watchdog/supervisor/requests",
		LatestPath:  "./var/lib/watchdog/supervisor/latest.json",
		StatePath:   "./var/lib/watchdog/supervisor/current_state.json",
		HookTimeout: "5s",
		Cooldowns: fileCooldownConfig{
			Notify:   "30s",
			Degrade:  "15s",
			SafeStop: "5s",
			Resolve:  "5s",
		},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	if raw.SocketPath == "" {
		return Config{}, fmt.Errorf("socket_path must not be empty")
	}
	if raw.AuditDir == "" {
		return Config{}, fmt.Errorf("audit_dir must not be empty")
	}
	if raw.StatePath == "" {
		return Config{}, fmt.Errorf("state_path must not be empty")
	}

	timeout, err := time.ParseDuration(raw.HookTimeout)
	if err != nil {
		return Config{}, fmt.Errorf("parse hook_timeout: %w", err)
	}
	if timeout <= 0 {
		return Config{}, fmt.Errorf("hook_timeout must be positive")
	}

	cooldowns, err := parseCooldowns(raw.Cooldowns)
	if err != nil {
		return Config{}, err
	}

	return Config{
		SocketPath:  raw.SocketPath,
		AuditDir:    raw.AuditDir,
		LatestPath:  raw.LatestPath,
		StatePath:   raw.StatePath,
		HookTimeout: timeout,
		Cooldowns:   cooldowns,
		Hooks:       raw.Hooks,
	}, nil
}

func parseCooldowns(raw fileCooldownConfig) (CooldownConfig, error) {
	notify, err := time.ParseDuration(raw.Notify)
	if err != nil {
		return CooldownConfig{}, fmt.Errorf("parse cooldowns.notify: %w", err)
	}
	degrade, err := time.ParseDuration(raw.Degrade)
	if err != nil {
		return CooldownConfig{}, fmt.Errorf("parse cooldowns.degrade: %w", err)
	}
	safeStop, err := time.ParseDuration(raw.SafeStop)
	if err != nil {
		return CooldownConfig{}, fmt.Errorf("parse cooldowns.safe_stop: %w", err)
	}
	resolve, err := time.ParseDuration(raw.Resolve)
	if err != nil {
		return CooldownConfig{}, fmt.Errorf("parse cooldowns.resolve: %w", err)
	}
	for key, value := range map[string]time.Duration{
		"notify":    notify,
		"degrade":   degrade,
		"safe_stop": safeStop,
		"resolve":   resolve,
	} {
		if value < 0 {
			return CooldownConfig{}, fmt.Errorf("cooldowns.%s must not be negative", key)
		}
	}
	return CooldownConfig{
		Notify:   notify,
		Degrade:  degrade,
		SafeStop: safeStop,
		Resolve:  resolve,
	}, nil
}
