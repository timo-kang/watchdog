package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"

	"watchdog/internal/metrics"
	"watchdog/internal/supervisor"
)

func main() {
	configPath := flag.String("config", "./configs/watchdog-supervisor.example.json", "path to supervisor config")
	flag.Parse()

	cfg, err := supervisor.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("load supervisor config: %v", err)
	}

	logger := log.New(os.Stdout, "watchdog-supervisor ", log.LstdFlags|log.Lmicroseconds)
	registry := prometheus.NewRegistry()
	supervisorMetrics := metrics.NewSupervisorCollector()
	registry.MustRegister(
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
		supervisorMetrics,
	)
	server := supervisor.NewServer(logger, cfg, supervisorMetrics)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	errCh := make(chan error, 2)
	go func() {
		errCh <- server.Run(ctx)
	}()
	if cfg.Metrics.Enabled {
		go func() {
			errCh <- metrics.Serve(ctx, logger, "watchdog-supervisor", cfg.Metrics, registry)
		}()
	}

	if err := <-errCh; err != nil {
		logger.Fatalf("run supervisor: %v", err)
	}
}
