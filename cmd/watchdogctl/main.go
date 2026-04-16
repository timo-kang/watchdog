package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"watchdog/internal/actions"
	"watchdog/internal/supervisor"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "status":
		runStatus(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		log.Fatalf("unknown subcommand %q", os.Args[1])
	}
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	configPath := fs.String("config", "./configs/watchdog-supervisor.example.json", "path to supervisor config")
	jsonOutput := fs.Bool("json", false, "print machine-readable JSON")
	fs.Parse(args)

	cfg, err := supervisor.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("load supervisor config: %v", err)
	}

	status, err := supervisor.LoadStatus(cfg.StatePath, cfg.LatestPath)
	if err != nil {
		log.Fatalf("load supervisor status: %v", err)
	}

	if *jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(status); err != nil {
			log.Fatalf("encode status: %v", err)
		}
		return
	}

	printStatus(os.Stdout, status)
}

func printStatus(w *os.File, status supervisor.StatusView) {
	fmt.Fprintf(w, "Overall action: %s\n", emptyAs(status.State.OverallAction, "none"))
	if !status.State.UpdatedAt.IsZero() {
		fmt.Fprintf(w, "State updated: %s\n", status.State.UpdatedAt.Format("2006-01-02 15:04:05Z07:00"))
	}
	if status.State.LastRequestID != "" {
		fmt.Fprintf(w, "Last request: %s\n", status.State.LastRequestID)
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Active components:")
	if len(status.State.ActiveComponents) == 0 {
		fmt.Fprintln(w, "  none")
	} else {
		for _, component := range status.State.ActiveComponents {
			fmt.Fprintf(w, "  - %s: action=%s severity=%s latched=%t\n", component.ComponentID, component.ActiveAction, component.Severity, component.Latched)
			if component.Reason != "" {
				fmt.Fprintf(w, "    reason=%s\n", component.Reason)
			}
			if len(component.SourceTypes) > 0 {
				fmt.Fprintf(w, "    sources=%s\n", strings.Join(component.SourceTypes, ","))
			}
			if !component.LastRequestAt.IsZero() {
				fmt.Fprintf(w, "    last_request_at=%s\n", component.LastRequestAt.Format("2006-01-02 15:04:05Z07:00"))
			}
		}
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Last hook timestamps:")
	fmt.Fprintf(w, "  notify=%s\n", formatMaybeTime(status.State.LastHookAt.Notify))
	fmt.Fprintf(w, "  degrade=%s\n", formatMaybeTime(status.State.LastHookAt.Degrade))
	fmt.Fprintf(w, "  safe_stop=%s\n", formatMaybeTime(status.State.LastHookAt.SafeStop))
	fmt.Fprintf(w, "  resolve=%s\n", formatMaybeTime(status.State.LastHookAt.Resolve))

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Latest request:")
	if status.Latest == nil {
		fmt.Fprintln(w, "  none")
		return
	}

	fmt.Fprintf(w, "  request_id=%s\n", status.Latest.Request.RequestID)
	fmt.Fprintf(w, "  event=%s action=%s overall=%s\n", status.Latest.Request.Event, status.Latest.Request.RequestedAction, status.Latest.Request.Overall)
	if status.Latest.Request.Reason != "" {
		fmt.Fprintf(w, "  reason=%s\n", status.Latest.Request.Reason)
	}
	if status.Latest.Hook != nil {
		fmt.Fprintf(w, "  hook.executed=%t hook.action=%s\n", status.Latest.Hook.Executed, status.Latest.Hook.Action)
		if status.Latest.Hook.Suppressed {
			fmt.Fprintf(w, "  hook.suppressed=%s\n", status.Latest.Hook.SuppressionReason)
		}
		if status.Latest.Hook.Error != "" {
			fmt.Fprintf(w, "  hook.error=%s\n", status.Latest.Hook.Error)
		}
	}
	if len(status.Latest.Decision.ChangedComponents) > 0 {
		fmt.Fprintf(w, "  changed=%s\n", strings.Join(status.Latest.Decision.ChangedComponents, ","))
	}
	if len(status.Latest.Decision.ClearedComponents) > 0 {
		fmt.Fprintf(w, "  cleared=%s\n", strings.Join(status.Latest.Decision.ClearedComponents, ","))
	}
}

func formatMaybeTime(value interface {
	IsZero() bool
	Format(string) string
}) string {
	if value.IsZero() {
		return "-"
	}
	return value.Format("2006-01-02 15:04:05Z07:00")
}

func emptyAs(value actions.Kind, fallback string) string {
	if value == "" {
		return fallback
	}
	return string(value)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: watchdogctl status [-config path] [-json]")
}
