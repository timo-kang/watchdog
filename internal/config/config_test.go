package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestLoadNormalizesEtherCATSlaveTopology(t *testing.T) {
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
			"can": {"enabled": false, "backend": "socketcan", "interfaces": []},
			"ethercat": {
				"enabled": true,
				"backend": "igh",
				"masters": [
					{
						"name": "master0",
						"source_id": "actuators",
						"expected_state": "op",
						"expected_slaves": 2,
						"require_link": true,
						"slaves": [
							{"position": 1, "name": "left_hip", "criticality": "critical"},
							{"position": 2, "name": "diagnostic_io"}
						]
					}
				]
			}
		},
		"rules": {}
	}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	slaves := cfg.Sources.EtherCAT.Masters[0].Slaves
	if len(slaves) != 2 {
		t.Fatalf("len(slaves) = %d, want 2", len(slaves))
	}
	if slaves[0].Criticality != "critical" || slaves[0].ExpectedState != "op" {
		t.Fatalf("first slave = %#v", slaves[0])
	}
	if slaves[1].Criticality != "important" || slaves[1].ExpectedState != "op" {
		t.Fatalf("second slave = %#v", slaves[1])
	}
}

func TestLoadRejectsInvalidEtherCATSlaveCriticality(t *testing.T) {
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
			"can": {"enabled": false, "backend": "socketcan", "interfaces": []},
			"ethercat": {
				"enabled": true,
				"backend": "igh",
				"masters": [
					{
						"name": "master0",
						"expected_state": "op",
						"slaves": [
							{"position": 1, "name": "left_hip", "criticality": "nice_to_have"}
						]
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
	if !strings.Contains(err.Error(), "sources.ethercat.masters[0].slaves[0].criticality") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRequiresNetworkInterfacesWhenEnabled(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"sources": {
			"network": {
				"enabled": true,
				"interfaces": []
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
	if !strings.Contains(err.Error(), "sources.network.interfaces") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRequiresPowerSuppliesWhenEnabled(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"sources": {
			"power": {
				"enabled": true,
				"supplies": []
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
	if !strings.Contains(err.Error(), "sources.power.supplies") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRequiresStorageMountsWhenEnabled(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"sources": {
			"storage": {
				"enabled": true,
				"mounts": []
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
	if !strings.Contains(err.Error(), "sources.storage.mounts") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadDefaultsSourceIDsForMandatoryInfraSources(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"sources": {
			"network": {
				"enabled": true,
				"interfaces": [
					{
						"name": "enp12s0",
						"source_id": "",
						"require_up": true,
						"min_speed_mbps": 1000
					}
				]
			},
			"power": {
				"enabled": true,
				"supplies": [
					{
						"name": "BAT0",
						"source_id": "",
						"require_present": true
					}
				]
			},
			"storage": {
				"enabled": true,
				"mounts": [
					{
						"path": "/var/log",
						"source_id": "",
						"require_writable": true
					}
				]
			},
			"time_sync": {
				"enabled": true,
				"source_id": "",
				"require_synchronized": true,
				"warn_on_local_rtc": true
			}
		},
		"rules": {}
	}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if got := cfg.Sources.Network.Interfaces[0].SourceID; got != "enp12s0" {
		t.Fatalf("network source_id = %q, want enp12s0", got)
	}
	if got := cfg.Sources.Power.Supplies[0].SourceID; got != "BAT0" {
		t.Fatalf("power source_id = %q, want BAT0", got)
	}
	if got := cfg.Sources.Storage.Mounts[0].SourceID; got != "var-log" {
		t.Fatalf("storage source_id = %q, want var-log", got)
	}
	if got := cfg.Sources.TimeSync.SourceID; got != "system-clock" {
		t.Fatalf("time_sync source_id = %q, want system-clock", got)
	}
}

func TestLoadParsesTimeSyncGracePeriod(t *testing.T) {
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
			"can": {"enabled": false, "backend": "socketcan", "interfaces": []},
			"ethercat": {"enabled": false, "backend": "igh", "masters": []},
			"network": {"enabled": false, "interfaces": []},
			"power": {"enabled": false, "supplies": []},
			"storage": {"enabled": false, "mounts": []},
			"time_sync": {
				"enabled": true,
				"source_id": "system-clock",
				"require_synchronized": true,
				"warn_on_local_rtc": true,
				"sync_grace_period": "15m"
			}
		},
		"rules": {}
	}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Sources.TimeSync.SyncGracePeriod != 15*time.Minute {
		t.Fatalf("time_sync sync_grace_period = %s, want 15m", cfg.Sources.TimeSync.SyncGracePeriod)
	}
}

func TestLoadParsesRawLogsConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"raw_logs": {
			"enabled": true,
			"manifest_dir": "./var/lib/watchdog/logs/manifests",
			"incident_index_dir": "./var/lib/watchdog/logs/incident-index",
			"pre_window": "45s",
			"post_window": "15s"
		},
		"sources": {
			"host": {"enabled": false},
			"module_reports": {
				"enabled": false,
				"socket_path": "./var/run/watchdog/module.sock",
				"max_message_bytes": 4096,
				"default_stale_after": "5s"
			}
		},
		"rules": {}
	}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.RawLogs.Enabled {
		t.Fatal("raw logs not enabled")
	}
	if cfg.RawLogs.ManifestDir != "./var/lib/watchdog/logs/manifests" {
		t.Fatalf("manifest_dir = %q", cfg.RawLogs.ManifestDir)
	}
	if cfg.RawLogs.IncidentIndexDir != "./var/lib/watchdog/logs/incident-index" {
		t.Fatalf("incident_index_dir = %q", cfg.RawLogs.IncidentIndexDir)
	}
	if cfg.RawLogs.PreWindow != 45*time.Second || cfg.RawLogs.PostWindow != 15*time.Second {
		t.Fatalf("raw log windows = %s/%s", cfg.RawLogs.PreWindow, cfg.RawLogs.PostWindow)
	}
}

func TestLoadRejectsEnabledRawLogsWithoutManifestDir(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"raw_logs": {
			"enabled": true,
			"manifest_dir": "",
			"incident_index_dir": "./var/lib/watchdog/logs/incident-index",
			"pre_window": "30s",
			"post_window": "30s"
		},
		"sources": {
			"host": {"enabled": false},
			"module_reports": {
				"enabled": false,
				"socket_path": "./var/run/watchdog/module.sock",
				"max_message_bytes": 4096,
				"default_stale_after": "5s"
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
	if !strings.Contains(err.Error(), "raw_logs.manifest_dir") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRejectsNegativeRawLogWindow(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"raw_logs": {
			"enabled": true,
			"manifest_dir": "./var/lib/watchdog/logs/manifests",
			"incident_index_dir": "./var/lib/watchdog/logs/incident-index",
			"pre_window": "-1s",
			"post_window": "30s"
		},
		"sources": {
			"host": {"enabled": false},
			"module_reports": {
				"enabled": false,
				"socket_path": "./var/run/watchdog/module.sock",
				"max_message_bytes": 4096,
				"default_stale_after": "5s"
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
	if !strings.Contains(err.Error(), "raw_logs.pre_window") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadParsesModuleRules(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"sources": {
			"host": {"enabled": false},
			"module_reports": {
				"enabled": true,
				"socket_path": "./var/run/watchdog/module.sock",
				"max_message_bytes": 4096,
				"default_stale_after": "5s"
			},
			"systemd": {"enabled": false, "units": []},
			"can": {"enabled": false, "backend": "socketcan", "interfaces": []},
			"ethercat": {"enabled": false, "backend": "igh", "masters": []},
			"network": {"enabled": false, "interfaces": []},
			"power": {"enabled": false, "supplies": []},
			"storage": {"enabled": false, "mounts": []},
			"time_sync": {
				"enabled": false,
				"source_id": "system-clock",
				"require_synchronized": true,
				"warn_on_local_rtc": true,
				"sync_grace_period": "10m"
			}
		},
		"rules": {
			"module": {
				"control_period_warn_us": 2000,
				"control_period_fail_us": 5000,
				"drive_current_ratio_warn": 0.8,
				"drive_current_ratio_fail": 1.0,
				"drive_motor_temp_warn_c": 80,
				"drive_motor_temp_fail_c": 95,
				"drive_bus_voltage_min_warn_v": 42,
				"drive_bus_voltage_min_fail_v": 36,
				"drive_fault_code_fail": true
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Rules.Module.ControlPeriodWarnUS != 2000 {
		t.Fatalf("control_period_warn_us = %f, want 2000", cfg.Rules.Module.ControlPeriodWarnUS)
	}
	if cfg.Rules.Module.ControlPeriodFailUS != 5000 {
		t.Fatalf("control_period_fail_us = %f, want 5000", cfg.Rules.Module.ControlPeriodFailUS)
	}
	if cfg.Rules.Module.ControlPeriodWarnConsecutive != 3 {
		t.Fatalf("control_period_warn_consecutive = %d, want 3", cfg.Rules.Module.ControlPeriodWarnConsecutive)
	}
	if cfg.Rules.Module.ControlPeriodFailConsecutive != 5 {
		t.Fatalf("control_period_fail_consecutive = %d, want 5", cfg.Rules.Module.ControlPeriodFailConsecutive)
	}
	if cfg.Rules.Module.ControlPeriodRecoverConsecutive != 3 {
		t.Fatalf("control_period_recover_consecutive = %d, want 3", cfg.Rules.Module.ControlPeriodRecoverConsecutive)
	}
	if cfg.Rules.Module.DriveCurrentRatioWarn != 0.8 || cfg.Rules.Module.DriveCurrentRatioFail != 1.0 {
		t.Fatalf("drive current ratio rules = %f/%f", cfg.Rules.Module.DriveCurrentRatioWarn, cfg.Rules.Module.DriveCurrentRatioFail)
	}
	if cfg.Rules.Module.DriveMotorTempWarnC != 80 || cfg.Rules.Module.DriveMotorTempFailC != 95 {
		t.Fatalf("drive motor temp rules = %f/%f", cfg.Rules.Module.DriveMotorTempWarnC, cfg.Rules.Module.DriveMotorTempFailC)
	}
	if cfg.Rules.Module.DriveBusVoltageMinWarnV != 42 || cfg.Rules.Module.DriveBusVoltageMinFailV != 36 {
		t.Fatalf("drive bus voltage rules = %f/%f", cfg.Rules.Module.DriveBusVoltageMinWarnV, cfg.Rules.Module.DriveBusVoltageMinFailV)
	}
	if !cfg.Rules.Module.DriveFaultCodeFail {
		t.Fatal("drive_fault_code_fail = false, want true")
	}
}

func TestLoadRejectsInvalidModuleRuleOrder(t *testing.T) {
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
			}
		},
		"rules": {
			"module": {
				"control_period_warn_us": 5000,
				"control_period_fail_us": 2000
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "rules.module.control_period_fail_us") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigDefaultsIncidentRetention(t *testing.T) {
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
			}
		},
		"rules": {}
	}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Retention.Incidents.MaxFiles != 1000 || cfg.Retention.Incidents.MinKeep != 50 {
		t.Fatalf("incident retention defaults wrong: %+v", cfg.Retention.Incidents)
	}
	if cfg.Retention.SweepInterval != time.Minute {
		t.Fatalf("SweepInterval = %v, want 1m", cfg.Retention.SweepInterval)
	}
}

func TestParseIncidentPolicyRejectsNegativeLimits(t *testing.T) {
	tests := []struct {
		name string
		raw  fileIncidentPolicyConfig
		want string
	}{
		{
			name: "max files",
			raw:  fileIncidentPolicyConfig{MaxFiles: -1},
			want: "max_files must not be negative",
		},
		{
			name: "min keep",
			raw:  fileIncidentPolicyConfig{MinKeep: -1},
			want: "min_keep must not be negative",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseIncidentPolicy(tt.raw)
			if err == nil {
				t.Fatal("parseIncidentPolicy unexpectedly succeeded")
			}
			if err.Error() != tt.want {
				t.Fatalf("error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestLoadParsesMetricsEndpoint(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "watchdog.json")
	data := `{
		"poll_interval": "2s",
		"incident_dir": "./var/incidents",
		"metrics": {
			"enabled": true,
			"listen_address": "127.0.0.1:9108",
			"path": "/metrics"
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
			"ethercat": {"enabled": false, "backend": "igh", "masters": []},
			"network": {"enabled": false, "interfaces": []},
			"power": {"enabled": false, "supplies": []},
			"storage": {"enabled": false, "mounts": []},
			"time_sync": {
				"enabled": false,
				"source_id": "system-clock",
				"require_synchronized": true,
				"warn_on_local_rtc": true,
				"sync_grace_period": "10m"
			}
		},
		"rules": {}
	}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.Metrics.Enabled || cfg.Metrics.ListenAddress != "127.0.0.1:9108" || cfg.Metrics.Path != "/metrics" {
		t.Fatalf("metrics = %+v", cfg.Metrics)
	}
}
