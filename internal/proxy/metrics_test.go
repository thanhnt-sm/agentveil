package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsHandler(t *testing.T) {
	// Reset metrics
	GlobalMetrics = &Metrics{}

	// Simulate some activity
	GlobalMetrics.RequestCount.Add(42)
	GlobalMetrics.ErrorCount.Add(3)
	GlobalMetrics.PIIDetections.Add(7)
	GlobalMetrics.CacheHits.Add(10)
	GlobalMetrics.CacheMisses.Add(5)
	GlobalMetrics.TotalTokensIn.Add(1000)
	GlobalMetrics.TotalTokensOut.Add(500)
	GlobalMetrics.TotalLatencyMs.Add(12345)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	MetricsHandler()(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()

	// Verify Content-Type header
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain; version=0.0.4" {
		t.Errorf("expected text/plain content-type, got %q", ct)
	}

	checks := []struct {
		name  string
		want  string
	}{
		{"requests", "agentveil_requests_total 42"},
		{"errors", "agentveil_errors_total 3"},
		{"pii", "agentveil_pii_detections_total 7"},
		{"cache_hits", "agentveil_cache_hits_total 10"},
		{"cache_misses", "agentveil_cache_misses_total 5"},
		{"tokens_in", "agentveil_tokens_in_total 1000"},
		{"tokens_out", "agentveil_tokens_out_total 500"},
		{"latency", "agentveil_latency_ms_total 12345"},
	}

	for _, c := range checks {
		if !strings.Contains(body, c.want) {
			t.Errorf("%s: expected body to contain %q", c.name, c.want)
		}
	}

	// Verify Prometheus format
	if !strings.Contains(body, "# HELP") {
		t.Error("missing Prometheus HELP comment")
	}
	if !strings.Contains(body, "# TYPE") {
		t.Error("missing Prometheus TYPE comment")
	}
}

func TestRequestTracingMiddleware(t *testing.T) {
	GlobalMetrics = &Metrics{}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request ID was set
		reqID := r.Header.Get("X-Veil-Request-ID")
		if reqID == "" {
			t.Error("X-Veil-Request-ID not set on request")
		}
		if !strings.HasPrefix(reqID, "veil-") {
			t.Errorf("request ID should start with 'veil-', got %q", reqID)
		}
		w.WriteHeader(200)
	})

	handler := RequestTracingMiddleware(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify response header
	respID := w.Header().Get("X-Veil-Request-ID")
	if respID == "" {
		t.Error("X-Veil-Request-ID not set on response")
	}

	// Verify metrics incremented
	if GlobalMetrics.RequestCount.Load() != 1 {
		t.Errorf("expected request count 1, got %d", GlobalMetrics.RequestCount.Load())
	}
}

func TestRequestTracingReusesExistingID(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Veil-Request-ID") != "my-custom-id" {
			t.Error("should reuse existing request ID")
		}
		w.WriteHeader(200)
	})

	handler := RequestTracingMiddleware(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Veil-Request-ID", "my-custom-id")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Header().Get("X-Veil-Request-ID") != "my-custom-id" {
		t.Error("should preserve custom request ID in response")
	}
}

func TestRequestTracingTracksErrors(t *testing.T) {
	GlobalMetrics = &Metrics{}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})

	handler := RequestTracingMiddleware(inner)

	req := httptest.NewRequest("GET", "/error", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if GlobalMetrics.ErrorCount.Load() != 1 {
		t.Errorf("expected error count 1, got %d", GlobalMetrics.ErrorCount.Load())
	}
}

func TestFormatMetric(t *testing.T) {
	result := formatMetric("test_counter", "A test counter", "counter", 42)
	expected := "# HELP test_counter A test counter\n# TYPE test_counter counter\ntest_counter 42\n"
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{-1, "-1"},
		{1234567890, "1234567890"},
	}
	for _, tt := range tests {
		got := itoa(tt.input)
		if got != tt.want {
			t.Errorf("itoa(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
