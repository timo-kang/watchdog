package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

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

	printStatus(os.Stdout, status, time.Now().UTC())
}

func printStatus(w io.Writer, status supervisor.StatusView, now time.Time) {
	fmt.Fprintf(w, "Overall: %s\n", emptyAs(status.State.OverallAction, "none"))
	if !status.State.UpdatedAt.IsZero() {
		fmt.Fprintf(w, "Updated: %s\n", formatTimeWithAge(status.State.UpdatedAt, now))
	}
	fmt.Fprintf(w, "Active: %d component%s\n", len(status.State.ActiveComponents), plural(len(status.State.ActiveComponents)))
	if status.State.LastRequestID != "" {
		fmt.Fprintf(w, "Last request: %s\n", status.State.LastRequestID)
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Active Components")
	if len(status.State.ActiveComponents) == 0 {
		fmt.Fprintln(w, "  none")
	} else {
		for i, component := range status.State.ActiveComponents {
			fmt.Fprintf(w, "%d. %s\n", i+1, component.ComponentID)
			fmt.Fprintf(w, "   State: %s -> %s", component.Severity, component.ActiveAction)
			if component.Latched {
				fmt.Fprint(w, " [latched]")
			}
			fmt.Fprintln(w)
			if len(component.SourceTypes) > 0 {
				fmt.Fprintf(w, "   Sources: %s\n", strings.Join(component.SourceTypes, ", "))
			}
			if !component.FirstActivatedAt.IsZero() {
				fmt.Fprintf(w, "   First seen: %s\n", formatTimeWithAge(component.FirstActivatedAt, now))
			}
			if !component.LastRequestAt.IsZero() {
				fmt.Fprintf(w, "   Last request: %s\n", formatTimeWithAge(component.LastRequestAt, now))
			}
			writeReasonBlock(w, "   Reason:", component.Reason)
		}
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Latest Request")
	if status.Latest == nil {
		fmt.Fprintln(w, "  none")
	} else {
		fmt.Fprintf(w, "  Event: %s\n", status.Latest.Request.Event)
		fmt.Fprintf(w, "  Action: %s\n", emptyAs(status.Latest.Request.RequestedAction, "none"))
		fmt.Fprintf(w, "  Overall: %s\n", status.Latest.Request.Overall)
		fmt.Fprintf(w, "  When: %s\n", formatTimeWithAge(status.Latest.Request.Timestamp, now))
		fmt.Fprintf(w, "  ID: %s\n", status.Latest.Request.RequestID)
		if len(status.Latest.Decision.ChangedComponents) > 0 {
			fmt.Fprintf(w, "  Changed: %s\n", strings.Join(status.Latest.Decision.ChangedComponents, ", "))
		}
		if len(status.Latest.Decision.ClearedComponents) > 0 {
			fmt.Fprintf(w, "  Cleared: %s\n", strings.Join(status.Latest.Decision.ClearedComponents, ", "))
		}
		writeReasonBlock(w, "  Reason:", status.Latest.Request.Reason)
		if status.Latest.Hook != nil {
			fmt.Fprintf(w, "  Hook: %s\n", summarizeHook(*status.Latest.Hook))
			if status.Latest.Hook.Error != "" {
				fmt.Fprintf(w, "  Hook error: %s\n", status.Latest.Hook.Error)
			}
		}
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Hook Timestamps")
	fmt.Fprintf(w, "  notify: %s\n", formatMaybeTime(status.State.LastHookAt.Notify, now))
	fmt.Fprintf(w, "  degrade: %s\n", formatMaybeTime(status.State.LastHookAt.Degrade, now))
	fmt.Fprintf(w, "  safe_stop: %s\n", formatMaybeTime(status.State.LastHookAt.SafeStop, now))
	fmt.Fprintf(w, "  resolve: %s\n", formatMaybeTime(status.State.LastHookAt.Resolve, now))
}

func formatMaybeTime(value time.Time, now time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return formatTimeWithAge(value, now)
}

func emptyAs(value actions.Kind, fallback string) string {
	if value == "" {
		return fallback
	}
	return string(value)
}

func writeReasonBlock(w io.Writer, label, reason string) {
	if strings.TrimSpace(reason) == "" {
		return
	}
	fmt.Fprintln(w, label)
	for _, line := range splitReason(reason) {
		fmt.Fprintf(w, "     - %s\n", line)
	}
}

func splitReason(reason string) []string {
	parts := strings.Split(reason, "; ")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lines = append(lines, part)
	}
	if len(lines) == 0 {
		return []string{strings.TrimSpace(reason)}
	}
	return lines
}

func summarizeHook(hook supervisor.HookResult) string {
	switch {
	case hook.Executed:
		if hook.Command != nil {
			return fmt.Sprintf("executed %s (%dms)", strings.Join(hook.Command, " "), hook.DurationMs)
		}
		return fmt.Sprintf("executed (%dms)", hook.DurationMs)
	case hook.Suppressed:
		if hook.SuppressionReason != "" {
			return fmt.Sprintf("suppressed: %s", hook.SuppressionReason)
		}
		return "suppressed"
	case len(hook.Command) == 0:
		return "not configured"
	default:
		return "not executed"
	}
}

func formatTimeWithAge(value time.Time, now time.Time) string {
	if value.IsZero() {
		return "-"
	}
	local := value.In(time.Local)
	return fmt.Sprintf("%s (%s ago)", local.Format("2006-01-02 15:04:05 MST"), humanDuration(now.Sub(value)))
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Second:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Round(time.Second)/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Round(time.Minute)/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Round(time.Hour)/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(d.Round(24*time.Hour)/(24*time.Hour)))
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: watchdogctl status [-config path] [-json]")
}
