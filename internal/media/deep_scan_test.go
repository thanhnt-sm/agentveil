package media

import (
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vurakit/agentveil/internal/detector"
)

// TestMediaMiddleware_WithImageContent tests the deep PII scan path
// using inline base64 image content in OpenAI message format
func TestMediaMiddleware_WithImageContent(t *testing.T) {
	ext := New()
	det := detector.New()
	sm := NewScanMiddleware(ext, det, slog.Default(), false)

	var called bool
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))

	// Create a tiny 1x1 PNG as base64 — no actual PII, should pass through
	tinyPNG := base64.StdEncoding.EncodeToString([]byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG header
	})

	body := `{"messages":[{"role":"user","content":[
		{"type":"text","text":"check this image"},
		{"type":"image_url","image_url":{"url":"data:image/png;base64,` + tinyPNG + `"}}
	]}]}`

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("request with image should pass through (no PII)")
	}
}

// TestMediaMiddleware_EmptyBody tests handling of empty POST body
func TestMediaMiddleware_EmptyBody(t *testing.T) {
	ext := New()
	det := detector.New()
	sm := NewScanMiddleware(ext, det, slog.Default(), false)

	var called bool
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("empty body should pass through")
	}
}

// TestMediaMiddleware_InvalidJSON tests handling of malformed JSON body
func TestMediaMiddleware_InvalidJSON(t *testing.T) {
	ext := New()
	det := detector.New()
	sm := NewScanMiddleware(ext, det, slog.Default(), false)

	var called bool
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader("{not valid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("invalid JSON should pass through (no media to scan)")
	}
}

// TestMediaMiddleware_BlockMode tests that block mode returns 403 when PII is detected in results
func TestMediaMiddleware_BlockModeNoPII(t *testing.T) {
	ext := New()
	det := detector.New()
	sm := NewScanMiddleware(ext, det, slog.Default(), true)

	var called bool
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	// Normal messages without images — should pass through even in block mode
	body := `{"messages":[{"role":"user","content":"hello, no images here"}]}`
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("block mode with no PII should pass through")
	}
}

func TestNewExtractor(t *testing.T) {
	ext := New()
	if ext == nil {
		t.Fatal("NewExtractor should not return nil")
	}
}
