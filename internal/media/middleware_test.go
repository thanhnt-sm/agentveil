package media

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/vurakit/agentveil/internal/detector"
)

func TestNewScanMiddleware(t *testing.T) {
	ext := New()
	det := detector.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	sm := NewScanMiddleware(ext, det, logger, true)
	if sm == nil {
		t.Fatal("NewScanMiddleware returned nil")
	}
	if sm.blockMode != true {
		t.Error("blockMode should be true")
	}
}

func TestScanMiddleware_SkipsGET(t *testing.T) {
	ext := New()
	det := detector.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sm := NewScanMiddleware(ext, det, logger, true)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})

	handler := sm.Middleware(inner)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("GET should pass through, got %d", w.Code)
	}
}

func TestScanMiddleware_SkipsNonJSON(t *testing.T) {
	ext := New()
	det := detector.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sm := NewScanMiddleware(ext, det, logger, true)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	handler := sm.Middleware(inner)
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader("plain text"))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("non-JSON should pass through, got %d", w.Code)
	}
}

func TestScanMiddleware_PassesCleanJSON(t *testing.T) {
	ext := New()
	det := detector.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sm := NewScanMiddleware(ext, det, logger, true)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	handler := sm.Middleware(inner)

	// Clean JSON without any media attachments
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("clean JSON should pass through, got %d", w.Code)
	}
}

func TestScanMiddleware_LogMode(t *testing.T) {
	ext := New()
	det := detector.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	// blockMode = false → log only, don't block
	sm := NewScanMiddleware(ext, det, logger, false)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	handler := sm.Middleware(inner)
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}`
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("log mode should pass through, got %d", w.Code)
	}
}

func TestExtractor_New(t *testing.T) {
	ext := New()
	if ext == nil {
		t.Fatal("New returned nil")
	}
}
