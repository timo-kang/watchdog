package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	PollInterval       time.Duration
	IncidentDir        string
	LogTransitionsOnly bool
	Actions            ActionsConfig
	Sources            SourcesConfig
	Rules              RulesConfig
}

type fileConfig struct {
	PollInterval       string        `json:"poll_interval"`
	IncidentDir        string        `json:"incident_dir"`
	LogTransitionsOnly bool          `json:"log_transitions_only"`
	Actions            ActionsConfig `json:"actions"`
	Sources            fileSources   `json:"sources"`
	Rules              RulesConfig   `json:"rules"`
}

type ActionsConfig struct {
	UnixSocket UnixSocketActionConfig `json:"unix_socket"`
}

type UnixSocketActionConfig struct {
	Enabled         bool   `json:"enabled"`
	SocketPath      string `json:"socket_path"`
	SendResolved    bool   `json:"send_resolved"`
	SpoolDir        string `json:"spool_dir"`
	ReplayBatchSize int    `json:"replay_batch_size"`
}

type SourcesConfig struct {
	Host          HostSourceConfig         `json:"host"`
	ModuleReports ModuleReportSourceConfig `json:"module_reports"`
	Systemd       SystemdSourceConfig      `json:"systemd"`
	CAN           CANSourceConfig          `json:"can"`
	EtherCAT      EtherCATSourceConfig     `json:"ethercat"`
	Network       NetworkSourceConfig      `json:"network"`
	Power         PowerSourceConfig        `json:"power"`
	Storage       StorageSourceConfig      `json:"storage"`
	TimeSync      TimeSyncSourceConfig     `json:"time_sync"`
}

type HostSourceConfig struct {
	Enabled bool `json:"enabled"`
}

type ModuleReportSourceConfig struct {
	Enabled           bool
	SocketPath        string
	MaxMessageBytes   int
	DefaultStaleAfter time.Duration
}

type SystemdSourceConfig struct {
	Enabled bool
	Units   []SystemdUnitConfig
}

type SystemdUnitConfig struct {
	Name           string `json:"name"`
	SourceID       string `json:"source_id"`
	RequireMainPID bool   `json:"require_main_pid"`
}

type CANSourceConfig struct {
	Enabled    bool                 `json:"enabled"`
	Backend    string               `json:"backend"`
	Interfaces []CANInterfaceConfig `json:"interfaces"`
}

type CANInterfaceConfig struct {
	Name            string          `json:"name"`
	SourceID        string          `json:"source_id"`
	ExpectedBitrate int             `json:"expected_bitrate"`
	RequireUp       bool            `json:"require_up"`
	ProbeCommand    []string        `json:"probe_command"`
	ExpectedNodes   []CANNodeConfig `json:"expected_nodes"`
}

type CANNodeConfig struct {
	Name string `json:"name"`
	ID   uint32 `json:"id"`
}

type EtherCATSourceConfig struct {
	Enabled bool                   `json:"enabled"`
	Backend string                 `json:"backend"`
	Masters []EtherCATMasterConfig `json:"masters"`
}

type EtherCATMasterConfig struct {
	Name           string   `json:"name"`
	SourceID       string   `json:"source_id"`
	ExpectedState  string   `json:"expected_state"`
	ExpectedSlaves int      `json:"expected_slaves"`
	RequireLink    bool     `json:"require_link"`
	ProbeCommand   []string `json:"probe_command"`
}

type NetworkSourceConfig struct {
	Enabled    bool                     `json:"enabled"`
	Interfaces []NetworkInterfaceConfig `json:"interfaces"`
}

type NetworkInterfaceConfig struct {
	Name         string `json:"name"`
	SourceID     string `json:"source_id"`
	RequireUp    bool   `json:"require_up"`
	MinSpeedMbps int    `json:"min_speed_mbps"`
}

type PowerSourceConfig struct {
	Enabled  bool                `json:"enabled"`
	Supplies []PowerSupplyConfig `json:"supplies"`
}

type PowerSupplyConfig struct {
	Name           string `json:"name"`
	SourceID       string `json:"source_id"`
	RequirePresent bool   `json:"require_present"`
	RequireOnline  bool   `json:"require_online"`
}

type StorageSourceConfig struct {
	Enabled bool                 `json:"enabled"`
	Mounts  []StorageMountConfig `json:"mounts"`
}

type StorageMountConfig struct {
	Path            string `json:"path"`
	SourceID        string `json:"source_id"`
	Device          string `json:"device"`
	RequireWritable bool   `json:"require_writable"`
}

type TimeSyncSourceConfig struct {
	Enabled             bool
	SourceID            string
	RequireSynchronized bool
	WarnOnLocalRTC      bool
	SyncGracePeriod     time.Duration
}

type fileTimeSyncSourceConfig struct {
	Enabled             bool   `json:"enabled"`
	SourceID            string `json:"source_id"`
	RequireSynchronized bool   `json:"require_synchronized"`
	WarnOnLocalRTC      bool   `json:"warn_on_local_rtc"`
	SyncGracePeriod     string `json:"sync_grace_period"`
}

type fileSources struct {
	Host          HostSourceConfig         `json:"host"`
	ModuleReports fileModuleReportSource   `json:"module_reports"`
	Systemd       fileSystemdSource        `json:"systemd"`
	CAN           CANSourceConfig          `json:"can"`
	EtherCAT      EtherCATSourceConfig     `json:"ethercat"`
	Network       NetworkSourceConfig      `json:"network"`
	Power         PowerSourceConfig        `json:"power"`
	Storage       StorageSourceConfig      `json:"storage"`
	TimeSync      fileTimeSyncSourceConfig `json:"time_sync"`
}

type fileModuleReportSource struct {
	Enabled           bool   `json:"enabled"`
	SocketPath        string `json:"socket_path"`
	MaxMessageBytes   int    `json:"max_message_bytes"`
	DefaultStaleAfter string `json:"default_stale_after"`
}

type fileSystemdSource struct {
	Enabled bool                `json:"enabled"`
	Units   []SystemdUnitConfig `json:"units"`
}

type RulesConfig struct {
	Host     HostRules     `json:"host"`
	Process  ProcessRules  `json:"process"`
	CAN      CANRules      `json:"can"`
	EtherCAT EtherCATRules `json:"ethercat"`
	Network  NetworkRules  `json:"network"`
	Power    PowerRules    `json:"power"`
	Storage  StorageRules  `json:"storage"`
	TimeSync TimeSyncRules `json:"time_sync"`
}

type HostRules struct {
	MaxCPUTempWarnC        float64 `json:"max_cpu_temp_warn_c"`
	MaxCPUTempCriticalC    float64 `json:"max_cpu_temp_critical_c"`
	MaxTempWarnC           float64 `json:"max_temp_warn_c"`
	MaxTempCriticalC       float64 `json:"max_temp_critical_c"`
	MemAvailableWarnMB     float64 `json:"mem_available_warn_mb"`
	MemAvailableCriticalMB float64 `json:"mem_available_critical_mb"`
	LoadRatioWarn          float64 `json:"load_ratio_warn"`
	LoadRatioCritical      float64 `json:"load_ratio_critical"`
}

type ProcessRules struct {
	RestartWarn uint64 `json:"restart_warn"`
	RestartFail uint64 `json:"restart_fail"`
}

type CANRules struct {
	MissingNodesWarn int    `json:"missing_nodes_warn"`
	MissingNodesFail int    `json:"missing_nodes_fail"`
	RestartWarn      uint64 `json:"restart_warn"`
	RestartFail      uint64 `json:"restart_fail"`
}

type EtherCATRules struct {
	MissingSlavesWarn int     `json:"missing_slaves_warn"`
	MissingSlavesFail int     `json:"missing_slaves_fail"`
	WKCWarnRatio      float64 `json:"wkc_warn_ratio"`
	WKCFailRatio      float64 `json:"wkc_fail_ratio"`
}

type NetworkRules struct {
	ErrorDeltaWarn float64 `json:"error_delta_warn"`
	DropDeltaWarn  float64 `json:"drop_delta_warn"`
}

type PowerRules struct {
	CapacityWarnPct float64 `json:"capacity_warn_pct"`
	CapacityFailPct float64 `json:"capacity_fail_pct"`
	TempWarnC       float64 `json:"temp_warn_c"`
	TempFailC       float64 `json:"temp_fail_c"`
}

type StorageRules struct {
	UsedPercentWarn float64 `json:"used_percent_warn"`
	UsedPercentFail float64 `json:"used_percent_fail"`
	AvailWarnMB     float64 `json:"avail_warn_mb"`
	AvailFailMB     float64 `json:"avail_fail_mb"`
	BusyPercentWarn float64 `json:"busy_percent_warn"`
	BusyPercentFail float64 `json:"busy_percent_fail"`
}

type TimeSyncRules struct {
	RTCDeltaWarnS float64 `json:"rtc_delta_warn_s"`
	RTCDeltaFailS float64 `json:"rtc_delta_fail_s"`
}

func Load(path string) (Config, error) {
	raw := fileConfig{
		PollInterval:       "2s",
		IncidentDir:        "./var/incidents",
		LogTransitionsOnly: true,
		Actions: ActionsConfig{
			UnixSocket: UnixSocketActionConfig{
				Enabled:         false,
				SocketPath:      "./var/run/watchdog/supervisor.sock",
				SendResolved:    true,
				SpoolDir:        "./var/spool/watchdog/actions",
				ReplayBatchSize: 64,
			},
		},
		Sources: fileSources{
			Host: HostSourceConfig{Enabled: true},
			ModuleReports: fileModuleReportSource{
				Enabled:           false,
				SocketPath:        "./var/run/watchdog/module.sock",
				MaxMessageBytes:   4096,
				DefaultStaleAfter: "5s",
			},
			Systemd: fileSystemdSource{
				Enabled: false,
				Units:   nil,
			},
			CAN: CANSourceConfig{
				Enabled: false,
				Backend: "socketcan",
				Interfaces: []CANInterfaceConfig{
					{
						Name:            "can0",
						SourceID:        "drive-can",
						ExpectedBitrate: 1000000,
						RequireUp:       true,
						ProbeCommand:    nil,
						ExpectedNodes: []CANNodeConfig{
							{Name: "left_drive", ID: 1},
							{Name: "right_drive", ID: 2},
						},
					},
				},
			},
			EtherCAT: EtherCATSourceConfig{
				Enabled: false,
				Backend: "igh",
				Masters: []EtherCATMasterConfig{
					{
						Name:           "master0",
						SourceID:       "actuators",
						ExpectedState:  "op",
						ExpectedSlaves: 12,
						RequireLink:    true,
						ProbeCommand:   nil,
					},
				},
			},
			Network: NetworkSourceConfig{
				Enabled: false,
				Interfaces: []NetworkInterfaceConfig{
					{
						Name:         "eth0",
						SourceID:     "uplink",
						RequireUp:    true,
						MinSpeedMbps: 100,
					},
				},
			},
			Power: PowerSourceConfig{
				Enabled: false,
				Supplies: []PowerSupplyConfig{
					{
						Name:           "BAT0",
						SourceID:       "main-battery",
						RequirePresent: true,
						RequireOnline:  false,
					},
				},
			},
			Storage: StorageSourceConfig{
				Enabled: false,
				Mounts: []StorageMountConfig{
					{
						Path:            "/",
						SourceID:        "rootfs",
						RequireWritable: true,
					},
				},
			},
			TimeSync: fileTimeSyncSourceConfig{
				Enabled:             false,
				SourceID:            "system-clock",
				RequireSynchronized: true,
				WarnOnLocalRTC:      true,
				SyncGracePeriod:     "10m",
			},
		},
		Rules: RulesConfig{
			Host: HostRules{
				MaxCPUTempWarnC:        85,
				MaxCPUTempCriticalC:    95,
				MaxTempWarnC:           90,
				MaxTempCriticalC:       100,
				MemAvailableWarnMB:     1024,
				MemAvailableCriticalMB: 512,
				LoadRatioWarn:          1.0,
				LoadRatioCritical:      1.5,
			},
			Process: ProcessRules{
				RestartWarn: 1,
				RestartFail: 3,
			},
			CAN: CANRules{
				MissingNodesWarn: 1,
				MissingNodesFail: 2,
				RestartWarn:      1,
				RestartFail:      3,
			},
			EtherCAT: EtherCATRules{
				MissingSlavesWarn: 1,
				MissingSlavesFail: 2,
				WKCWarnRatio:      0.95,
				WKCFailRatio:      0.80,
			},
			Network: NetworkRules{
				ErrorDeltaWarn: 1,
				DropDeltaWarn:  1,
			},
			Power: PowerRules{
				CapacityWarnPct: 30,
				CapacityFailPct: 15,
				TempWarnC:       50,
				TempFailC:       60,
			},
			Storage: StorageRules{
				UsedPercentWarn: 85,
				UsedPercentFail: 95,
				AvailWarnMB:     2048,
				AvailFailMB:     512,
				BusyPercentWarn: 90,
				BusyPercentFail: 98,
			},
			TimeSync: TimeSyncRules{
				RTCDeltaWarnS: 30,
				RTCDeltaFailS: 120,
			},
		},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	interval, err := time.ParseDuration(raw.PollInterval)
	if err != nil {
		return Config{}, fmt.Errorf("parse poll_interval: %w", err)
	}
	if interval <= 0 {
		return Config{}, fmt.Errorf("poll_interval must be positive")
	}

	if raw.IncidentDir == "" {
		return Config{}, fmt.Errorf("incident_dir must not be empty")
	}
	if strings.TrimSpace(raw.Sources.TimeSync.SyncGracePeriod) == "" {
		raw.Sources.TimeSync.SyncGracePeriod = "10m"
	}
	if raw.Actions.UnixSocket.Enabled && raw.Actions.UnixSocket.SocketPath == "" {
		return Config{}, fmt.Errorf("actions.unix_socket.socket_path must not be empty when enabled")
	}
	if raw.Actions.UnixSocket.Enabled && raw.Actions.UnixSocket.SpoolDir == "" {
		return Config{}, fmt.Errorf("actions.unix_socket.spool_dir must not be empty when enabled")
	}
	if raw.Actions.UnixSocket.Enabled && raw.Actions.UnixSocket.ReplayBatchSize <= 0 {
		return Config{}, fmt.Errorf("actions.unix_socket.replay_batch_size must be positive when enabled")
	}

	moduleStaleAfter, err := time.ParseDuration(raw.Sources.ModuleReports.DefaultStaleAfter)
	if err != nil {
		return Config{}, fmt.Errorf("parse sources.module_reports.default_stale_after: %w", err)
	}
	if moduleStaleAfter <= 0 {
		return Config{}, fmt.Errorf("sources.module_reports.default_stale_after must be positive")
	}

	if raw.Sources.ModuleReports.MaxMessageBytes <= 0 {
		return Config{}, fmt.Errorf("sources.module_reports.max_message_bytes must be positive")
	}
	timeSyncGracePeriod, err := time.ParseDuration(raw.Sources.TimeSync.SyncGracePeriod)
	if err != nil {
		return Config{}, fmt.Errorf("parse sources.time_sync.sync_grace_period: %w", err)
	}
	if timeSyncGracePeriod < 0 {
		return Config{}, fmt.Errorf("sources.time_sync.sync_grace_period must be >= 0")
	}

	sources := SourcesConfig{
		Host: raw.Sources.Host,
		ModuleReports: ModuleReportSourceConfig{
			Enabled:           raw.Sources.ModuleReports.Enabled,
			SocketPath:        raw.Sources.ModuleReports.SocketPath,
			MaxMessageBytes:   raw.Sources.ModuleReports.MaxMessageBytes,
			DefaultStaleAfter: moduleStaleAfter,
		},
		Systemd: SystemdSourceConfig{
			Enabled: raw.Sources.Systemd.Enabled,
			Units:   append([]SystemdUnitConfig(nil), raw.Sources.Systemd.Units...),
		},
		CAN: CANSourceConfig{
			Enabled:    raw.Sources.CAN.Enabled,
			Backend:    raw.Sources.CAN.Backend,
			Interfaces: append([]CANInterfaceConfig(nil), raw.Sources.CAN.Interfaces...),
		},
		EtherCAT: EtherCATSourceConfig{
			Enabled: raw.Sources.EtherCAT.Enabled,
			Backend: raw.Sources.EtherCAT.Backend,
			Masters: append([]EtherCATMasterConfig(nil), raw.Sources.EtherCAT.Masters...),
		},
		Network: NetworkSourceConfig{
			Enabled:    raw.Sources.Network.Enabled,
			Interfaces: append([]NetworkInterfaceConfig(nil), raw.Sources.Network.Interfaces...),
		},
		Power: PowerSourceConfig{
			Enabled:  raw.Sources.Power.Enabled,
			Supplies: append([]PowerSupplyConfig(nil), raw.Sources.Power.Supplies...),
		},
		Storage: StorageSourceConfig{
			Enabled: raw.Sources.Storage.Enabled,
			Mounts:  append([]StorageMountConfig(nil), raw.Sources.Storage.Mounts...),
		},
		TimeSync: TimeSyncSourceConfig{
			Enabled:             raw.Sources.TimeSync.Enabled,
			SourceID:            raw.Sources.TimeSync.SourceID,
			RequireSynchronized: raw.Sources.TimeSync.RequireSynchronized,
			WarnOnLocalRTC:      raw.Sources.TimeSync.WarnOnLocalRTC,
			SyncGracePeriod:     timeSyncGracePeriod,
		},
	}

	if sources.ModuleReports.Enabled && sources.ModuleReports.SocketPath == "" {
		return Config{}, fmt.Errorf("sources.module_reports.socket_path must not be empty when enabled")
	}
	if sources.Systemd.Enabled && len(sources.Systemd.Units) == 0 {
		return Config{}, fmt.Errorf("sources.systemd.units must not be empty when enabled")
	}
	for i, unit := range sources.Systemd.Units {
		if unit.Name == "" {
			return Config{}, fmt.Errorf("sources.systemd.units[%d].name must not be empty", i)
		}
		if unit.SourceID == "" {
			sources.Systemd.Units[i].SourceID = unit.Name
		}
	}
	if sources.CAN.Enabled && sources.CAN.Backend == "" {
		return Config{}, fmt.Errorf("sources.can.backend must not be empty when enabled")
	}
	if sources.CAN.Enabled && len(sources.CAN.Interfaces) == 0 {
		return Config{}, fmt.Errorf("sources.can.interfaces must not be empty when enabled")
	}
	for i, iface := range sources.CAN.Interfaces {
		if iface.Name == "" {
			return Config{}, fmt.Errorf("sources.can.interfaces[%d].name must not be empty", i)
		}
		if iface.SourceID == "" {
			sources.CAN.Interfaces[i].SourceID = iface.Name
		}
		if iface.ExpectedBitrate < 0 {
			return Config{}, fmt.Errorf("sources.can.interfaces[%d].expected_bitrate must be >= 0", i)
		}
		if backendRequiresProbeCommand(sources.CAN.Backend) && len(iface.ProbeCommand) == 0 {
			return Config{}, fmt.Errorf("sources.can.interfaces[%d].probe_command must not be empty for backend %q", i, sources.CAN.Backend)
		}
	}
	if sources.EtherCAT.Enabled && sources.EtherCAT.Backend == "" {
		return Config{}, fmt.Errorf("sources.ethercat.backend must not be empty when enabled")
	}
	if sources.EtherCAT.Enabled && len(sources.EtherCAT.Masters) == 0 {
		return Config{}, fmt.Errorf("sources.ethercat.masters must not be empty when enabled")
	}
	for i, master := range sources.EtherCAT.Masters {
		if master.Name == "" {
			return Config{}, fmt.Errorf("sources.ethercat.masters[%d].name must not be empty", i)
		}
		if master.SourceID == "" {
			sources.EtherCAT.Masters[i].SourceID = master.Name
		}
		if master.ExpectedState == "" {
			sources.EtherCAT.Masters[i].ExpectedState = "op"
		}
		if master.ExpectedSlaves < 0 {
			return Config{}, fmt.Errorf("sources.ethercat.masters[%d].expected_slaves must be >= 0", i)
		}
		if backendRequiresProbeCommand(sources.EtherCAT.Backend) && len(master.ProbeCommand) == 0 {
			return Config{}, fmt.Errorf("sources.ethercat.masters[%d].probe_command must not be empty for backend %q", i, sources.EtherCAT.Backend)
		}
	}
	if sources.Network.Enabled && len(sources.Network.Interfaces) == 0 {
		return Config{}, fmt.Errorf("sources.network.interfaces must not be empty when enabled")
	}
	for i, iface := range sources.Network.Interfaces {
		if iface.Name == "" {
			return Config{}, fmt.Errorf("sources.network.interfaces[%d].name must not be empty", i)
		}
		if iface.SourceID == "" {
			sources.Network.Interfaces[i].SourceID = iface.Name
		}
		if iface.MinSpeedMbps < 0 {
			return Config{}, fmt.Errorf("sources.network.interfaces[%d].min_speed_mbps must be >= 0", i)
		}
	}
	if sources.Power.Enabled && len(sources.Power.Supplies) == 0 {
		return Config{}, fmt.Errorf("sources.power.supplies must not be empty when enabled")
	}
	for i, supply := range sources.Power.Supplies {
		if supply.Name == "" {
			return Config{}, fmt.Errorf("sources.power.supplies[%d].name must not be empty", i)
		}
		if supply.SourceID == "" {
			sources.Power.Supplies[i].SourceID = supply.Name
		}
	}
	if sources.Storage.Enabled && len(sources.Storage.Mounts) == 0 {
		return Config{}, fmt.Errorf("sources.storage.mounts must not be empty when enabled")
	}
	for i, mount := range sources.Storage.Mounts {
		if mount.Path == "" {
			return Config{}, fmt.Errorf("sources.storage.mounts[%d].path must not be empty", i)
		}
		if mount.SourceID == "" {
			sources.Storage.Mounts[i].SourceID = sanitizeSourceID(mount.Path)
		}
	}
	if sources.TimeSync.Enabled && sources.TimeSync.SourceID == "" {
		sources.TimeSync.SourceID = "system-clock"
	}

	return Config{
		PollInterval:       interval,
		IncidentDir:        raw.IncidentDir,
		LogTransitionsOnly: raw.LogTransitionsOnly,
		Actions:            raw.Actions,
		Sources:            sources,
		Rules:              raw.Rules,
	}, nil
}

func sanitizeSourceID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return "rootfs"
	}
	value = strings.TrimPrefix(value, "/")
	value = strings.ReplaceAll(value, "/", "-")
	return value
}

func backendRequiresProbeCommand(backend string) bool {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "command-json", "command_json", "cmdjson", "soem":
		return true
	default:
		return false
	}
}
