package media

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vurakit/agentveil/internal/detector"
)

func TestMediaMiddleware_GETPassthrough(t *testing.T) {
	ext := &Extractor{}
	det := detector.New()
	sm := NewScanMiddleware(ext, det, slog.Default(), false)

	var called bool
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("GET should pass through")
	}
}

func TestMediaMiddleware_NonJSONPassthrough(t *testing.T) {
	ext := &Extractor{}
	det := detector.New()
	sm := NewScanMiddleware(ext, det, slog.Default(), false)

	var called bool
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("POST", "/v1/uploads", strings.NewReader("binary data"))
	req.Header.Set("Content-Type", "multipart/form-data")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("non-JSON should pass through")
	}
}

func TestMediaMiddleware_CleanJSON(t *testing.T) {
	ext := &Extractor{}
	det := detector.New()
	sm := NewScanMiddleware(ext, det, slog.Default(), false)

	var called bool
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	body := `{"messages":[{"role":"user","content":"hello world"}]}`
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("clean JSON post should pass through")
	}
}

func TestMediaMiddleware_PUTMethod(t *testing.T) {
	ext := &Extractor{}
	det := detector.New()
	sm := NewScanMiddleware(ext, det, slog.Default(), false)

	var called bool
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	body := `{"messages":[{"role":"user","content":"test"}]}`
	req := httptest.NewRequest("PUT", "/v1/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("PUT with JSON should be scanned and pass through")
	}
}

func TestMediaMiddleware_DELETEPassthrough(t *testing.T) {
	ext := &Extractor{}
	det := detector.New()
	sm := NewScanMiddleware(ext, det, slog.Default(), false)

	var called bool
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("DELETE", "/v1/resource", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("DELETE should pass through")
	}
}

func TestMediaMiddleware_NoBlockMode(t *testing.T) {
	ext := &Extractor{}
	det := detector.New()
	sm := NewScanMiddleware(ext, det, slog.Default(), false)

	var called bool
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	body := `{"messages":[{"role":"user","content":"no images here"}]}`
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("non-block mode should always pass through")
	}
}

func TestNewScanMiddleware_BlockMode(t *testing.T) {
	ext := &Extractor{}
	det := detector.New()
	sm := NewScanMiddleware(ext, det, slog.Default(), true)

	if sm == nil {
		t.Fatal("NewScanMiddleware should not return nil")
	}
	if !sm.blockMode {
		t.Error("block mode should be true")
	}
}
