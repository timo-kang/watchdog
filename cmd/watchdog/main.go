package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"watchdog/internal/actions"
	"watchdog/internal/adapters"
	"watchdog/internal/adapters/can"
	"watchdog/internal/adapters/ethercat"
	"watchdog/internal/adapters/host"
	"watchdog/internal/adapters/module"
	"watchdog/internal/adapters/network"
	"watchdog/internal/adapters/power"
	"watchdog/internal/adapters/storage"
	"watchdog/internal/adapters/systemd"
	"watchdog/internal/adapters/timesync"
	"watchdog/internal/app"
	"watchdog/internal/config"
	"watchdog/internal/incident"
	"watchdog/internal/rules"
)

func main() {
	configPath := flag.String("config", "./configs/watchdog.example.json", "path to watchdog config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	logger := log.New(os.Stdout, "watchdog ", log.LstdFlags|log.Lmicroseconds)

	var collectors []adapters.Collector
	if cfg.Sources.Host.Enabled {
		collectors = append(collectors, host.New(cfg.Sources.Host))
	}
	if cfg.Sources.ModuleReports.Enabled {
		collectors = append(collectors, module.New(cfg.Sources.ModuleReports))
	}
	if cfg.Sources.Systemd.Enabled {
		collectors = append(collectors, systemd.New(cfg.Sources.Systemd))
	}
	if cfg.Sources.CAN.Enabled {
		collectors = append(collectors, can.New(cfg.Sources.CAN))
	}
	if cfg.Sources.EtherCAT.Enabled {
		collectors = append(collectors, ethercat.New(cfg.Sources.EtherCAT))
	}
	if cfg.Sources.Network.Enabled {
		collectors = append(collectors, network.New(cfg.Sources.Network))
	}
	if cfg.Sources.Power.Enabled {
		collectors = append(collectors, power.New(cfg.Sources.Power))
	}
	if cfg.Sources.Storage.Enabled {
		collectors = append(collectors, storage.New(cfg.Sources.Storage))
	}
	if cfg.Sources.TimeSync.Enabled {
		collectors = append(collectors, timesync.New(cfg.Sources.TimeSync))
	}

	if len(collectors) == 0 {
		logger.Fatal("no collectors enabled")
	}

	evaluator := rules.New(cfg.Rules)
	incidentWriter := incident.New(cfg.IncidentDir)
	actionSink := actions.NewMultiSink(
		actions.NewTransitionLogger(logger, cfg.LogTransitionsOnly),
		buildSocketSink(cfg),
	)
	daemon := app.New(logger, cfg.PollInterval, collectors, evaluator, incidentWriter, actionSink)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := daemon.Run(ctx); err != nil {
		logger.Fatalf("run watchdog: %v", err)
	}
}

func buildSocketSink(cfg config.Config) actions.Sink {
	if !cfg.Actions.UnixSocket.Enabled {
		return nil
	}
	return actions.NewUnixDatagramSink(
		cfg.Actions.UnixSocket.SocketPath,
		cfg.Actions.UnixSocket.SendResolved,
		cfg.Actions.UnixSocket.SpoolDir,
		cfg.Actions.UnixSocket.ReplayBatchSize,
	)
}
