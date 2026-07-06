package supervisor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigDefaultsRetention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "supervisor.json")
	if err := os.WriteFile(path, []byte(`{"socket_path":"/x.sock","audit_dir":"/a","state_path":"/s.json"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DedupCacheSize != 2048 {
		t.Fatalf("DedupCacheSize = %d, want 2048", cfg.DedupCacheSize)
	}
	if cfg.Retention.Audit.MaxFiles != 5000 || cfg.Retention.Audit.MinKeep != 100 {
		t.Fatalf("audit retention defaults wrong: %+v", cfg.Retention.Audit)
	}
	if cfg.Retention.SweepInterval != time.Minute {
		t.Fatalf("SweepInterval = %v, want 1m", cfg.Retention.SweepInterval)
	}
}

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
  "shadow_fsm": {
    "enabled": true,
    "request_dir": "/tmp/watchdog/shadow-fsm/requests",
    "latest_path": "/tmp/watchdog/shadow-fsm/latest.json"
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
	if !cfg.ShadowFSM.Enabled ||
		cfg.ShadowFSM.RequestDir != "/tmp/watchdog/shadow-fsm/requests" ||
		cfg.ShadowFSM.LatestPath != "/tmp/watchdog/shadow-fsm/latest.json" {
		t.Fatalf("ShadowFSM = %+v", cfg.ShadowFSM)
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

func TestLoadConfigRejectsNonPositiveSweepInterval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "supervisor.json")
	content := `{
  "socket_path": "/x.sock",
  "audit_dir": "/a",
  "state_path": "/s.json",
  "retention": { "sweep_interval": "0s" }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig unexpectedly succeeded")
	}
	if err.Error() != "retention.sweep_interval must be positive" {
		t.Fatalf("error = %q, want %q", err.Error(), "retention.sweep_interval must be positive")
	}
}

func TestParsePolicyRejectsNegativeLimits(t *testing.T) {
	tests := []struct {
		name string
		raw  filePolicyConfig
		want string
	}{
		{
			name: "max files",
			raw:  filePolicyConfig{MaxFiles: -1},
			want: "max_files must not be negative",
		},
		{
			name: "min keep",
			raw:  filePolicyConfig{MinKeep: -1},
			want: "min_keep must not be negative",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePolicy(tt.raw)
			if err == nil {
				t.Fatal("parsePolicy unexpectedly succeeded")
			}
			if err.Error() != tt.want {
				t.Fatalf("error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestLoadConfigClampsDedupCacheToAuditMaxFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "supervisor.json")
	content := `{
  "socket_path": "/x.sock",
  "audit_dir": "/a",
  "state_path": "/s.json",
  "dedup_cache_size": 9000,
  "retention": {
    "audit": { "max_files": 100 }
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DedupCacheSize != cfg.Retention.Audit.MaxFiles {
		t.Fatalf("DedupCacheSize = %d, want %d (Audit.MaxFiles)", cfg.DedupCacheSize, cfg.Retention.Audit.MaxFiles)
	}
	if cfg.DedupCacheSize != 100 {
		t.Fatalf("DedupCacheSize = %d, want 100", cfg.DedupCacheSize)
	}
}

func TestLoadConfigRejectsEnabledShadowFSMWithoutRequestDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "supervisor.json")
	content := `{
  "socket_path": "/tmp/watchdog-supervisor.sock",
  "audit_dir": "/tmp/watchdog/audit",
  "latest_path": "/tmp/watchdog/latest.json",
  "state_path": "/tmp/watchdog/current_state.json",
  "shadow_fsm": {
    "enabled": true,
    "request_dir": ""
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
