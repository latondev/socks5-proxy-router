package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	activityLogs   []ActivityLog
	activityLogsMu sync.RWMutex
)

type ActivityLog struct {
	Time    string `json:"time"`
	Message string `json:"message"`
	Type    string `json:"type"` // "switch", "refresh", "test"
}

func addActivityLog(msg, logType string) {
	activityLogsMu.Lock()
	defer activityLogsMu.Unlock()
	vnLoc := time.FixedZone("ICT", 7*3600)
	tStr := time.Now().In(vnLoc).Format("15:04:05")
	activityLogs = append([]ActivityLog{{Time: tStr, Message: msg, Type: logType}}, activityLogs...)
	if len(activityLogs) > 10 {
		activityLogs = activityLogs[:10]
	}
}

type StatusServer struct {
	pool       *ProxyPool
	listenAddr string
}

type StatusData struct {
	Total             int           `json:"total"`
	ActiveIndex       int           `json:"active_index"`
	ActiveProxy       string        `json:"active_proxy"`
	ActiveRegion      string        `json:"active_region"`
	ActiveCountry     string        `json:"active_country"`
	ActiveCity        string        `json:"active_city"`
	ActiveCountryCode string        `json:"active_country_code"`
	ActiveLatencyMs   int64         `json:"active_latency_ms"`
	FastestLatencyMs  int64         `json:"fastest_latency_ms"`
	AvgLatencyMs      int64         `json:"avg_latency_ms"`
	FastCount         int           `json:"fast_count"`
	MidCount          int           `json:"mid_count"`
	SlowCount         int           `json:"slow_count"`
	CountriesCount    int           `json:"countries_count"`
	LastScrape        string        `json:"last_scrape"`
	NextScrape        string        `json:"next_scrape"`
	NextScrapeUnix    int64         `json:"next_scrape_unix"`
	ListenAddr        string        `json:"listen_addr"`
	Logs              []ActivityLog `json:"logs"`
	Proxies           []ProxyStatus `json:"proxies"`
}

type ProxyStatus struct {
	Index       int    `json:"index"`
	Addr        string `json:"addr"`
	IP          string `json:"ip"`
	Port        string `json:"port"`
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	City        string `json:"city"`
	Active      bool   `json:"active"`
	LatencyMs   int64  `json:"latency_ms"`
}

func NewStatusServer(pool *ProxyPool, listenAddr string) *StatusServer {
	addActivityLog(fmt.Sprintf("SOCKS5 Router engine started on %s", listenAddr), "system")
	return &StatusServer{
		pool:       pool,
		listenAddr: listenAddr,
	}
}

func (s *StatusServer) Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/api/status", s.handleAPI)
	mux.HandleFunc("/api/refresh", s.handleRefresh)
	mux.HandleFunc("/api/switch", s.handleSwitch)
	mux.HandleFunc("/api/test", s.handleTest)
	mux.HandleFunc("/api/export", s.handleExport)
	return http.ListenAndServe(addr, mux)
}

func (s *StatusServer) getStatusData() StatusData {
	proxies := s.pool.All()
	activeIdx := s.pool.CurrentIndex()
	last, next := getScrapeTimes()

	// Local / VN timezone (UTC+7)
	vnLoc := time.FixedZone("ICT", 7*3600)

	var lastStr, nextStr string
	var nextUnix int64
	if !last.IsZero() {
		lastStr = last.In(vnLoc).Format("2006-01-02 15:04:05")
	}
	if !next.IsZero() {
		nextStr = next.In(vnLoc).Format("2006-01-02 15:04:05")
		nextUnix = next.Unix()
	}

	countrySet := make(map[string]bool)
	var totalLatency int64
	var fastestLatency int64 = 999999
	var fastCount, midCount, slowCount int

	var ps []ProxyStatus
	for i, p := range proxies {
		lat := p.Latency.Milliseconds()
		if lat > 0 {
			totalLatency += lat
			if lat < fastestLatency {
				fastestLatency = lat
			}
			if lat < 500 {
				fastCount++
			} else if lat < 1200 {
				midCount++
			} else {
				slowCount++
			}
		}
		if p.Country != "" {
			countrySet[strings.ToLower(p.Country)] = true
		}

		ps = append(ps, ProxyStatus{
			Index:       i,
			Addr:        p.Addr(),
			IP:          p.IP,
			Port:        p.Port,
			Country:     p.Country,
			CountryCode: p.CountryCode,
			City:        p.City,
			Active:      i == activeIdx,
			LatencyMs:   lat,
		})
	}

	var avgLatency int64
	if len(proxies) > 0 && totalLatency > 0 {
		avgLatency = totalLatency / int64(len(proxies))
	}
	if fastestLatency == 999999 {
		fastestLatency = 0
	}

	// Active proxy info
	var activeProxy, activeRegion, activeCountry, activeCity, activeCountryCode string
	var activeLatencyMs int64
	if p, ok := s.pool.Current(); ok {
		activeProxy = p.Addr()
		activeCountry = p.Country
		activeCity = p.City
		activeCountryCode = p.CountryCode
		activeRegion = p.Country
		if p.City != "" {
			activeRegion += ", " + p.City
		}
		activeLatencyMs = p.Latency.Milliseconds()
	} else {
		activeProxy = "None"
		activeRegion = "No Active Proxy"
	}

	activityLogsMu.RLock()
	logsCopy := make([]ActivityLog, len(activityLogs))
	copy(logsCopy, activityLogs)
	activityLogsMu.RUnlock()

	return StatusData{
		Total:             len(proxies),
		ActiveIndex:       activeIdx,
		ActiveProxy:       activeProxy,
		ActiveRegion:      activeRegion,
		ActiveCountry:     activeCountry,
		ActiveCity:        activeCity,
		ActiveCountryCode: activeCountryCode,
		ActiveLatencyMs:   activeLatencyMs,
		FastestLatencyMs:  fastestLatency,
		AvgLatencyMs:      avgLatency,
		FastCount:         fastCount,
		MidCount:          midCount,
		SlowCount:         slowCount,
		CountriesCount:    len(countrySet),
		LastScrape:        lastStr,
		NextScrape:        nextStr,
		NextScrapeUnix:    nextUnix,
		ListenAddr:        s.listenAddr,
		Logs:              logsCopy,
		Proxies:           ps,
	}
}

func (s *StatusServer) handleAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(s.getStatusData())
}

func (s *StatusServer) handleRefresh(w http.ResponseWriter, r *http.Request) {
	TriggerRefresh()
	addActivityLog("Manual pool refresh triggered", "refresh")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write([]byte(`{"status":"refresh triggered"}`))
}

func (s *StatusServer) handleSwitch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	indexStr := r.URL.Query().Get("index")
	if indexStr != "" {
		index, err := strconv.Atoi(indexStr)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "invalid index"})
			return
		}
		if p, ok := s.pool.SwitchTo(index); ok {
			addActivityLog(fmt.Sprintf("Switched to proxy #%d (%s, %s)", index, p.Addr(), p.Country), "switch")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "ok",
				"proxy":  p.Addr(),
				"index":  index,
			})
		} else {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "index out of range"})
		}
	} else {
		if p, ok := s.pool.SwitchNext(); ok {
			idx := s.pool.CurrentIndex()
			addActivityLog(fmt.Sprintf("Rotated to proxy #%d (%s, %s)", idx, p.Addr(), p.Country), "switch")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "ok",
				"proxy":  p.Addr(),
				"index":  idx,
			})
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "no proxies available"})
		}
	}
}

func (s *StatusServer) handleTest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	current, ok := s.pool.Current()
	if !ok {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "error", "message": "no active proxy"})
		return
	}
	okCheck, latency := checkHTTPS(current, 5*time.Second)
	if !okCheck {
		addActivityLog(fmt.Sprintf("Health test failed for %s", current.Addr()), "test")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "failed",
			"proxy":   current.Addr(),
			"message": "upstream unreachable",
		})
		return
	}
	addActivityLog(fmt.Sprintf("Health test passed for %s in %d ms", current.Addr(), latency.Milliseconds()), "test")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"proxy":      current.Addr(),
		"latency_ms": latency.Milliseconds(),
		"country":    current.Country,
		"city":       current.City,
	})
}

func (s *StatusServer) handleExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	proxies := s.pool.All()
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=\"proxies.json\"")
		json.NewEncoder(w).Encode(proxies)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"socks5-proxies.txt\"")
	var b strings.Builder
	for _, p := range proxies {
		b.WriteString(p.Addr() + "\n")
	}
	w.Write([]byte(b.String()))
}

func (s *StatusServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data := s.getStatusData()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	dashboardTmpl.Execute(w, data)
}

var dashboardTmpl = template.Must(template.New("dashboard").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>SOCKS5 Proxy Router Console</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@400;500;600;700;800&family=JetBrains+Mono:wght@400;500;600;700&display=swap" rel="stylesheet">
<style>
:root {
  --bg-app: #07090e;
  --bg-surface: #0c1017;
  --bg-card: rgba(16, 23, 34, 0.7);
  --bg-card-hover: rgba(22, 32, 48, 0.85);
  --border-subtle: rgba(255, 255, 255, 0.08);
  --border-focus: rgba(56, 189, 248, 0.45);
  --accent-sky: #38bdf8;
  --accent-emerald: #10b981;
  --accent-amber: #f59e0b;
  --accent-rose: #f43f5e;
  --accent-purple: #a855f7;
  --text-primary: #f8fafc;
  --text-secondary: #94a3b8;
  --text-muted: #64748b;
  --font-sans: 'Plus Jakarta Sans', system-ui, -apple-system, sans-serif;
  --font-mono: 'JetBrains Mono', ui-monospace, Menlo, Monaco, Consolas, monospace;
  --radius-sm: 6px;
  --radius-md: 10px;
  --radius-lg: 14px;
  --radius-pill: 9999px;
  --shadow-panel: 0 4px 20px -2px rgba(0, 0, 0, 0.45), inset 0 1px 0 rgba(255, 255, 255, 0.05);
}

* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

body {
  font-family: var(--font-sans);
  background-color: var(--bg-app);
  background-image: 
    radial-gradient(ellipse 70% 30% at 50% 0%, rgba(56, 189, 248, 0.07) 0%, transparent 60%),
    radial-gradient(ellipse 50% 30% at 85% 90%, rgba(16, 185, 129, 0.04) 0%, transparent 50%);
  background-attachment: fixed;
  color: var(--text-primary);
  min-height: 100dvh;
  display: flex;
  flex-direction: column;
  -webkit-font-smoothing: antialiased;
}

/* Header */
.app-header {
  height: 60px;
  background: rgba(12, 16, 23, 0.88);
  backdrop-filter: blur(16px);
  -webkit-backdrop-filter: blur(16px);
  border-bottom: 1px solid var(--border-subtle);
  display: flex;
  align-items: center;
  position: sticky;
  top: 0;
  z-index: 100;
  padding: 0 20px;
}

.header-inner {
  width: 100%;
  max-width: 1440px;
  margin: 0 auto;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.brand-group {
  display: flex;
  align-items: center;
  gap: 12px;
}

.brand-icon-box {
  width: 34px;
  height: 34px;
  border-radius: var(--radius-md);
  background: linear-gradient(135deg, rgba(56, 189, 248, 0.22), rgba(16, 185, 129, 0.15));
  border: 1px solid rgba(56, 189, 248, 0.35);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--accent-sky);
  box-shadow: 0 0 14px rgba(56, 189, 248, 0.15);
}

.brand-text {
  font-size: 1.05rem;
  font-weight: 800;
  letter-spacing: -0.02em;
  color: #fff;
  display: flex;
  align-items: center;
  gap: 8px;
}

.status-pill {
  font-size: 0.65rem;
  font-weight: 700;
  padding: 2px 7px;
  border-radius: var(--radius-pill);
  background: rgba(16, 185, 129, 0.15);
  color: var(--accent-emerald);
  border: 1px solid rgba(16, 185, 129, 0.3);
  display: inline-flex;
  align-items: center;
  gap: 5px;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.pulse-dot {
  width: 6px;
  height: 6px;
  background-color: var(--accent-emerald);
  border-radius: 50%;
  box-shadow: 0 0 8px var(--accent-emerald);
  animation: pulseDot 2s infinite ease-in-out;
}

@keyframes pulseDot {
  0%, 100% { transform: scale(0.9); opacity: 0.8; }
  50% { transform: scale(1.4); opacity: 1; }
}

.header-tools {
  display: flex;
  align-items: center;
  gap: 10px;
}

.listen-badge {
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-pill);
  padding: 5px 12px;
  font-family: var(--font-mono);
  font-size: 0.78rem;
  color: var(--accent-sky);
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  transition: all 0.18s ease;
}

.listen-badge:hover {
  background: rgba(56, 189, 248, 0.1);
  border-color: rgba(56, 189, 248, 0.35);
}

.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 6px 12px;
  border-radius: var(--radius-md);
  font-size: 0.8rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.16s ease;
  border: none;
  font-family: var(--font-sans);
  user-select: none;
}

.btn:active {
  transform: scale(0.97);
}

.btn-primary {
  background: linear-gradient(135deg, #0284c7, #0369a1);
  color: #fff;
  border: 1px solid rgba(56, 189, 248, 0.3);
  box-shadow: 0 2px 8px rgba(2, 132, 199, 0.3);
}

.btn-primary:hover:not(:disabled) {
  background: linear-gradient(135deg, #0ea5e9, #0284c7);
  box-shadow: 0 4px 14px rgba(2, 132, 199, 0.45);
}

.btn-primary:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.btn-ghost {
  background: rgba(255, 255, 255, 0.04);
  color: var(--text-secondary);
  border: 1px solid var(--border-subtle);
}

.btn-ghost:hover {
  background: rgba(255, 255, 255, 0.08);
  color: #fff;
  border-color: rgba(255, 255, 255, 0.15);
}

.btn-success {
  background: rgba(16, 185, 129, 0.1);
  color: var(--accent-emerald);
  border: 1px solid rgba(16, 185, 129, 0.3);
}

.btn-success:hover:not(:disabled) {
  background: rgba(16, 185, 129, 0.2);
  color: #fff;
}

.icon-spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* Active Tunnel Top Bar */
.tunnel-bar {
  background: linear-gradient(90deg, rgba(14, 21, 33, 0.98), rgba(9, 14, 22, 0.98));
  border-bottom: 1px solid rgba(56, 189, 248, 0.25);
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.35);
  padding: 12px 20px;
}

.tunnel-inner {
  max-width: 1440px;
  margin: 0 auto;
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}

.tunnel-left {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
}

.tunnel-status-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 0.72rem;
  font-weight: 700;
  text-transform: uppercase;
  color: var(--accent-emerald);
  background: rgba(16, 185, 129, 0.12);
  border: 1px solid rgba(16, 185, 129, 0.25);
  padding: 3px 8px;
  border-radius: var(--radius-sm);
  letter-spacing: 0.04em;
}

.tunnel-main-addr {
  font-family: var(--font-mono);
  font-size: 1.15rem;
  font-weight: 700;
  color: #fff;
  display: flex;
  align-items: center;
  gap: 10px;
}

.tunnel-loc {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--text-secondary);
  font-size: 0.85rem;
}

.flag-emoji {
  font-size: 1.15rem;
  line-height: 1;
}

.tunnel-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.latency-pill {
  font-family: var(--font-mono);
  font-size: 0.8rem;
  font-weight: 700;
  padding: 3px 9px;
  border-radius: var(--radius-pill);
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.latency-fast {
  background: rgba(16, 185, 129, 0.15);
  color: var(--accent-emerald);
  border: 1px solid rgba(16, 185, 129, 0.3);
}

.latency-mid {
  background: rgba(245, 158, 11, 0.15);
  color: var(--accent-amber);
  border: 1px solid rgba(245, 158, 11, 0.3);
}

.latency-slow {
  background: rgba(244, 63, 94, 0.15);
  color: var(--accent-rose);
  border: 1px solid rgba(244, 63, 94, 0.3);
}

/* 2-Column Main Workspace */
.app-layout {
  flex: 1;
  max-width: 1440px;
  width: 100%;
  margin: 18px auto 0;
  padding: 0 20px 24px;
  display: grid;
  grid-template-columns: 350px 1fr;
  gap: 18px;
}

/* Left Sidebar Console */
.sidebar {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.console-card {
  background: var(--bg-card);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  padding: 16px;
  box-shadow: var(--shadow-panel);
}

.card-title-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.card-heading {
  font-size: 0.76rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-muted);
  display: flex;
  align-items: center;
  gap: 6px;
}

.stat-grid-compact {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
  margin-bottom: 12px;
}

.stat-box {
  background: rgba(0, 0, 0, 0.25);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  padding: 10px 12px;
}

.stat-box-label {
  font-size: 0.68rem;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
}

.stat-box-val {
  font-family: var(--font-mono);
  font-size: 1.25rem;
  font-weight: 700;
  color: #fff;
  margin-top: 2px;
  display: flex;
  align-items: baseline;
  gap: 4px;
}

.stat-box-val small {
  font-size: 0.72rem;
  color: var(--text-muted);
  font-weight: 500;
}

.progress-track {
  width: 100%;
  height: 5px;
  background: rgba(255, 255, 255, 0.08);
  border-radius: var(--radius-pill);
  overflow: hidden;
  display: flex;
  margin: 6px 0 10px;
}

.fill-item {
  height: 100%;
  transition: width 0.3s ease;
}

.fill-green { background: var(--accent-emerald); }
.fill-amber { background: var(--accent-amber); }
.fill-rose { background: var(--accent-rose); }

.countdown-bar {
  background: rgba(0, 0, 0, 0.25);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  padding: 8px 12px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 0.78rem;
}

.countdown-time {
  font-family: var(--font-mono);
  font-weight: 700;
  color: var(--accent-sky);
}

/* Snippets Tabs */
.snippet-tabs {
  display: flex;
  gap: 6px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

.tab-btn {
  padding: 4px 9px;
  border-radius: var(--radius-sm);
  background: rgba(255, 255, 255, 0.04);
  color: var(--text-secondary);
  font-size: 0.74rem;
  font-weight: 600;
  cursor: pointer;
  border: 1px solid transparent;
  transition: all 0.15s ease;
}

.tab-btn.active {
  background: rgba(56, 189, 248, 0.15);
  color: var(--accent-sky);
  border-color: rgba(56, 189, 248, 0.35);
}

.code-field {
  background: #04060a;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  padding: 8px 10px;
  font-family: var(--font-mono);
  font-size: 0.76rem;
  color: #38bdf8;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  word-break: break-all;
}

/* Activity Logs */
.log-stream {
  display: flex;
  flex-direction: column;
  gap: 7px;
  max-height: 180px;
  overflow-y: auto;
}

.log-item {
  font-size: 0.75rem;
  display: flex;
  gap: 8px;
  align-items: baseline;
  color: var(--text-secondary);
}

.log-time {
  font-family: var(--font-mono);
  color: var(--text-muted);
  font-size: 0.7rem;
  flex-shrink: 0;
}

.log-badge-switch { color: var(--accent-sky); }
.log-badge-refresh { color: var(--accent-emerald); }
.log-badge-test { color: var(--accent-amber); }
.log-badge-system { color: var(--accent-purple); }

/* Right Column: Proxy Pool Explorer */
.explorer-panel {
  background: var(--bg-card);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  padding: 18px 20px;
  box-shadow: var(--shadow-panel);
  display: flex;
  flex-direction: column;
}

.explorer-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 16px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border-subtle);
}

.search-wrap {
  position: relative;
  flex: 1;
  min-width: 220px;
}

.search-box-input {
  width: 100%;
  background: #04060a;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  padding: 8px 32px 8px 36px;
  color: #fff;
  font-family: var(--font-sans);
  font-size: 0.82rem;
  outline: none;
  transition: all 0.18s ease;
}

.search-box-input:focus {
  border-color: var(--accent-sky);
  box-shadow: 0 0 0 2px rgba(56, 189, 248, 0.18);
}

.search-ico {
  position: absolute;
  left: 11px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--text-muted);
  pointer-events: none;
}

.clear-ico {
  position: absolute;
  right: 10px;
  top: 50%;
  transform: translateY(-50%);
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  display: none;
  font-size: 0.95rem;
}

.clear-ico:hover {
  color: #fff;
}

.filter-set {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.filter-chip {
  padding: 5px 11px;
  border-radius: var(--radius-sm);
  background: rgba(255, 255, 255, 0.04);
  color: var(--text-secondary);
  border: 1px solid var(--border-subtle);
  font-size: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s ease;
}

.filter-chip:hover {
  background: rgba(255, 255, 255, 0.08);
  color: #fff;
}

.filter-chip.active {
  background: rgba(56, 189, 248, 0.15);
  color: var(--accent-sky);
  border-color: rgba(56, 189, 248, 0.35);
}

.view-segment {
  display: flex;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  padding: 2px;
}

.segment-btn {
  padding: 4px 7px;
  background: none;
  border: none;
  color: var(--text-muted);
  border-radius: 4px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s ease;
}

.segment-btn.active {
  background: rgba(255, 255, 255, 0.12);
  color: #fff;
}

/* Proxy Card List */
.proxies-stack {
  display: flex;
  flex-direction: column;
  gap: 8px;
  overflow-y: auto;
  max-height: calc(100vh - 240px);
  padding-right: 4px;
}

.proxy-card-item {
  background: rgba(11, 15, 22, 0.85);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  padding: 10px 14px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  transition: all 0.18s ease;
  cursor: pointer;
}

.proxy-card-item:hover {
  background: var(--bg-card-hover);
  border-color: rgba(255, 255, 255, 0.18);
  transform: translateX(2px);
}

.proxy-card-item.active {
  background: rgba(16, 185, 129, 0.08);
  border-color: rgba(16, 185, 129, 0.4);
  box-shadow: 0 0 14px rgba(16, 185, 129, 0.08);
}

.card-left {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.card-idx {
  font-family: var(--font-mono);
  font-size: 0.72rem;
  color: var(--text-muted);
  width: 24px;
  text-align: center;
  flex-shrink: 0;
}

.card-center {
  min-width: 0;
}

.card-addr-line {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.card-addr {
  font-family: var(--font-mono);
  font-size: 0.92rem;
  font-weight: 600;
  color: #fff;
}

.card-meta-line {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.78rem;
  color: var(--text-secondary);
  margin-top: 2px;
}

.card-right {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}

.switch-cta-btn {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border-subtle);
  color: var(--text-secondary);
  border-radius: var(--radius-sm);
  padding: 4px 10px;
  font-size: 0.74rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s ease;
}

.proxy-card-item:hover .switch-cta-btn {
  background: var(--accent-sky);
  color: #06090e;
  border-color: var(--accent-sky);
}

.proxy-card-item.active .switch-cta-btn {
  background: rgba(16, 185, 129, 0.15);
  color: var(--accent-emerald);
  border-color: rgba(16, 185, 129, 0.3);
  pointer-events: none;
}

/* Dense Table */
.dense-table-wrap {
  overflow-x: auto;
  max-height: calc(100vh - 240px);
}

.compact-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.8rem;
  text-align: left;
}

.compact-table th {
  padding: 9px 12px;
  color: var(--text-muted);
  font-weight: 700;
  font-size: 0.7rem;
  text-transform: uppercase;
  border-bottom: 1px solid var(--border-subtle);
  position: sticky;
  top: 0;
  background: rgba(11, 15, 22, 0.95);
  backdrop-filter: blur(8px);
}

.compact-table td {
  padding: 8px 12px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.04);
}

.compact-table tr:hover td {
  background: rgba(255, 255, 255, 0.03);
}

.compact-table tr.active td {
  background: rgba(16, 185, 129, 0.06);
}

/* Empty View */
.no-data {
  text-align: center;
  padding: 48px 16px;
  color: var(--text-muted);
}

.no-data-title {
  font-size: 0.95rem;
  font-weight: 700;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

/* Toast Notifications */
.toast-box {
  position: fixed;
  bottom: 20px;
  right: 20px;
  z-index: 9999;
  display: flex;
  flex-direction: column;
  gap: 8px;
  pointer-events: none;
}

.toast-pill {
  background: rgba(16, 23, 34, 0.95);
  backdrop-filter: blur(12px);
  border: 1px solid rgba(56, 189, 248, 0.35);
  color: #fff;
  padding: 9px 15px;
  border-radius: var(--radius-md);
  font-size: 0.8rem;
  font-weight: 600;
  box-shadow: 0 10px 24px rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  gap: 8px;
  opacity: 0;
  transform: translateY(10px);
  animation: toastShow 0.2s forwards, toastHide 0.2s forwards 2.8s;
  pointer-events: auto;
}

@keyframes toastShow { to { opacity: 1; transform: translateY(0); } }
@keyframes toastHide { to { opacity: 0; transform: translateY(-8px); } }

@media (max-width: 960px) {
  .app-layout { grid-template-columns: 1fr; }
  .tunnel-inner { flex-direction: column; align-items: flex-start; }
  .tunnel-right { width: 100%; justify-content: space-between; }
}
</style>
</head>
<body>

<!-- Header -->
<header class="app-header">
  <div class="header-inner">
    <div class="brand-group">
      <div class="brand-icon-box">
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
          <rect x="2" y="2" width="20" height="8" rx="2" ry="2"></rect>
          <rect x="2" y="14" width="20" height="8" rx="2" ry="2"></rect>
          <line x1="6" y1="6" x2="6.01" y2="6"></line>
          <line x1="6" y1="18" x2="6.01" y2="18"></line>
        </svg>
      </div>
      <div class="brand-text">
        SOCKS5 Router
        <span class="status-pill"><span class="pulse-dot"></span> Online</span>
      </div>
    </div>

    <div class="header-tools">
      <div class="listen-badge" onclick="copyText('{{if .ListenAddr}}{{.ListenAddr}}{{else}}127.0.0.1:1080{{end}}', 'Local SOCKS5 endpoint copied!')" title="Click to copy local listen address">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>
        <span id="nav-endpoint">{{if .ListenAddr}}{{.ListenAddr}}{{else}}127.0.0.1:1080{{end}}</span>
      </div>

      <button id="btn-test-route" class="btn btn-success" onclick="testActiveRoute()" title="Test Google HTTPS TLS Handshake">
        <svg id="ico-test" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path><polyline points="22 4 12 14.01 9 11.01"></polyline></svg>
        <span id="txt-test">Test Route</span>
      </button>

      <button id="btn-refresh-pool" class="btn btn-primary" onclick="triggerPoolRefresh()">
        <svg id="ico-refresh" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="23 4 23 10 17 10"></polyline>
          <polyline points="1 20 1 14 7 14"></polyline>
          <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path>
        </svg>
        <span id="txt-refresh">Refresh</span>
      </button>

      <a href="/api/export" class="btn btn-ghost" title="Export as text list">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path><polyline points="7 10 12 15 17 10"></polyline><line x1="12" y1="15" x2="12" y2="3"></line></svg>
        Export
      </a>

      <a href="https://github.com/Dreamy-rain/socks5-proxy" target="_blank" rel="noopener" class="btn btn-ghost" title="GitHub">
        <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
          <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/>
        </svg>
      </a>
    </div>
  </div>
</header>

<!-- Active Tunnel Pinned Top Bar -->
<section class="tunnel-bar">
  <div class="tunnel-inner">
    <div class="tunnel-left">
      <span class="tunnel-status-chip">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3"><polyline points="20 6 9 17 4 12"></polyline></svg>
        Active Tunnel
      </span>
      <div class="tunnel-main-addr">
        <span id="active-addr-display">{{.ActiveProxy}}</span>
        <button class="btn btn-ghost" style="padding:2px 7px;font-size:0.7rem;" onclick="copyActiveProxy()" title="Copy SOCKS5 URL">Copy</button>
      </div>
      <div class="tunnel-loc">
        <span class="flag-emoji" id="active-flag-emoji">🌐</span>
        <span id="active-geo-display">{{.ActiveRegion}}</span>
      </div>
    </div>

    <div class="tunnel-right">
      <div id="active-latency-display">
        {{if gt .ActiveLatencyMs 0}}
        <span class="latency-pill {{if lt .ActiveLatencyMs 500}}latency-fast{{else if lt .ActiveLatencyMs 1200}}latency-mid{{else}}latency-slow{{end}}">
          ⚡ {{.ActiveLatencyMs}} ms
        </span>
        {{end}}
      </div>
      <button class="btn btn-ghost" onclick="rotateNextProxy()" title="Switch to next proxy in pool">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="16 3 21 3 21 8"></polyline><line x1="4" y1="20" x2="21" y2="3"></line><polyline points="21 16 21 21 16 21"></polyline><line x1="15" y1="15" x2="21" y2="21"></line><line x1="4" y1="4" x2="9" y2="9"></line></svg>
        Rotate Route
      </button>
    </div>
  </div>
</section>

<!-- Main Workspace -->
<main class="app-layout">
  <!-- Left Console -->
  <aside class="sidebar">
    <!-- Pool Health & Stats -->
    <div class="console-card">
      <div class="card-title-row">
        <span class="card-heading">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"></circle><polyline points="12 6 12 12 16 14"></polyline></svg>
          Pool Metrics
        </span>
        <span style="font-size:0.72rem;color:var(--text-muted);" id="country-count-val">{{.CountriesCount}} countries</span>
      </div>

      <div class="stat-grid-compact">
        <div class="stat-box">
          <div class="stat-box-label">Verified Alive</div>
          <div class="stat-box-val" id="total-val">{{.Total}} <small>live</small></div>
        </div>
        <div class="stat-box">
          <div class="stat-box-label">Lowest Ping</div>
          <div class="stat-box-val" id="fastest-val">{{if gt .FastestLatencyMs 0}}{{.FastestLatencyMs}}<small>ms</small>{{else}}-{{end}}</div>
        </div>
      </div>

      <div class="progress-track" title="Latency Distribution">
        <div class="fill-item fill-green" id="bar-green" style="width:40%"></div>
        <div class="fill-item fill-amber" id="bar-amber" style="width:40%"></div>
        <div class="fill-item fill-rose" id="bar-rose" style="width:20%"></div>
      </div>

      <div class="countdown-bar">
        <span style="color:var(--text-muted)">Next Auto Scrape:</span>
        <span class="countdown-time" id="countdown-val">--:--</span>
      </div>
    </div>

    <!-- Quick Integrations -->
    <div class="console-card">
      <div class="card-title-row">
        <span class="card-heading">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="16 18 22 12 16 6"></polyline><polyline points="8 6 2 12 8 18"></polyline></svg>
          Client Snippets
        </span>
      </div>

      <div class="snippet-tabs">
        <button class="tab-btn active" onclick="pickSnippetTab('curl', this)">cURL</button>
        <button class="tab-btn" onclick="pickSnippetTab('python', this)">Python</button>
        <button class="tab-btn" onclick="pickSnippetTab('env', this)">Env</button>
        <button class="tab-btn" onclick="pickSnippetTab('git', this)">Git</button>
        <button class="tab-btn" onclick="pickSnippetTab('telegram', this)">Telegram</button>
      </div>

      <div class="code-field">
        <span id="snippet-display">curl -x socks5h://127.0.0.1:1080 https://api.ipify.org</span>
        <button class="btn btn-ghost" style="padding:2px 7px;font-size:0.7rem;" onclick="copyActiveSnippet()">Copy</button>
      </div>
    </div>

    <!-- Activity Log -->
    <div class="console-card">
      <div class="card-title-row">
        <span class="card-heading">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="8" y1="6" x2="21" y2="6"></line><line x1="8" y1="12" x2="21" y2="12"></line><line x1="8" y1="18" x2="21" y2="18"></line><line x1="3" y1="6" x2="3.01" y2="6"></line><line x1="3" y1="12" x2="3.01" y2="12"></line><line x1="3" y1="18" x2="3.01" y2="18"></line></svg>
          Activity Stream
        </span>
      </div>
      <div class="log-stream" id="log-list">
        {{if .Logs}}
          {{range .Logs}}
          <div class="log-item">
            <span class="log-time">{{.Time}}</span>
            <span class="log-badge-{{.Type}}">•</span>
            <span>{{.Message}}</span>
          </div>
          {{end}}
        {{else}}
          <div class="log-item" style="color:var(--text-muted)">Waiting for events...</div>
        {{end}}
      </div>
    </div>
  </aside>

  <!-- Right Explorer -->
  <section class="explorer-panel">
    <div class="explorer-toolbar">
      <div class="search-wrap">
        <svg class="search-ico" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>
        <input type="text" id="proxy-search" class="search-box-input" placeholder="Search IP, Port, Country, City..." oninput="handleSearchInput(this.value)">
        <button id="clear-search-btn" class="clear-ico" onclick="resetSearch()">✕</button>
      </div>

      <div class="filter-set">
        <button class="filter-chip active" onclick="changeFilter('all', this)">All (<span id="all-count-badge">{{.Total}}</span>)</button>
        <button class="filter-chip" onclick="changeFilter('fast', this)">⚡ Fast &lt;500ms</button>
        <button class="filter-chip" onclick="changeFilter('mid', this)">Normal &lt;1.2s</button>
        <button class="filter-chip" onclick="changeFilter('slow', this)">Standard</button>

        <div class="view-segment">
          <button class="segment-btn active" id="btn-mode-card" onclick="switchViewMode('card')" title="Card View">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7"></rect><rect x="14" y="3" width="7" height="7"></rect><rect x="14" y="14" width="7" height="7"></rect><rect x="3" y="14" width="7" height="7"></rect></svg>
          </button>
          <button class="segment-btn" id="btn-mode-table" onclick="switchViewMode('table')" title="Dense Table View">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="8" y1="6" x2="21" y2="6"></line><line x1="8" y1="12" x2="21" y2="12"></line><line x1="8" y1="18" x2="21" y2="18"></line><line x1="3" y1="6" x2="3.01" y2="6"></line><line x1="3" y1="12" x2="3.01" y2="12"></line><line x1="3" y1="18" x2="3.01" y2="18"></line></svg>
          </button>
        </div>
      </div>
    </div>

    <!-- Proxy List View -->
    <div id="proxy-render-target">
      <div class="proxies-stack" id="card-stack-view">
        {{if .Proxies}}
          {{range .Proxies}}
          <div class="proxy-card-item{{if .Active}} active{{end}}" onclick="routeToProxy({{.Index}}, this)">
            <div class="card-left">
              <span class="card-idx">#{{.Index}}</span>
              <div class="card-center">
                <div class="card-addr-line">
                  <span class="card-addr">{{.Addr}}</span>
                  <span class="latency-pill {{if lt .LatencyMs 500}}latency-fast{{else if lt .LatencyMs 1200}}latency-mid{{else}}latency-slow{{end}}">
                    {{.LatencyMs}} ms
                  </span>
                </div>
                <div class="card-meta-line">
                  <span>{{.Country}}{{if .City}}, {{.City}}{{end}}</span>
                </div>
              </div>
            </div>
            <div class="card-right">
              <button class="switch-cta-btn">{{if .Active}}IN USE{{else}}Switch Route{{end}}</button>
            </div>
          </div>
          {{end}}
        {{else}}
          <div class="no-data">
            <div class="no-data-title">Scanning candidate proxies...</div>
            <p>Live proxies passing TLS connectivity will appear automatically.</p>
          </div>
        {{end}}
      </div>
    </div>
  </section>
</main>

<!-- Toast Shelf -->
<div class="toast-box" id="toast-shelf"></div>

<script>
let appState = null;
let activeFilter = 'all';
let searchKeyword = '';
let currentViewMode = 'card';
let selectedSnippet = 'curl';
let nextScrapeStamp = {{if .NextScrapeUnix}}{{.NextScrapeUnix}}{{else}}0{{end}};

document.addEventListener('DOMContentLoaded', () => {
  setupCountdown();
  updateFlagEmoji('{{.ActiveCountryCode}}');
  // Background live poll (4 seconds)
  setInterval(refreshStatusFeed, 4000);
});

function getFlagEmoji(countryCode) {
  if (!countryCode || countryCode.length !== 2) return '🌐';
  const codePoints = countryCode
    .toUpperCase()
    .split('')
    .map(char => 127397 + char.charCodeAt());
  return String.fromCodePoint(...codePoints);
}

function updateFlagEmoji(code) {
  const el = document.getElementById('active-flag-emoji');
  if (el) el.textContent = getFlagEmoji(code);
}

function refreshStatusFeed() {
  fetch('/api/status')
    .then(r => r.json())
    .then(data => {
      appState = data;
      nextScrapeStamp = data.next_scrape_unix;
      renderApp(data);
    })
    .catch(err => console.debug('Sync feed notice:', err));
}

function renderApp(d) {
  if (d.listen_addr) {
    const navEp = document.getElementById('nav-endpoint');
    if (navEp) navEp.textContent = d.listen_addr;
  }

  // Active tunnel bar
  const addrDisplay = document.getElementById('active-addr-display');
  if (addrDisplay) addrDisplay.textContent = d.active_proxy;

  const geoDisplay = document.getElementById('active-geo-display');
  if (geoDisplay) geoDisplay.textContent = d.active_region;

  updateFlagEmoji(d.active_country_code);

  const latDisplay = document.getElementById('active-latency-display');
  if (latDisplay && d.active_latency_ms > 0) {
    const latClass = d.active_latency_ms < 500 ? 'latency-fast' : (d.active_latency_ms < 1200 ? 'latency-mid' : 'latency-slow');
    latDisplay.innerHTML = '<span class="latency-pill ' + latClass + '">⚡ ' + d.active_latency_ms + ' ms</span>';
  }

  // Sidebar stats
  const totalVal = document.getElementById('total-val');
  if (totalVal) totalVal.innerHTML = d.total + ' <small>live</small>';

  const fastestVal = document.getElementById('fastest-val');
  if (fastestVal) fastestVal.innerHTML = (d.fastest_latency_ms > 0 ? d.fastest_latency_ms + '<small>ms</small>' : '-');

  const countryCount = document.getElementById('country-count-val');
  if (countryCount) countryCount.textContent = d.countries_count + ' countries';

  const allBadge = document.getElementById('all-count-badge');
  if (allBadge) allBadge.textContent = d.total;

  // Meter bar
  if (d.total > 0) {
    const fPct = ((d.fast_count || 0) / d.total) * 100;
    const mPct = ((d.mid_count || 0) / d.total) * 100;
    const sPct = ((d.slow_count || 0) / d.total) * 100;
    const bg = document.getElementById('bar-green');
    if (bg) bg.style.width = fPct + '%';
    const ba = document.getElementById('bar-amber');
    if (ba) ba.style.width = mPct + '%';
    const br = document.getElementById('bar-rose');
    if (br) br.style.width = sPct + '%';
  }

  // Update logs
  if (d.logs) {
    const logList = document.getElementById('log-list');
    if (logList) {
      let logHtml = '';
      d.logs.forEach(l => {
        logHtml += '<div class="log-item">' +
          '<span class="log-time">' + escapeHtml(l.time) + '</span>' +
          '<span class="log-badge-' + escapeHtml(l.type) + '">•</span>' +
          '<span>' + escapeHtml(l.message) + '</span>' +
        '</div>';
      });
      logList.innerHTML = logHtml;
    }
  }

  renderExplorer(d.proxies);
}

function renderExplorer(proxies) {
  const target = document.getElementById('proxy-render-target');
  if (!target) return;

  if (!proxies || proxies.length === 0) {
    target.innerHTML = '<div class="no-data"><div class="no-data-title">No proxies available</div><p>Click "Refresh" to scan candidate proxies.</p></div>';
    return;
  }

  const q = searchKeyword.toLowerCase().trim();
  const filtered = proxies.filter(p => {
    if (activeFilter === 'fast' && p.latency_ms >= 500) return false;
    if (activeFilter === 'mid' && (p.latency_ms < 500 || p.latency_ms >= 1200)) return false;
    if (activeFilter === 'slow' && p.latency_ms < 1200) return false;

    if (q) {
      const matchA = p.addr.toLowerCase().includes(q);
      const matchC = (p.country || '').toLowerCase().includes(q);
      const matchCity = (p.city || '').toLowerCase().includes(q);
      return matchA || matchC || matchCity;
    }
    return true;
  });

  if (filtered.length === 0) {
    target.innerHTML = '<div class="no-data"><div class="no-data-title">No matching proxies</div><p>Try clearing your search query or filter tags.</p></div>';
    return;
  }

  if (currentViewMode === 'table') {
    let html = '<div class="dense-table-wrap"><table class="compact-table"><thead><tr>' +
      '<th>#</th><th>Flag</th><th>SOCKS5 Address</th><th>Location</th><th>Latency</th><th>Action</th></tr></thead><tbody>';
    filtered.forEach(p => {
      const latClass = p.latency_ms < 500 ? 'latency-fast' : (p.latency_ms < 1200 ? 'latency-mid' : 'latency-slow');
      const activeClass = p.active ? ' class="active"' : '';
      const btnText = p.active ? 'IN USE' : 'Switch';
      const loc = p.city ? (p.country + ', ' + p.city) : (p.country || 'Unknown');
      const flag = getFlagEmoji(p.country_code);

      html += '<tr' + activeClass + ' onclick="routeToProxy(' + p.index + ', this)">' +
        '<td style="font-family:var(--font-mono);color:var(--text-muted)">#' + p.index + '</td>' +
        '<td><span class="flag-emoji">' + flag + '</span></td>' +
        '<td><span style="font-family:var(--font-mono);font-weight:600;color:#fff">' + escapeHtml(p.addr) + '</span></td>' +
        '<td>' + escapeHtml(loc) + '</td>' +
        '<td><span class="latency-pill ' + latClass + '">' + p.latency_ms + ' ms</span></td>' +
        '<td><button class="switch-cta-btn">' + btnText + '</button></td>' +
      '</tr>';
    });
    html += '</tbody></table></div>';
    target.innerHTML = html;
  } else {
    let html = '<div class="proxies-stack">';
    filtered.forEach(p => {
      const latClass = p.latency_ms < 500 ? 'latency-fast' : (p.latency_ms < 1200 ? 'latency-mid' : 'latency-slow');
      const activeClass = p.active ? ' active' : '';
      const btnText = p.active ? 'IN USE' : 'Switch Route';
      const loc = p.city ? (p.country + ', ' + p.city) : (p.country || 'Unknown');
      const flag = getFlagEmoji(p.country_code);

      html += '<div class="proxy-card-item' + activeClass + '" onclick="routeToProxy(' + p.index + ', this)">' +
        '<div class="card-left">' +
          '<span class="card-idx">#' + p.index + '</span>' +
          '<span class="flag-emoji">' + flag + '</span>' +
          '<div class="card-center">' +
            '<div class="card-addr-line">' +
              '<span class="card-addr">' + escapeHtml(p.addr) + '</span>' +
              '<span class="latency-pill ' + latClass + '">' + p.latency_ms + ' ms</span>' +
            '</div>' +
            '<div class="card-meta-line">' +
              '<span>' + escapeHtml(loc) + '</span>' +
            '</div>' +
          '</div>' +
        '</div>' +
        '<div class="card-right">' +
          '<button class="switch-cta-btn">' + btnText + '</button>' +
        '</div>' +
      '</div>';
    });
    html += '</div>';
    target.innerHTML = html;
  }
}

function handleSearchInput(val) {
  searchKeyword = val;
  const clearBtn = document.getElementById('clear-search-btn');
  if (clearBtn) clearBtn.style.display = val ? 'block' : 'none';
  if (appState) renderExplorer(appState.proxies);
}

function resetSearch() {
  const inp = document.getElementById('proxy-search');
  if (inp) inp.value = '';
  handleSearchInput('');
}

function changeFilter(f, btn) {
  activeFilter = f;
  document.querySelectorAll('.filter-chip').forEach(b => b.classList.remove('active'));
  btn.classList.add('active');
  if (appState) renderExplorer(appState.proxies);
}

function switchViewMode(mode) {
  currentViewMode = mode;
  document.getElementById('btn-mode-card').classList.toggle('active', mode === 'card');
  document.getElementById('btn-mode-table').classList.toggle('active', mode === 'table');
  if (appState) renderExplorer(appState.proxies);
}

function routeToProxy(idx, el) {
  if (el && el.classList.contains('active')) return;
  if (el) el.style.opacity = '0.5';

  fetch('/api/switch?index=' + idx)
    .then(r => r.json())
    .then(res => {
      if (res.status === 'ok') {
        showToast('Switched to #' + idx + ' (' + (res.proxy || 'active') + ')');
        refreshStatusFeed();
      } else {
        if (el) el.style.opacity = '1';
        showToast('Switch error: ' + (res.status || 'unknown'));
      }
    })
    .catch(() => {
      if (el) el.style.opacity = '1';
      showToast('Network error during switch');
    });
}

function rotateNextProxy() {
  fetch('/api/switch')
    .then(r => r.json())
    .then(res => {
      if (res.status === 'ok') {
        showToast('Rotated to route: ' + res.proxy);
        refreshStatusFeed();
      } else {
        showToast('Rotate failed: ' + (res.status || 'No proxies'));
      }
    })
    .catch(() => showToast('Rotation request failed'));
}

function testActiveRoute() {
  const btn = document.getElementById('btn-test-route');
  const ico = document.getElementById('ico-test');
  const txt = document.getElementById('txt-test');

  if (btn) btn.disabled = true;
  if (txt) txt.textContent = 'Testing...';
  if (ico) ico.classList.add('icon-spin');

  fetch('/api/test')
    .then(r => r.json())
    .then(res => {
      if (btn) btn.disabled = false;
      if (ico) ico.classList.remove('icon-spin');
      if (txt) txt.textContent = 'Test Route';

      if (res.status === 'ok') {
        showToast('Route Verified! Ping: ' + res.latency_ms + ' ms (' + res.country + ')');
        refreshStatusFeed();
      } else {
        showToast('Route check failed: ' + (res.message || 'unreachable'));
      }
    })
    .catch(() => {
      if (btn) btn.disabled = false;
      if (ico) ico.classList.remove('icon-spin');
      if (txt) txt.textContent = 'Test Route';
      showToast('Health test failed');
    });
}

function triggerPoolRefresh() {
  const btn = document.getElementById('btn-refresh-pool');
  const ico = document.getElementById('ico-refresh');
  const txt = document.getElementById('txt-refresh');

  if (btn) btn.disabled = true;
  if (ico) ico.classList.add('icon-spin');
  if (txt) txt.textContent = 'Scanning...';

  showToast('Scraping & testing fresh proxies (~10s)...');

  fetch('/api/refresh', { method: 'POST' })
    .then(() => {
      setTimeout(() => {
        refreshStatusFeed();
        if (btn) btn.disabled = false;
        if (ico) ico.classList.remove('icon-spin');
        if (txt) txt.textContent = 'Refresh';
        showToast('Proxy pool refresh completed!');
      }, 10000);
    })
    .catch(() => {
      if (btn) btn.disabled = false;
      if (ico) ico.classList.remove('icon-spin');
      if (txt) txt.textContent = 'Refresh';
      showToast('Refresh failed to trigger');
    });
}

function copyActiveProxy() {
  const el = document.getElementById('active-addr-display');
  const txt = el ? el.textContent.trim() : '';
  if (txt && txt !== 'None') {
    copyText('socks5://' + txt, 'SOCKS5 URL copied: socks5://' + txt);
  }
}

function copyText(str, msg) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(str).then(() => showToast(msg || 'Copied to clipboard!'));
  } else {
    const ta = document.createElement('textarea');
    ta.value = str;
    document.body.appendChild(ta);
    ta.select();
    document.execCommand('copy');
    document.body.removeChild(ta);
    showToast(msg || 'Copied to clipboard!');
  }
}

function showToast(msg) {
  const shelf = document.getElementById('toast-shelf');
  if (!shelf) return;
  const t = document.createElement('div');
  t.className = 'toast-pill';
  t.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#38bdf8" stroke-width="2.5"><circle cx="12" cy="12" r="10"></circle><polyline points="12 6 12 12 16 14"></polyline></svg>' +
    '<span>' + escapeHtml(msg) + '</span>';
  shelf.appendChild(t);
  setTimeout(() => {
    if (t.parentNode) t.parentNode.removeChild(t);
  }, 2900);
}

function pickSnippetTab(type, btn) {
  selectedSnippet = type;
  document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
  btn.classList.add('active');

  const ep = (appState && appState.listen_addr) ? appState.listen_addr : '127.0.0.1:1080';
  const val = document.getElementById('snippet-display');
  if (!val) return;

  if (type === 'curl') {
    val.textContent = 'curl -x socks5h://' + ep + ' https://api.ipify.org';
  } else if (type === 'python') {
    val.textContent = "proxies = {'http': 'socks5h://" + ep + "', 'https': 'socks5h://" + ep + "'}";
  } else if (type === 'env') {
    val.textContent = 'export ALL_PROXY=socks5://' + ep;
  } else if (type === 'git') {
    val.textContent = 'git config --global http.proxy socks5://' + ep;
  } else if (type === 'telegram') {
    const parts = ep.split(':');
    val.textContent = 'tg://socks?server=' + (parts[0] || '127.0.0.1') + '&port=' + (parts[1] || '1080');
  }
}

function copyActiveSnippet() {
  const v = document.getElementById('snippet-display');
  if (v) copyText(v.textContent, 'Snippet copied to clipboard!');
}

function setupCountdown() {
  setInterval(() => {
    const el = document.getElementById('countdown-val');
    if (!el || !nextScrapeStamp) return;
    const diff = nextScrapeStamp - Math.floor(Date.now() / 1000);
    if (diff <= 0) {
      el.textContent = 'Scraping...';
      return;
    }
    const m = Math.floor(diff / 60);
    const s = diff % 60;
    el.textContent = (m < 10 ? '0' + m : m) + ':' + (s < 10 ? '0' + s : s);
  }, 1000);
}

function escapeHtml(str) {
  if (!str) return '';
  return str.replace(/[&<>"']/g, function(m) {
    return ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[m];
  });
}
</script>
</body>
</html>`))
