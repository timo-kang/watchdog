package supervisor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigParsesStateAndCooldowns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "supervisor.json")
	content := `{
  "socket_path": "/tmp/watchdog-supervisor.sock",
  "audit_dir": "/tmp/watchdog/audit",
  "latest_path": "/tmp/watchdog/latest.json",
  "state_path": "/tmp/watchdog/current_state.json",
  "metrics": {
    "enabled": true,
    "listen_address": "127.0.0.1:9109",
    "path": "/metrics"
  },
  "hook_timeout": "7s",
  "cooldowns": {
    "notify": "11s",
    "degrade": "22s",
    "safe_stop": "33s",
    "resolve": "44s"
  },
  "hooks": {
    "notify": ["echo", "notify"],
    "degrade": [],
    "safe_stop": [],
    "resolve": []
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.StatePath != "/tmp/watchdog/current_state.json" {
		t.Fatalf("StatePath = %q", cfg.StatePath)
	}
	if !cfg.Metrics.Enabled || cfg.Metrics.ListenAddress != "127.0.0.1:9109" {
		t.Fatalf("Metrics = %+v", cfg.Metrics)
	}
	if cfg.HookTimeout != 7*time.Second {
		t.Fatalf("HookTimeout = %s", cfg.HookTimeout)
	}
	if cfg.Cooldowns.Notify != 11*time.Second ||
		cfg.Cooldowns.Degrade != 22*time.Second ||
		cfg.Cooldowns.SafeStop != 33*time.Second ||
		cfg.Cooldowns.Resolve != 44*time.Second {
		t.Fatalf("Cooldowns = %+v", cfg.Cooldowns)
	}
}

func TestLoadConfigRejectsNegativeCooldown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "supervisor.json")
	content := `{
  "socket_path": "/tmp/watchdog-supervisor.sock",
  "audit_dir": "/tmp/watchdog/audit",
  "latest_path": "/tmp/watchdog/latest.json",
  "state_path": "/tmp/watchdog/current_state.json",
  "hook_timeout": "5s",
  "cooldowns": {
    "notify": "-1s",
    "degrade": "1s",
    "safe_stop": "1s",
    "resolve": "1s"
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := LoadConfig(path); err == nil {
		t.Fatal("LoadConfig unexpectedly succeeded")
	}
}

func TestLoadConfigRejectsMetricsPathWithoutSlash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "supervisor.json")
	content := `{
  "socket_path": "/tmp/watchdog-supervisor.sock",
  "audit_dir": "/tmp/watchdog/audit",
  "latest_path": "/tmp/watchdog/latest.json",
  "state_path": "/tmp/watchdog/current_state.json",
  "metrics": {
    "enabled": true,
    "listen_address": "127.0.0.1:9109",
    "path": "metrics"
  },
  "hook_timeout": "5s",
  "cooldowns": {
    "notify": "1s",
    "degrade": "1s",
    "safe_stop": "1s",
    "resolve": "1s"
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := LoadConfig(path); err == nil {
		t.Fatal("LoadConfig unexpectedly succeeded")
	}
}
