package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- cryptoRandInt64n ---

func TestCryptoRandInt64n_Positive(t *testing.T) {
	for i := 0; i < 100; i++ {
		v := cryptoRandInt64n(10)
		if v < 0 || v >= 10 {
			t.Errorf("cryptoRandInt64n(10) = %d, out of range [0,10)", v)
		}
	}
}

func TestCryptoRandInt64n_ZeroInput(t *testing.T) {
	v := cryptoRandInt64n(0)
	if v != 0 {
		t.Errorf("cryptoRandInt64n(0) = %d, expected 0", v)
	}
}

func TestCryptoRandInt64n_NegativeInput(t *testing.T) {
	v := cryptoRandInt64n(-5)
	if v != 0 {
		t.Errorf("cryptoRandInt64n(-5) = %d, expected 0", v)
	}
}

func TestCryptoRandInt64n_One(t *testing.T) {
	v := cryptoRandInt64n(1)
	if v != 0 {
		t.Errorf("cryptoRandInt64n(1) = %d, expected 0", v)
	}
}

func TestCryptoRandInt64n_Large(t *testing.T) {
	v := cryptoRandInt64n(1<<62)
	if v < 0 {
		t.Errorf("cryptoRandInt64n(1<<62) = %d, should be non-negative", v)
	}
}

// --- StartHealthProbe ---

func TestStartHealthProbe_Healthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	p := &Provider{}
	p.healthy.Store(false)

	stop := StartHealthProbe("test", server.URL, p, 50*time.Millisecond)
	defer stop()

	// Wait for at least one probe
	time.Sleep(200 * time.Millisecond)

	if !p.healthy.Load() {
		t.Error("provider should be healthy after successful probe")
	}
}

func TestStartHealthProbe_Unhealthy(t *testing.T) {
	// Point to a non-existent server
	p := &Provider{}
	p.healthy.Store(true)

	stop := StartHealthProbe("dead", "http://localhost:1", p, 50*time.Millisecond)
	defer stop()

	time.Sleep(200 * time.Millisecond)

	if p.healthy.Load() {
		t.Error("provider should be unhealthy when probe fails")
	}
}

func TestStartHealthProbe_Server500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	p := &Provider{}
	p.healthy.Store(true)

	stop := StartHealthProbe("sick", server.URL, p, 50*time.Millisecond)
	defer stop()

	time.Sleep(200 * time.Millisecond)

	if p.healthy.Load() {
		t.Error("provider should be unhealthy on 500 response")
	}
}

func TestStartHealthProbe_Stop(t *testing.T) {
	var probeCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probeCount.Add(1)
		w.WriteHeader(200)
	}))
	defer server.Close()

	p := &Provider{}
	stop := StartHealthProbe("stopper", server.URL, p, 50*time.Millisecond)

	time.Sleep(200 * time.Millisecond)
	stop() // Stop the probe

	countAtStop := probeCount.Load()
	time.Sleep(200 * time.Millisecond)

	// Count shouldn't increase much after stop
	if probeCount.Load() > countAtStop+1 {
		t.Error("probes should stop after calling stop()")
	}
}

// --- codexRewriter.ServeHTTP ---

func TestCodexRewriter_ServeHTTP(t *testing.T) {
	var receivedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	cr, err := newCodexRewriter(backend.URL + "/backend-api/codex")
	if err != nil {
		t.Fatalf("newCodexRewriter error: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"prompt":"test"}`))
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJSUzI1NiJ9.test.sig")
	w := httptest.NewRecorder()

	cr.ServeHTTP(w, req)

	if !strings.Contains(receivedPath, "responses") {
		t.Errorf("expected path to contain 'responses', got %s", receivedPath)
	}
}

func TestCodexRewriter_ServeHTTP_SubPath(t *testing.T) {
	var receivedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(200)
	}))
	defer backend.Close()

	cr, _ := newCodexRewriter(backend.URL + "/backend-api/codex")

	req := httptest.NewRequest("POST", "/v1/responses/resp_123/cancel", nil)
	w := httptest.NewRecorder()

	cr.ServeHTTP(w, req)

	if !strings.Contains(receivedPath, "/resp_123/cancel") {
		t.Errorf("subpath should be preserved, got %s", receivedPath)
	}
}
