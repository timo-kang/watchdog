package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"watchdog/internal/actions"
	"watchdog/internal/health"
	"watchdog/internal/supervisor"
)

type statusOptions struct {
	verbose      bool
	incident     *health.Snapshot
	incidentErr  error
	incidentPath string
}

type verboseStatusView struct {
	StatusView    supervisor.StatusView `json:"status"`
	IncidentPath  string                `json:"incident_path,omitempty"`
	Incident      *health.Snapshot      `json:"incident_snapshot,omitempty"`
	IncidentError string                `json:"incident_error,omitempty"`
}

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
	verbose := fs.Bool("verbose", false, "print fuller request and incident details")
	fs.Parse(args)

	cfg, err := supervisor.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("load supervisor config: %v", err)
	}

	status, err := supervisor.LoadStatus(cfg.StatePath, cfg.LatestPath)
	if err != nil {
		log.Fatalf("load supervisor status: %v", err)
	}

	opts := statusOptions{verbose: *verbose}
	if status.Latest != nil && status.Latest.Request.IncidentPath != "" {
		opts.incidentPath = status.Latest.Request.IncidentPath
		if *verbose {
			incident, incidentErr := loadIncident(status.Latest.Request.IncidentPath)
			opts.incident = incident
			opts.incidentErr = incidentErr
		}
	}

	if *jsonOutput {
		payload := status
		if *verbose {
			view := verboseStatusView{
				StatusView:   status,
				IncidentPath: opts.incidentPath,
				Incident:     opts.incident,
			}
			if opts.incidentErr != nil {
				view.IncidentError = opts.incidentErr.Error()
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(view); err != nil {
				log.Fatalf("encode status: %v", err)
			}
			return
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			log.Fatalf("encode status: %v", err)
		}
		return
	}

	printStatus(os.Stdout, status, time.Now().UTC(), opts)
}

func printStatus(w io.Writer, status supervisor.StatusView, now time.Time, opts statusOptions) {
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
		if status.Latest.Request.PreviousOverall != "" {
			fmt.Fprintf(w, "  Previous overall: %s\n", status.Latest.Request.PreviousOverall)
		}
		fmt.Fprintf(w, "  When: %s\n", formatTimeWithAge(status.Latest.Request.Timestamp, now))
		fmt.Fprintf(w, "  ID: %s\n", status.Latest.Request.RequestID)
		if opts.incidentPath != "" {
			fmt.Fprintf(w, "  Incident: %s\n", opts.incidentPath)
		}
		if len(status.Latest.Decision.ChangedComponents) > 0 {
			fmt.Fprintf(w, "  Changed: %s\n", strings.Join(status.Latest.Decision.ChangedComponents, ", "))
		}
		if len(status.Latest.Decision.ClearedComponents) > 0 {
			fmt.Fprintf(w, "  Cleared: %s\n", strings.Join(status.Latest.Decision.ClearedComponents, ", "))
		}
		writeReasonBlock(w, "  Reason:", status.Latest.Request.Reason)
		if len(status.Latest.Request.Errors) > 0 {
			writeListBlock(w, "  Request errors:", status.Latest.Request.Errors)
		}
		if status.Latest.Hook != nil {
			fmt.Fprintf(w, "  Hook: %s\n", summarizeHook(*status.Latest.Hook))
			if len(status.Latest.Hook.Command) > 0 {
				fmt.Fprintf(w, "  Hook command: %s\n", strings.Join(status.Latest.Hook.Command, " "))
			}
			if status.Latest.Hook.Suppressed {
				fmt.Fprintf(w, "  Hook suppression: %s\n", status.Latest.Hook.SuppressionReason)
			}
			if status.Latest.Hook.ExitCode != 0 {
				fmt.Fprintf(w, "  Hook exit code: %d\n", status.Latest.Hook.ExitCode)
			}
			if status.Latest.Hook.Error != "" {
				fmt.Fprintf(w, "  Hook error: %s\n", status.Latest.Hook.Error)
			}
			if opts.verbose {
				writeTextBlock(w, "  Hook stdout:", status.Latest.Hook.Stdout)
				writeTextBlock(w, "  Hook stderr:", status.Latest.Hook.Stderr)
			}
		}
		if opts.verbose && len(status.Latest.Request.Components) > 0 {
			fmt.Fprintln(w, "  Requested components:")
			for i, component := range status.Latest.Request.Components {
				fmt.Fprintf(w, "    %d. %s\n", i+1, component.ComponentID)
				fmt.Fprintf(w, "       State: %s -> %s\n", component.Severity, component.RequestedAction)
				if len(component.SourceTypes) > 0 {
					fmt.Fprintf(w, "       Sources: %s\n", strings.Join(component.SourceTypes, ", "))
				}
				writeReasonBlock(w, "       Reason:", component.Reason)
			}
		}
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Hook Timestamps")
	fmt.Fprintf(w, "  notify: %s\n", formatMaybeTime(status.State.LastHookAt.Notify, now))
	fmt.Fprintf(w, "  degrade: %s\n", formatMaybeTime(status.State.LastHookAt.Degrade, now))
	fmt.Fprintf(w, "  safe_stop: %s\n", formatMaybeTime(status.State.LastHookAt.SafeStop, now))
	fmt.Fprintf(w, "  resolve: %s\n", formatMaybeTime(status.State.LastHookAt.Resolve, now))

	if opts.verbose {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Incident Snapshot")
		switch {
		case opts.incidentErr != nil:
			fmt.Fprintf(w, "  load error: %v\n", opts.incidentErr)
		case opts.incident == nil:
			fmt.Fprintln(w, "  none")
		default:
			printIncidentSnapshot(w, *opts.incident, now, opts.incidentPath)
		}
	}
}

func printIncidentSnapshot(w io.Writer, snapshot health.Snapshot, now time.Time, path string) {
	if path != "" {
		fmt.Fprintf(w, "  Path: %s\n", path)
	}
	fmt.Fprintf(w, "  Collected: %s\n", formatTimeWithAge(snapshot.CollectedAt, now))
	fmt.Fprintf(w, "  Overall: %s\n", snapshot.Overall)
	fmt.Fprintf(w, "  Raw statuses: %d\n", len(snapshot.Statuses))
	fmt.Fprintf(w, "  Components: %d\n", len(snapshot.Components))
	if len(snapshot.Errors) > 0 {
		writeListBlock(w, "  Errors:", snapshot.Errors)
	}
	if len(snapshot.Statuses) == 0 {
		return
	}
	fmt.Fprintln(w, "  Status details:")
	for i, status := range snapshot.Statuses {
		fmt.Fprintf(w, "    %d. %s [%s] %s\n", i+1, status.SourceID, status.SourceType, status.Severity)
		fmt.Fprintf(w, "       Observed: %s\n", formatTimeWithAge(status.ObservedAt, now))
		writeReasonBlock(w, "       Reason:", status.Reason)
		writeMapBlock(w, "       Metrics:", formatFloatMap(status.Metrics))
		writeMapBlock(w, "       Labels:", status.Labels)
	}
	if len(snapshot.Components) > 0 {
		fmt.Fprintln(w, "  Component details:")
		for i, component := range snapshot.Components {
			fmt.Fprintf(w, "    %d. %s %s\n", i+1, component.ComponentID, component.Severity)
			fmt.Fprintf(w, "       Observed: %s\n", formatTimeWithAge(component.ObservedAt, now))
			if len(component.Sources) > 0 {
				sources := make([]string, 0, len(component.Sources))
				for _, source := range component.Sources {
					sources = append(sources, fmt.Sprintf("%s=%s", source.SourceType, source.Severity))
				}
				sort.Strings(sources)
				fmt.Fprintf(w, "       Sources: %s\n", strings.Join(sources, ", "))
			}
			writeReasonBlock(w, "       Reason:", component.Reason)
		}
	}
}

func loadIncident(path string) (*health.Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snapshot health.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
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
		fmt.Fprintf(w, "%s- %s\n", blockIndent(label), line)
	}
}

func writeListBlock(w io.Writer, label string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintln(w, label)
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			continue
		}
		fmt.Fprintf(w, "%s- %s\n", blockIndent(label), item)
	}
}

func writeTextBlock(w io.Writer, label, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	fmt.Fprintln(w, label)
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Fprintf(w, "%s%s\n", blockIndent(label), line)
	}
}

func writeMapBlock(w io.Writer, label string, items map[string]string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintln(w, label)
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(w, "%s- %s=%s\n", blockIndent(label), key, items[key])
	}
}

func blockIndent(label string) string {
	count := 0
	for count < len(label) && label[count] == " "[0] {
		count++
	}
	return strings.Repeat(" ", count+2)
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

func formatFloatMap(values map[string]float64) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = strconv.FormatFloat(value, 'f', -1, 64)
	}
	return out
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: watchdogctl status [-config path] [-json] [-verbose]")
}
