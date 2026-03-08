package proxy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "embed"
)

//go:embed dashboard.html
var dashboardHTML []byte

var serverStartTime = time.Now()

// HandleDashboard serves the web dashboard UI.
func HandleDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(dashboardHTML)
	}
}

// DashboardStatus represents the dashboard status API response.
type DashboardStatus struct {
	Proxy     bool     `json:"proxy"`
	Redis     bool     `json:"redis"`
	Uptime    string   `json:"uptime"`
	UptimeSec float64  `json:"uptime_sec"`
	Role      string   `json:"role"`
	Providers []string `json:"providers"`
	Router    bool     `json:"router"`
	Timestamp string   `json:"timestamp"`
}

// HandleDashboardStatus returns JSON status of all components.
func HandleDashboardStatus(providers []string, isRouter bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uptime := time.Since(serverStartTime)
		redisOK := true // If we got this far, Redis was checked at startup

		role := os.Getenv("VEIL_DEFAULT_ROLE")
		if role == "" {
			role = "viewer"
		}

		status := DashboardStatus{
			Proxy:     true,
			Redis:     redisOK,
			Uptime:    formatDuration(uptime),
			UptimeSec: uptime.Seconds(),
			Role:      role,
			Providers: providers,
			Router:    isRouter,
			Timestamp: time.Now().Format(time.RFC3339),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}
}

// HandleDashboardLogs returns recent proxy log entries as JSON.
func HandleDashboardLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logDir := filepath.Join(os.Getenv("HOME"), ".agentveil", "logs")
		logFile := filepath.Join(logDir, "proxy.log")

		lines, err := tailFile(logFile, 100)
		if err != nil {
			slog.Warn("failed to read proxy logs", "error", err)
			lines = []string{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"lines": lines,
			"file":  logFile,
		})
	}
}

// HandleDashboardReports returns the latest watchdog reports.
func HandleDashboardReports() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reportDir := filepath.Join(os.Getenv("HOME"), ".agentveil", "reports")
		entries, err := os.ReadDir(reportDir)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"reports": []any{}})
			return
		}

		// Sort newest first
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() > entries[j].Name()
		})

		// Return up to 10 latest
		limit := 10
		if len(entries) < limit {
			limit = len(entries)
		}

		var reports []json.RawMessage
		for _, e := range entries[:limit] {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(reportDir, e.Name()))
			if err == nil {
				reports = append(reports, json.RawMessage(data))
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"reports": reports})
	}
}

func tailFile(path string, n int) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	allLines := strings.Split(string(data), "\n")

	// Remove trailing empty line
	if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
		allLines = allLines[:len(allLines)-1]
	}

	start := 0
	if len(allLines) > n {
		start = len(allLines) - n
	}
	return allLines[start:], nil
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}
