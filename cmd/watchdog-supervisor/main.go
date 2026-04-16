package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

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
	server := supervisor.NewServer(logger, cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := server.Run(ctx); err != nil {
		logger.Fatalf("run supervisor: %v", err)
	}
}
