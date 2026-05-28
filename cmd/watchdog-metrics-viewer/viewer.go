package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type sample struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	Value  float64           `json:"value"`
}

type endpointResult struct {
	OK        bool     `json:"ok"`
	URL       string   `json:"url"`
	Error     string   `json:"error,omitempty"`
	FetchedAt string   `json:"fetched_at"`
	Samples   []sample `json:"-"`
}

type sourceView struct {
	SourceID   string             `json:"source_id"`
	SourceType string             `json:"source_type"`
	Severity   float64            `json:"severity"`
	ObservedAt float64            `json:"observed_at"`
	Metrics    map[string]float64 `json:"metrics"`
}

type componentView struct {
	ComponentID string  `json:"component_id"`
	Severity    float64 `json:"severity"`
}

type watchdogView struct {
	Endpoint           endpointResult  `json:"endpoint"`
	Overall            float64         `json:"overall"`
	SnapshotComponents float64         `json:"snapshot_components"`
	SnapshotStatuses   float64         `json:"snapshot_statuses"`
	SnapshotErrors     float64         `json:"snapshot_errors"`
	SnapshotTimestamp  float64         `json:"snapshot_timestamp"`
	Components         []componentView `json:"components"`
	Sources            []sourceView    `json:"sources"`
}

type supervisorComponentView struct {
	ComponentID string  `json:"component_id"`
	Action      string  `json:"action"`
	Severity    string  `json:"severity"`
	Latched     string  `json:"latched"`
	Value       float64 `json:"value"`
}

type supervisorView struct {
	Endpoint         endpointResult                `json:"endpoint"`
	ActiveComponents float64                       `json:"active_components"`
	OverallActions   map[string]float64            `json:"overall_actions"`
	Components       []supervisorComponentView     `json:"components"`
	RequestTotals    map[string]map[string]float64 `json:"request_totals"`
}

type historyPoint struct {
	Time            string  `json:"time"`
	AgeS            float64 `json:"age_s"`
	ControlPeriodUS float64 `json:"control_period_us"`
}

type apiResponse struct {
	Now        string         `json:"now"`
	Watchdog   watchdogView   `json:"watchdog"`
	Supervisor supervisorView `json:"supervisor"`
	History    []historyPoint `json:"history"`
}

type sampler struct {
	mu             sync.RWMutex
	client         *http.Client
	watchdogURL    string
	supervisorURL  string
	historySource  string
	latest         apiResponse
	history        []historyPoint
	maxHistorySize int
}

func main() {
	listen := flag.String("listen", "127.0.0.1:18080", "HTTP listen address")
	watchdogURL := flag.String("watchdog", "http://127.0.0.1:9108/metrics", "watchdog metrics URL")
	supervisorURL := flag.String("supervisor", "http://127.0.0.1:9109/metrics", "watchdog-supervisor metrics URL")
	historySource := flag.String("source", "robot.main", "source_id to graph")
	flag.Parse()

	state := &sampler{
		client:         &http.Client{Timeout: 2 * time.Second},
		watchdogURL:    *watchdogURL,
		supervisorURL:  *supervisorURL,
		historySource:  *historySource,
		maxHistorySize: 300,
	}
	state.sample()
	go state.run()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := pageTemplate.Execute(w, nil); err != nil {
			log.Printf("render page: %v", err)
		}
	})
	mux.HandleFunc("/api/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(state.snapshot()); err != nil {
			log.Printf("encode metrics: %v", err)
		}
	})

	log.Printf("watchdog metrics viewer listening http://%s", *listen)
	log.Fatal(http.ListenAndServe(*listen, mux))
}

func (s *sampler) run() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.sample()
	}
}

func (s *sampler) sample() {
	resp := apiResponse{
		Now:        time.Now().Format(time.RFC3339Nano),
		Watchdog:   buildWatchdogView(fetchMetrics(s.client, s.watchdogURL)),
		Supervisor: buildSupervisorView(fetchMetrics(s.client, s.supervisorURL)),
	}

	point, ok := s.historyPoint(resp)

	s.mu.Lock()
	if ok {
		s.history = append(s.history, point)
		if len(s.history) > s.maxHistorySize {
			s.history = s.history[len(s.history)-s.maxHistorySize:]
		}
	}
	resp.History = append([]historyPoint(nil), s.history...)
	s.latest = resp
	s.mu.Unlock()
}

func (s *sampler) historyPoint(resp apiResponse) (historyPoint, bool) {
	src := findSource(resp.Watchdog.Sources, s.historySource)
	if src == nil {
		return historyPoint{}, false
	}
	point := historyPoint{
		Time:            resp.Now,
		AgeS:            src.Metrics["age_s"],
		ControlPeriodUS: src.Metrics["control_period_us"],
	}
	return point, isFinite(point.AgeS) || isFinite(point.ControlPeriodUS)
}

func (s *sampler) snapshot() apiResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()
	resp := s.latest
	resp.History = append([]historyPoint(nil), s.history...)
	return resp
}

func findSource(sources []sourceView, sourceID string) *sourceView {
	for i := range sources {
		if sources[i].SourceID == sourceID {
			return &sources[i]
		}
	}
	return nil
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func fetchMetrics(client *http.Client, url string) endpointResult {
	out := endpointResult{
		URL:       url,
		FetchedAt: time.Now().Format(time.RFC3339Nano),
	}
	resp, err := client.Get(url)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		out.Error = fmt.Sprintf("HTTP %s", resp.Status)
		return out
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		out.Error = err.Error()
		return out
	}
	samples, err := parsePrometheusText(string(body))
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.OK = true
	out.Samples = samples
	return out
}

func buildWatchdogView(endpoint endpointResult) watchdogView {
	view := watchdogView{
		Endpoint:   endpoint,
		Components: []componentView{},
		Sources:    []sourceView{},
	}
	sourceMap := map[string]*sourceView{}

	for _, s := range endpoint.Samples {
		switch s.Name {
		case "watchdog_snapshot_overall_code":
			view.Overall = s.Value
		case "watchdog_snapshot_components":
			view.SnapshotComponents = s.Value
		case "watchdog_snapshot_statuses":
			view.SnapshotStatuses = s.Value
		case "watchdog_snapshot_errors":
			view.SnapshotErrors = s.Value
		case "watchdog_snapshot_timestamp_seconds":
			view.SnapshotTimestamp = s.Value
		case "watchdog_component_severity_code":
			view.Components = append(view.Components, componentView{
				ComponentID: s.Labels["component_id"],
				Severity:    s.Value,
			})
		case "watchdog_status_severity_code":
			src := source(sourceMap, s.Labels["source_id"], s.Labels["source_type"])
			src.Severity = s.Value
		case "watchdog_status_observed_at_seconds":
			src := source(sourceMap, s.Labels["source_id"], s.Labels["source_type"])
			src.ObservedAt = s.Value
		case "watchdog_status_metric_value":
			src := source(sourceMap, s.Labels["source_id"], s.Labels["source_type"])
			src.Metrics[s.Labels["metric_name"]] = s.Value
		}
	}

	for _, src := range sourceMap {
		view.Sources = append(view.Sources, *src)
	}
	sort.Slice(view.Components, func(i, j int) bool {
		return view.Components[i].ComponentID < view.Components[j].ComponentID
	})
	sort.Slice(view.Sources, func(i, j int) bool {
		return view.Sources[i].SourceID < view.Sources[j].SourceID
	})
	return view
}

func source(sourceMap map[string]*sourceView, id, sourceType string) *sourceView {
	key := id + "\x00" + sourceType
	if src, ok := sourceMap[key]; ok {
		return src
	}
	src := &sourceView{
		SourceID:   id,
		SourceType: sourceType,
		Metrics:    map[string]float64{},
	}
	sourceMap[key] = src
	return src
}

func buildSupervisorView(endpoint endpointResult) supervisorView {
	view := supervisorView{
		Endpoint:       endpoint,
		OverallActions: map[string]float64{},
		Components:     []supervisorComponentView{},
		RequestTotals:  map[string]map[string]float64{},
	}
	for _, s := range endpoint.Samples {
		switch s.Name {
		case "watchdog_supervisor_active_components":
			view.ActiveComponents = s.Value
		case "watchdog_supervisor_overall_action":
			view.OverallActions[s.Labels["action"]] = s.Value
		case "watchdog_supervisor_component_action":
			view.Components = append(view.Components, supervisorComponentView{
				ComponentID: s.Labels["component_id"],
				Action:      s.Labels["action"],
				Severity:    s.Labels["severity"],
				Latched:     s.Labels["latched"],
				Value:       s.Value,
			})
		case "watchdog_supervisor_requests_total":
			key := s.Labels["event"] + "/" + s.Labels["requested_action"]
			if _, ok := view.RequestTotals[key]; !ok {
				view.RequestTotals[key] = map[string]float64{}
			}
			view.RequestTotals[key][s.Labels["result"]] = s.Value
		}
	}
	sort.Slice(view.Components, func(i, j int) bool {
		if view.Components[i].ComponentID == view.Components[j].ComponentID {
			return view.Components[i].Action < view.Components[j].Action
		}
		return view.Components[i].ComponentID < view.Components[j].ComponentID
	})
	return view
}

func parsePrometheusText(text string) ([]sample, error) {
	var samples []sample
	for lineNo, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		s, ok, err := parseSampleLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo+1, err)
		}
		if ok {
			samples = append(samples, s)
		}
	}
	return samples, nil
}

func parseSampleLine(line string) (sample, bool, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return sample{}, false, nil
	}
	head := fields[0]
	value, err := strconv.ParseFloat(fields[1], 64)
	if err != nil || !isFinite(value) {
		return sample{}, false, fmt.Errorf("invalid value %q", fields[1])
	}

	name := head
	labels := map[string]string{}
	if open := strings.IndexByte(head, '{'); open >= 0 {
		if !strings.HasSuffix(head, "}") {
			return sample{}, false, fmt.Errorf("invalid label set")
		}
		name = head[:open]
		parsed, err := parseLabels(head[open+1 : len(head)-1])
		if err != nil {
			return sample{}, false, err
		}
		labels = parsed
	}
	return sample{Name: name, Labels: labels, Value: value}, true, nil
}

func parseLabels(raw string) (map[string]string, error) {
	labels := map[string]string{}
	for len(raw) > 0 {
		eq := strings.IndexByte(raw, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("invalid label %q", raw)
		}
		key := raw[:eq]
		raw = raw[eq+1:]
		if !strings.HasPrefix(raw, "\"") {
			return nil, fmt.Errorf("invalid label value for %q", key)
		}
		raw = raw[1:]

		var value strings.Builder
		escaped := false
		closedAt := -1
		for i, r := range raw {
			if escaped {
				switch r {
				case 'n':
					value.WriteByte('\n')
				case 't':
					value.WriteByte('\t')
				case 'r':
					value.WriteByte('\r')
				default:
					value.WriteRune(r)
				}
				escaped = false
				continue
			}
			switch r {
			case '\\':
				escaped = true
			case '"':
				closedAt = i
				goto closed
			default:
				value.WriteRune(r)
			}
		}
	closed:
		if closedAt < 0 {
			return nil, fmt.Errorf("unterminated label value for %q", key)
		}
		labels[key] = value.String()
		raw = raw[closedAt+1:]
		if strings.HasPrefix(raw, ",") {
			raw = raw[1:]
		} else if raw != "" {
			return nil, fmt.Errorf("invalid label separator near %q", raw)
		}
	}
	return labels, nil
}

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Watchdog Metrics</title>
<style>
:root {
  color-scheme: dark;
  --bg: #101318;
  --panel: #171b22;
  --panel-2: #202633;
  --line: #343b48;
  --muted: #98a2b3;
  --text: #eef2f8;
  --good: #3ddc97;
  --warn: #f7b955;
  --bad: #ff6b6b;
  --cyan: #4cc9f0;
  --violet: #b99cff;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  background: var(--bg);
  color: var(--text);
  font: 14px/1.45 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
main {
  width: min(1440px, 100%);
  margin: 0 auto;
  padding: 20px;
}
header {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
}
h1 {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
  letter-spacing: 0;
}
.timestamp {
  color: var(--muted);
  font-size: 13px;
  white-space: nowrap;
}
.grid {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 12px;
}
.card, .chart, .table {
  background: var(--panel);
  border: 1px solid var(--line);
  border-radius: 8px;
}
.card {
  min-height: 92px;
  padding: 14px;
}
.label {
  color: var(--muted);
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: .08em;
}
.value {
  margin-top: 8px;
  font-size: 26px;
  font-weight: 700;
  letter-spacing: 0;
}
.sub {
  margin-top: 4px;
  color: var(--muted);
  font-size: 12px;
  min-height: 17px;
}
.ok { color: var(--good); }
.warn { color: var(--warn); }
.bad { color: var(--bad); }
.span-3 { grid-column: span 3; }
.span-6 { grid-column: span 6; }
.chart {
  min-height: 310px;
  padding: 14px;
}
.chart h2, .table h2 {
  margin: 0 0 10px;
  font-size: 14px;
  font-weight: 650;
}
canvas {
  display: block;
  width: 100%;
  height: 250px;
}
.table {
  padding: 14px;
  overflow: hidden;
}
table {
  width: 100%;
  border-collapse: collapse;
}
th, td {
  padding: 8px 6px;
  border-top: 1px solid var(--line);
  text-align: left;
  vertical-align: top;
  white-space: nowrap;
}
th {
  color: var(--muted);
  font-size: 12px;
  font-weight: 600;
}
td.metrics {
  white-space: normal;
  color: var(--muted);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
}
.empty {
  color: var(--muted);
  padding: 16px 0 4px;
}
.endpoint {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  color: var(--muted);
  font-size: 12px;
  margin-top: 8px;
}
.pill {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 8px;
  border: 1px solid var(--line);
  border-radius: 999px;
  background: var(--panel-2);
}
@media (max-width: 980px) {
  main { padding: 14px; }
  header { align-items: flex-start; flex-direction: column; }
  .grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .span-3, .span-6 { grid-column: span 2; }
}
@media (max-width: 620px) {
  .grid { grid-template-columns: 1fr; }
  .span-3, .span-6 { grid-column: span 1; }
  th, td { white-space: normal; }
}
</style>
</head>
<body>
<main>
  <header>
    <div>
      <h1>Watchdog Metrics</h1>
      <div class="endpoint">
        <span class="pill" id="watchdogEndpoint">watchdog pending</span>
        <span class="pill" id="supervisorEndpoint">supervisor pending</span>
      </div>
    </div>
    <div class="timestamp" id="timestamp">waiting</div>
  </header>

  <section class="grid">
    <div class="card">
      <div class="label">Watchdog Overall</div>
      <div class="value" id="watchdogOverall">-</div>
      <div class="sub" id="watchdogSub"></div>
    </div>
    <div class="card">
      <div class="label">Heartbeat Age</div>
      <div class="value" id="heartbeatAge">-</div>
      <div class="sub">robot.main age_s</div>
    </div>
    <div class="card">
      <div class="label">Control Period</div>
      <div class="value" id="controlPeriod">-</div>
      <div class="sub">control_period_us</div>
    </div>
    <div class="card">
      <div class="label">Supervisor Overall</div>
      <div class="value" id="supervisorOverall">-</div>
      <div class="sub" id="supervisorSub"></div>
    </div>
    <div class="card">
      <div class="label">Active Latches</div>
      <div class="value" id="activeComponents">-</div>
      <div class="sub">supervisor components</div>
    </div>
    <div class="card">
      <div class="label">Graph Samples</div>
      <div class="value" id="historyCount">-</div>
      <div class="sub">server-side history</div>
    </div>

    <div class="chart span-3">
      <h2>Heartbeat Age</h2>
      <canvas id="ageChart"></canvas>
    </div>
    <div class="chart span-3">
      <h2>Control Period</h2>
      <canvas id="periodChart"></canvas>
    </div>

    <div class="table span-6">
      <h2>Live Source Metrics</h2>
      <div id="sourceEmpty" class="empty">waiting for sources</div>
      <table id="sourceTable" hidden>
        <thead>
          <tr><th>Source</th><th>Type</th><th>Severity</th><th>Observed</th><th>Metrics</th></tr>
        </thead>
        <tbody></tbody>
      </table>
    </div>

    <div class="table span-6">
      <h2>Supervisor Latch State</h2>
      <div id="supervisorEmpty" class="empty">no active supervisor latches</div>
      <table id="supervisorTable" hidden>
        <thead>
          <tr><th>Component</th><th>Action</th><th>Severity</th><th>Latched</th><th>Value</th></tr>
        </thead>
        <tbody></tbody>
      </table>
    </div>
  </section>
</main>

<script>
const severityNames = new Map([[0, "none"], [1, "warn"], [2, "error"], [3, "fatal"]]);
const severityClass = new Map([[0, "ok"], [1, "warn"], [2, "bad"], [3, "bad"]]);
let lastData = null;

function fmtNumber(value, digits = 2) {
  const n = Number(value);
  return Number.isFinite(n) ? n.toFixed(digits) : "-";
}

function fmtAge(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return "-";
  if (n < 1) return (n * 1000).toFixed(0) + " ms";
  return n.toFixed(2) + " s";
}

function fmtPeriod(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return "-";
  return n.toFixed(0) + " us";
}

function setValue(id, text, className) {
  const node = document.getElementById(id);
  node.textContent = text;
  node.className = "value " + (className || "");
}

function severityName(code) {
  const rounded = Math.round(Number(code) || 0);
  return severityNames.get(rounded) || String(rounded);
}

function actionFromOverall(actions, activeComponents) {
  if (!actions || Number(activeComponents || 0) === 0) return "none";
  for (const action of ["reboot", "restart", "stop", "degrade"]) {
    if (Number(actions[action] || 0) > 0) return action;
  }
  return "none";
}

function metricsSummary(metrics) {
  return Object.entries(metrics || {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([k, v]) => k + "=" + fmtNumber(v, 3))
    .join(", ");
}

function updateEndpoint(id, endpoint) {
  const node = document.getElementById(id);
  if (!endpoint) {
    node.textContent = "endpoint missing";
    node.className = "pill bad";
    return;
  }
  node.textContent = endpoint.ok ? endpoint.url : endpoint.url + ": " + (endpoint.error || "error");
  node.className = "pill " + (endpoint.ok ? "ok" : "bad");
}

function updateSourceTable(rows) {
  rows = Array.isArray(rows) ? rows : [];
  const table = document.getElementById("sourceTable");
  const empty = document.getElementById("sourceEmpty");
  const body = table.querySelector("tbody");
  body.innerHTML = "";
  table.hidden = rows.length === 0;
  empty.hidden = rows.length !== 0;
  for (const row of rows) {
    const tr = document.createElement("tr");
    const severity = Math.round(Number(row.severity || 0));
    tr.innerHTML = "<td>" + escapeHtml(row.source_id || "") + "</td>" +
      "<td>" + escapeHtml(row.source_type || "") + "</td>" +
      "<td class=\"" + (severityClass.get(severity) || "") + "\">" + severityName(severity) + "</td>" +
      "<td>" + fmtNumber(row.observed_at, 3) + "</td>" +
      "<td class=\"metrics\">" + escapeHtml(metricsSummary(row.metrics)) + "</td>";
    body.appendChild(tr);
  }
}

function updateSupervisorTable(rows) {
  rows = Array.isArray(rows) ? rows : [];
  rows = rows.filter(row => Number(row.value || 0) !== 0);
  const table = document.getElementById("supervisorTable");
  const empty = document.getElementById("supervisorEmpty");
  const body = table.querySelector("tbody");
  body.innerHTML = "";
  table.hidden = rows.length === 0;
  empty.hidden = rows.length !== 0;
  for (const row of rows) {
    const tr = document.createElement("tr");
    tr.innerHTML = "<td>" + escapeHtml(row.component_id || "") + "</td>" +
      "<td>" + escapeHtml(row.action || "") + "</td>" +
      "<td>" + escapeHtml(row.severity || "") + "</td>" +
      "<td>" + escapeHtml(row.latched || "") + "</td>" +
      "<td>" + fmtNumber(row.value, 0) + "</td>";
    body.appendChild(tr);
  }
}

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function fitCanvas(canvas) {
  const dpr = window.devicePixelRatio || 1;
  const rect = canvas.getBoundingClientRect();
  const width = Math.max(240, rect.width);
  const height = Math.max(180, rect.height);
  const pixelWidth = Math.round(width * dpr);
  const pixelHeight = Math.round(height * dpr);
  if (canvas.width !== pixelWidth || canvas.height !== pixelHeight) {
    canvas.width = pixelWidth;
    canvas.height = pixelHeight;
  }
  const ctx = canvas.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  return {ctx, width, height};
}

function drawEmpty(ctx, width, height, text) {
  ctx.fillStyle = "#98a2b3";
  ctx.font = "13px system-ui, sans-serif";
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  ctx.fillText(text, width / 2, height / 2);
}

function drawChart(canvas, history, field, color, unit) {
  const {ctx, width, height} = fitCanvas(canvas);
  ctx.clearRect(0, 0, width, height);
  ctx.fillStyle = "#171b22";
  ctx.fillRect(0, 0, width, height);

  const points = (Array.isArray(history) ? history : [])
    .map(p => ({t: Date.parse(p.time), v: Number(p[field])}))
    .filter(p => Number.isFinite(p.t) && Number.isFinite(p.v));

  const padL = 70;
  const padR = 14;
  const padT = 14;
  const padB = 28;
  const plotW = width - padL - padR;
  const plotH = height - padT - padB;

  ctx.strokeStyle = "#343b48";
  ctx.lineWidth = 1;
  for (let i = 0; i <= 4; i++) {
    const y = padT + plotH * i / 4;
    ctx.beginPath();
    ctx.moveTo(padL, y);
    ctx.lineTo(width - padR, y);
    ctx.stroke();
  }

  if (points.length === 0) {
    drawEmpty(ctx, width, height, "waiting for samples");
    return;
  }

  let minT = Math.min(...points.map(p => p.t));
  let maxT = Math.max(...points.map(p => p.t));
  let minV = Math.min(...points.map(p => p.v));
  let maxV = Math.max(...points.map(p => p.v));
  if (minT === maxT) {
    minT -= 1000;
    maxT += 1000;
  }
  if (minV === maxV) {
    const delta = Math.max(1, Math.abs(minV) * 0.1);
    minV -= delta;
    maxV += delta;
  }

  const x = p => padL + (p.t - minT) / (maxT - minT) * plotW;
  const y = p => padT + (maxV - p.v) / (maxV - minV) * plotH;

  ctx.fillStyle = "#98a2b3";
  ctx.font = "11px ui-monospace, monospace";
  ctx.textAlign = "right";
  ctx.textBaseline = "middle";
  for (let i = 0; i <= 4; i++) {
    const value = maxV - (maxV - minV) * i / 4;
    ctx.fillText(value.toFixed(2) + unit, padL - 8, padT + plotH * i / 4);
  }

  ctx.strokeStyle = color;
  ctx.lineWidth = 2;
  ctx.beginPath();
  points.forEach((p, i) => {
    if (i === 0) ctx.moveTo(x(p), y(p));
    else ctx.lineTo(x(p), y(p));
  });
  ctx.stroke();

  ctx.fillStyle = color;
  for (const p of points.slice(-80)) {
    ctx.beginPath();
    ctx.arc(x(p), y(p), 2.5, 0, Math.PI * 2);
    ctx.fill();
  }
}

async function refresh() {
  try {
    const response = await fetch("/api/metrics", {cache: "no-store"});
    const data = await response.json();
    lastData = data;
    render(data);
  } catch (err) {
    document.getElementById("timestamp").textContent = "viewer error: " + err.message;
  }
}

function render(data) {
  const watchdog = data.watchdog || {};
  const supervisor = data.supervisor || {};
  const history = Array.isArray(data.history) ? data.history.slice(-180) : [];
  const mainSource = (watchdog.sources || []).find(s => s.source_id === "robot.main") || (watchdog.sources || [])[0] || {};
  const metrics = mainSource.metrics || {};
  const watchdogSeverity = Math.round(Number(watchdog.overall || 0));
  const supervisorAction = actionFromOverall(supervisor.overall_actions, supervisor.active_components);

  document.getElementById("timestamp").textContent = data.now ? new Date(data.now).toLocaleString() : "no sample";
  updateEndpoint("watchdogEndpoint", watchdog.endpoint);
  updateEndpoint("supervisorEndpoint", supervisor.endpoint);

  setValue("watchdogOverall", severityName(watchdogSeverity), severityClass.get(watchdogSeverity));
  document.getElementById("watchdogSub").textContent =
    fmtNumber(watchdog.snapshot_statuses, 0) + " statuses, " + fmtNumber(watchdog.snapshot_components, 0) + " components";
  setValue("heartbeatAge", fmtAge(metrics.age_s), Number(metrics.age_s || 0) < 1.5 ? "ok" : "warn");
  setValue("controlPeriod", fmtPeriod(metrics.control_period_us), "ok");
  setValue("supervisorOverall", supervisorAction, supervisorAction === "none" ? "ok" : "warn");
  document.getElementById("supervisorSub").textContent = fmtNumber(supervisor.active_components, 0) + " active";
  setValue("activeComponents", fmtNumber(supervisor.active_components, 0), Number(supervisor.active_components || 0) === 0 ? "ok" : "warn");
  setValue("historyCount", String(history.length), history.length > 0 ? "ok" : "warn");

  updateSourceTable(watchdog.sources || []);
  updateSupervisorTable(supervisor.components || []);
  drawChart(document.getElementById("ageChart"), history, "age_s", "#4cc9f0", "s");
  drawChart(document.getElementById("periodChart"), history, "control_period_us", "#3ddc97", "us");
}

window.addEventListener("resize", () => {
  if (lastData) render(lastData);
});

refresh();
setInterval(refresh, 1000);
</script>
</body>
</html>`))
