package proxy

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleDashboard(t *testing.T) {
	handler := HandleDashboard()

	req := httptest.NewRequest("GET", "/dashboard", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Agent Veil") && !strings.Contains(body, "dashboard") {
		t.Error("dashboard should contain dashboard content")
	}
}

func TestHandleDashboardStatus(t *testing.T) {
	providers := []string{"openai", "anthropic"}
	handler := HandleDashboardStatus(providers, true)

	req := httptest.NewRequest("GET", "/dashboard/api/status", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()
	if !strings.Contains(body, "openai") {
		t.Error("status should list openai provider")
	}
	if !strings.Contains(body, "anthropic") {
		t.Error("status should list anthropic provider")
	}
}

func TestHandleDashboardReports(t *testing.T) {
	handler := HandleDashboardReports()

	req := httptest.NewRequest("GET", "/dashboard/api/reports", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Result().StatusCode != 200 {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestHandleDashboardLogs(t *testing.T) {
	handler := HandleDashboardLogs()

	req := httptest.NewRequest("GET", "/dashboard/api/logs", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Result().StatusCode != 200 {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestFormatDuration_Coverage(t *testing.T) {
	import_time := []struct {
		name string
		dur  time.Duration
	}{
		{"seconds", 45 * time.Second},
		{"minutes", 2 * time.Minute},
		{"hours", 2 * time.Hour},
		{"days", 24 * time.Hour},
	}

	for _, tt := range import_time {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.dur)
			if result == "" {
				t.Errorf("formatDuration(%v) returned empty", tt.dur)
			}
		})
	}
}
