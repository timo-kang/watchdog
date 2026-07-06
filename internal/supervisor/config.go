package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"watchdog/internal/metrics"
	"watchdog/internal/retention"
)

type Config struct {
	SocketPath     string
	AuditDir       string
	LatestPath     string
	StatePath      string
	Metrics        metrics.EndpointConfig
	ShadowFSM      ShadowFSMConfig
	HookTimeout    time.Duration
	Cooldowns      CooldownConfig
	Hooks          HookConfig
	Retention      RetentionConfig
	DedupCacheSize int
}

type RetentionConfig struct {
	SweepInterval time.Duration
	Audit         retention.Policy
	Shadow        retention.Policy
}

type ShadowFSMConfig struct {
	Enabled    bool   `json:"enabled"`
	RequestDir string `json:"request_dir"`
	LatestPath string `json:"latest_path"`
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
	SocketPath     string                 `json:"socket_path"`
	AuditDir       string                 `json:"audit_dir"`
	LatestPath     string                 `json:"latest_path"`
	StatePath      string                 `json:"state_path"`
	Metrics        metrics.EndpointConfig `json:"metrics"`
	ShadowFSM      ShadowFSMConfig        `json:"shadow_fsm"`
	HookTimeout    string                 `json:"hook_timeout"`
	Cooldowns      fileCooldownConfig     `json:"cooldowns"`
	Hooks          HookConfig             `json:"hooks"`
	Retention      fileRetentionConfig    `json:"retention"`
	DedupCacheSize int                    `json:"dedup_cache_size"`
}

type fileCooldownConfig struct {
	Notify   string `json:"notify"`
	Degrade  string `json:"degrade"`
	SafeStop string `json:"safe_stop"`
	Resolve  string `json:"resolve"`
}

type fileRetentionConfig struct {
	SweepInterval string           `json:"sweep_interval"`
	Audit         filePolicyConfig `json:"audit"`
	Shadow        filePolicyConfig `json:"shadow"`
}

type filePolicyConfig struct {
	MaxFiles int    `json:"max_files"`
	MaxBytes string `json:"max_bytes"`
	MinKeep  int    `json:"min_keep"`
}

func LoadConfig(path string) (Config, error) {
	raw := fileConfig{
		SocketPath: "./var/run/watchdog/supervisor.sock",
		AuditDir:   "./var/lib/watchdog/supervisor/requests",
		LatestPath: "./var/lib/watchdog/supervisor/latest.json",
		StatePath:  "./var/lib/watchdog/supervisor/current_state.json",
		Metrics:    metrics.DefaultEndpoint("127.0.0.1:9109"),
		ShadowFSM: ShadowFSMConfig{
			Enabled:    false,
			RequestDir: "./var/lib/watchdog/supervisor/shadow_fsm/requests",
			LatestPath: "./var/lib/watchdog/supervisor/shadow_fsm/latest.json",
		},
		HookTimeout: "5s",
		Cooldowns: fileCooldownConfig{
			Notify:   "30s",
			Degrade:  "15s",
			SafeStop: "5s",
			Resolve:  "5s",
		},
		DedupCacheSize: 2048,
		Retention: fileRetentionConfig{
			SweepInterval: "60s",
			Audit:         filePolicyConfig{MaxFiles: 5000, MaxBytes: "64Mi", MinKeep: 100},
			Shadow:        filePolicyConfig{MaxFiles: 1000, MaxBytes: "32Mi", MinKeep: 50},
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
	raw.Metrics = metrics.NormalizeEndpoint(raw.Metrics, "127.0.0.1:9109")
	if err := metrics.ValidateEndpoint("metrics", raw.Metrics); err != nil {
		return Config{}, err
	}
	if raw.ShadowFSM.Enabled && raw.ShadowFSM.RequestDir == "" {
		return Config{}, fmt.Errorf("shadow_fsm.request_dir must not be empty when enabled")
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

	retentionCfg, err := parseRetention(raw.Retention)
	if err != nil {
		return Config{}, err
	}
	dedupSize := raw.DedupCacheSize
	if dedupSize <= 0 {
		dedupSize = 2048
	}
	if retentionCfg.Audit.MaxFiles > 0 && dedupSize > retentionCfg.Audit.MaxFiles {
		dedupSize = retentionCfg.Audit.MaxFiles
	}

	return Config{
		SocketPath:     raw.SocketPath,
		AuditDir:       raw.AuditDir,
		LatestPath:     raw.LatestPath,
		StatePath:      raw.StatePath,
		Metrics:        raw.Metrics,
		ShadowFSM:      raw.ShadowFSM,
		HookTimeout:    timeout,
		Cooldowns:      cooldowns,
		Hooks:          raw.Hooks,
		Retention:      retentionCfg,
		DedupCacheSize: dedupSize,
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

func parseRetention(raw fileRetentionConfig) (RetentionConfig, error) {
	interval, err := time.ParseDuration(nonEmpty(raw.SweepInterval, "60s"))
	if err != nil {
		return RetentionConfig{}, fmt.Errorf("parse retention.sweep_interval: %w", err)
	}
	if interval <= 0 {
		return RetentionConfig{}, fmt.Errorf("retention.sweep_interval must be positive")
	}
	audit, err := parsePolicy(raw.Audit)
	if err != nil {
		return RetentionConfig{}, fmt.Errorf("retention.audit: %w", err)
	}
	shadow, err := parsePolicy(raw.Shadow)
	if err != nil {
		return RetentionConfig{}, fmt.Errorf("retention.shadow: %w", err)
	}
	return RetentionConfig{SweepInterval: interval, Audit: audit, Shadow: shadow}, nil
}

func parsePolicy(raw filePolicyConfig) (retention.Policy, error) {
	if raw.MaxFiles < 0 {
		return retention.Policy{}, fmt.Errorf("max_files must not be negative")
	}
	if raw.MinKeep < 0 {
		return retention.Policy{}, fmt.Errorf("min_keep must not be negative")
	}
	maxBytes, err := retention.ParseByteSize(raw.MaxBytes)
	if err != nil {
		return retention.Policy{}, err
	}
	return retention.Policy{MaxBytes: maxBytes, MaxFiles: raw.MaxFiles, MinKeep: raw.MinKeep}, nil
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
