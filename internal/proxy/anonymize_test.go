package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/vurakit/agentveil/internal/detector"
	"github.com/vurakit/agentveil/internal/vault"
)

func newTestVault(t *testing.T) *vault.Vault {
	t.Helper()
	mr := miniredis.RunT(t)
	return vault.NewWithClient(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
}

// --- extractSessionID tests ---

func TestExtractSessionID_FromXSessionID(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat", nil)
	req.Header.Set("X-Session-ID", "sess-123")
	sid := extractSessionID(req)
	if sid != "sess-123" {
		t.Errorf("expected sess-123, got %s", sid)
	}
}

func TestExtractSessionID_FromXRequestID(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat", nil)
	req.Header.Set("X-Request-ID", "req-456")
	sid := extractSessionID(req)
	if sid != "req-456" {
		t.Errorf("expected req-456, got %s", sid)
	}
}

func TestExtractSessionID_Default(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat", nil)
	sid := extractSessionID(req)
	if sid != "default" {
		t.Errorf("expected default, got %s", sid)
	}
}

func TestExtractSessionID_PreferXSessionID(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat", nil)
	req.Header.Set("X-Session-ID", "sess")
	req.Header.Set("X-Request-ID", "req")
	sid := extractSessionID(req)
	if sid != "sess" {
		t.Errorf("X-Session-ID should take priority, got %s", sid)
	}
}

// --- extractSessionIDFromResponse tests ---

func TestExtractSessionIDFromResponse_WithRequest(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat", nil)
	req.Header.Set("X-Session-ID", "resp-sess")
	resp := &http.Response{Request: req}

	sid := extractSessionIDFromResponse(resp)
	if sid != "resp-sess" {
		t.Errorf("expected resp-sess, got %s", sid)
	}
}

func TestExtractSessionIDFromResponse_NilRequest(t *testing.T) {
	resp := &http.Response{Request: nil}
	sid := extractSessionIDFromResponse(resp)
	if sid != "default" {
		t.Errorf("expected default, got %s", sid)
	}
}

// --- anonymizeBody tests ---

func TestAnonymizeBody_CleanBody(t *testing.T) {
	det := detector.New()
	v := newTestVault(t)

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(body))

	anonymizeBody(req, det, v, "test-session", nil)

	// Read the resulting body
	result, _ := io.ReadAll(req.Body)
	if string(result) != body {
		t.Error("clean body should be unchanged")
	}
}

func TestAnonymizeBody_WithPII(t *testing.T) {
	det := detector.New()
	v := newTestVault(t)

	body := `{"content":"My email is john@example.com and phone 0912345678"}`
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(body))

	anonymizeBody(req, det, v, "pii-session", nil)

	result, _ := io.ReadAll(req.Body)
	resultStr := string(result)
	if strings.Contains(resultStr, "john@example.com") {
		t.Error("email should be anonymized")
	}
	if strings.Contains(resultStr, "0912345678") {
		t.Error("phone should be anonymized")
	}
}

func TestAnonymizeBody_QueryParamPII(t *testing.T) {
	det := detector.New()
	v := newTestVault(t)

	req := httptest.NewRequest("POST", "/v1/chat?email=test@example.com", strings.NewReader("{}"))

	anonymizeBody(req, det, v, "query-session", nil)

	if strings.Contains(req.URL.RawQuery, "test@example.com") {
		t.Error("query param PII should be anonymized")
	}
}

func TestAnonymizeBody_OversizedBody(t *testing.T) {
	det := detector.New()
	v := newTestVault(t)

	// Create body larger than MaxBodySize
	bigBody := strings.Repeat("x", int(MaxBodySize)+100)
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(bigBody))

	anonymizeBody(req, det, v, "big-session", nil)

	result, _ := io.ReadAll(req.Body)
	if !strings.Contains(string(result), "body_too_large") {
		t.Error("oversized body should be replaced with error message")
	}
}

// --- AnonymizeRequest tests ---

func TestAnonymizeRequest_POST(t *testing.T) {
	det := detector.New()
	v := newTestVault(t)
	modifier := AnonymizeRequest(det, v)

	body := `{"content":"Contact me at user@domain.com"}`
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(body))
	req.Header.Set("X-Session-ID", "anon-test")

	modifier(req)

	result, _ := io.ReadAll(req.Body)
	if strings.Contains(string(result), "user@domain.com") {
		t.Error("email should be anonymized by AnonymizeRequest")
	}
}

func TestAnonymizeRequest_GETSkipped(t *testing.T) {
	det := detector.New()
	v := newTestVault(t)
	modifier := AnonymizeRequest(det, v)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	// Should not panic on GET
	modifier(req)
}

func TestAnonymizeRequest_NilBodySkipped(t *testing.T) {
	det := detector.New()
	v := newTestVault(t)
	modifier := AnonymizeRequest(det, v)

	req := httptest.NewRequest("POST", "/v1/chat", nil)
	req.Body = nil
	// Should not panic
	modifier(req)
}
