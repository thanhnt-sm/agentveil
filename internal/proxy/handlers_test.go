package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vurakit/agentveil/internal/detector"
	"github.com/vurakit/agentveil/internal/vault"
)

func TestHandleAudit_GET(t *testing.T) {
	handler := HandleAudit()
	req := httptest.NewRequest("GET", "/audit", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET should return 405, got %d", w.Code)
	}
}

func TestHandleAudit_ValidBody(t *testing.T) {
	handler := HandleAudit()
	body := `{"content":"My email is test@example.com and my phone is 0912345678"}`
	req := httptest.NewRequest("POST", "/audit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "risk_level") {
		t.Error("response should contain risk_level")
	}
}

func TestHandleAudit_EmptyBody(t *testing.T) {
	handler := HandleAudit()
	req := httptest.NewRequest("POST", "/audit", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("empty body should return 400, got %d", w.Code)
	}
}

func TestHandleAudit_InvalidJSON(t *testing.T) {
	handler := HandleAudit()
	req := httptest.NewRequest("POST", "/audit", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid JSON should return 400, got %d", w.Code)
	}
}

func TestHandleScan_POST(t *testing.T) {
	det := detector.New()
	handler := HandleScan(det)
	body := `{"text":"Hello, my email is user@domain.com"}`
	req := httptest.NewRequest("POST", "/scan", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "entities") {
		t.Error("response should contain entities")
	}
}

func TestHandleScan_GET(t *testing.T) {
	det := detector.New()
	handler := HandleScan(det)
	req := httptest.NewRequest("GET", "/scan", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET should return 405, got %d", w.Code)
	}
}

func TestHandleScan_EmptyBody(t *testing.T) {
	det := detector.New()
	handler := HandleScan(det)
	req := httptest.NewRequest("POST", "/scan", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("empty body should return 400, got %d", w.Code)
	}
}

func TestHandleDashboardStatus_Router(t *testing.T) {
	providers := []string{"openai", "anthropic"}
	handler := HandleDashboardStatus(providers, true)
	req := httptest.NewRequest("GET", "/dashboard/api/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "openai") {
		t.Error("should contain provider names")
	}
}

func TestHandleDashboardLogs_API(t *testing.T) {
	handler := HandleDashboardLogs()
	req := httptest.NewRequest("GET", "/dashboard/api/logs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleDashboardReports_API(t *testing.T) {
	handler := HandleDashboardReports()
	req := httptest.NewRequest("GET", "/dashboard/api/reports", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAnonymizeRequest_NilBody(t *testing.T) {
	det := detector.New()
	v := setupTestVault(t)
	modifier := AnonymizeRequest(det, v)

	req := httptest.NewRequest("POST", "/v1/chat", nil)
	req.Body = nil
	// Should not panic
	modifier(req)
}

func TestAnonymizeRequest_GETSkips(t *testing.T) {
	det := detector.New()
	v := setupTestVault(t)
	modifier := AnonymizeRequest(det, v)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	// GET should be skipped
	modifier(req)
}

func setupTestVault(t *testing.T) *vault.Vault {
	t.Helper()
	return vault.New("localhost:6379", "", 0)
}
