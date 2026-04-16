package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRequiresActionSocketPathWhenEnabled(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"actions": {
			"unix_socket": {
				"enabled": true,
				"socket_path": ""
			}
		},
		"sources": {
			"host": {"enabled": false},
			"module_reports": {
				"enabled": false,
				"socket_path": "./var/run/watchdog/module.sock",
				"max_message_bytes": 4096,
				"default_stale_after": "5s"
			},
			"systemd": {"enabled": false, "units": []},
			"can": {"enabled": false, "backend": "socketcan", "interfaces": []},
			"ethercat": {"enabled": false, "backend": "igh", "masters": []}
		},
		"rules": {}
	}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "actions.unix_socket.socket_path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRequiresActionSpoolDirWhenEnabled(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"actions": {
			"unix_socket": {
				"enabled": true,
				"socket_path": "./var/run/watchdog/supervisor.sock",
				"spool_dir": "",
				"replay_batch_size": 64
			}
		},
		"sources": {
			"host": {"enabled": false},
			"module_reports": {
				"enabled": false,
				"socket_path": "./var/run/watchdog/module.sock",
				"max_message_bytes": 4096,
				"default_stale_after": "5s"
			},
			"systemd": {"enabled": false, "units": []},
			"can": {"enabled": false, "backend": "socketcan", "interfaces": []},
			"ethercat": {"enabled": false, "backend": "igh", "masters": []}
		},
		"rules": {}
	}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "actions.unix_socket.spool_dir") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRequiresActionReplayBatchSizeWhenEnabled(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"actions": {
			"unix_socket": {
				"enabled": true,
				"socket_path": "./var/run/watchdog/supervisor.sock",
				"spool_dir": "./var/spool/watchdog/actions",
				"replay_batch_size": 0
			}
		},
		"sources": {
			"host": {"enabled": false},
			"module_reports": {
				"enabled": false,
				"socket_path": "./var/run/watchdog/module.sock",
				"max_message_bytes": 4096,
				"default_stale_after": "5s"
			},
			"systemd": {"enabled": false, "units": []},
			"can": {"enabled": false, "backend": "socketcan", "interfaces": []},
			"ethercat": {"enabled": false, "backend": "igh", "masters": []}
		},
		"rules": {}
	}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "actions.unix_socket.replay_batch_size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRequiresProbeCommandForCANCommandJSON(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"sources": {
			"host": {"enabled": false},
			"module_reports": {
				"enabled": false,
				"socket_path": "./var/run/watchdog/module.sock",
				"max_message_bytes": 4096,
				"default_stale_after": "5s"
			},
			"systemd": {"enabled": false, "units": []},
			"can": {
				"enabled": true,
				"backend": "command-json",
				"interfaces": [
					{
						"name": "can0",
						"source_id": "drive-can",
						"expected_bitrate": 1000000,
						"require_up": true
					}
				]
			},
			"ethercat": {
				"enabled": false,
				"backend": "igh",
				"masters": []
			}
		},
		"rules": {}
	}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "sources.can.interfaces[0].probe_command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRequiresProbeCommandForEtherCATCommandJSON(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"sources": {
			"host": {"enabled": false},
			"module_reports": {
				"enabled": false,
				"socket_path": "./var/run/watchdog/module.sock",
				"max_message_bytes": 4096,
				"default_stale_after": "5s"
			},
			"systemd": {"enabled": false, "units": []},
			"can": {
				"enabled": false,
				"backend": "socketcan",
				"interfaces": []
			},
			"ethercat": {
				"enabled": true,
				"backend": "command-json",
				"masters": [
					{
						"name": "master0",
						"source_id": "actuators",
						"expected_state": "op",
						"expected_slaves": 12,
						"require_link": true
					}
				]
			}
		},
		"rules": {}
	}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "sources.ethercat.masters[0].probe_command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRequiresProbeCommandForEtherCATSOEM(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"sources": {
			"host": {"enabled": false},
			"module_reports": {
				"enabled": false,
				"socket_path": "./var/run/watchdog/module.sock",
				"max_message_bytes": 4096,
				"default_stale_after": "5s"
			},
			"systemd": {"enabled": false, "units": []},
			"can": {
				"enabled": false,
				"backend": "socketcan",
				"interfaces": []
			},
			"ethercat": {
				"enabled": true,
				"backend": "soem",
				"masters": [
					{
						"name": "master0",
						"source_id": "actuators",
						"expected_state": "op",
						"expected_slaves": 12,
						"require_link": true
					}
				]
			}
		},
		"rules": {}
	}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "sources.ethercat.masters[0].probe_command") {
		t.Fatalf("unexpected error: %v", err)
	}
}
