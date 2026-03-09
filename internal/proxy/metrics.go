package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// N8: Prometheus-compatible metrics
// N9: Token usage tracking
// N11: Request tracing with X-Veil-Request-ID

// Metrics tracks proxy-wide counters for observability
type Metrics struct {
	RequestCount   atomic.Int64
	ErrorCount     atomic.Int64
	PIIDetections  atomic.Int64
	CacheHits      atomic.Int64
	CacheMisses    atomic.Int64
	TotalTokensIn  atomic.Int64
	TotalTokensOut atomic.Int64
	TotalLatencyMs atomic.Int64
}

// GlobalMetrics is the singleton metrics instance
var GlobalMetrics = &Metrics{}

// MetricsHandler returns a Prometheus-compatible /metrics endpoint
// N8: Exposes request_count, error_count, pii_detections, cache_hits, etc.
func MetricsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		m := GlobalMetrics
		fmt := "# HELP %s %s\n# TYPE %s %s\n%s %d\n"
		write := func(name, help, mtype string, val int64) {
			w.Write([]byte(formatMetric(name, help, mtype, val)))
		}
		_ = fmt
		write("agentveil_requests_total", "Total HTTP requests proxied", "counter", m.RequestCount.Load())
		write("agentveil_errors_total", "Total proxy errors", "counter", m.ErrorCount.Load())
		write("agentveil_pii_detections_total", "Total PII detections", "counter", m.PIIDetections.Load())
		write("agentveil_cache_hits_total", "Total cache hits", "counter", m.CacheHits.Load())
		write("agentveil_cache_misses_total", "Total cache misses", "counter", m.CacheMisses.Load())
		write("agentveil_tokens_in_total", "Total input tokens", "counter", m.TotalTokensIn.Load())
		write("agentveil_tokens_out_total", "Total output tokens", "counter", m.TotalTokensOut.Load())
		write("agentveil_latency_ms_total", "Total latency in milliseconds", "counter", m.TotalLatencyMs.Load())
	}
}

func formatMetric(name, help, mtype string, val int64) string {
	return "# HELP " + name + " " + help + "\n" +
		"# TYPE " + name + " " + mtype + "\n" +
		name + " " + itoa(val) + "\n"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 20)
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	if neg {
		buf = append(buf, '-')
	}
	// reverse
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

// RequestTracingMiddleware adds X-Veil-Request-ID to every request/response
// N11: End-to-end request correlation
func RequestTracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Generate or reuse existing request ID
		requestID := r.Header.Get("X-Veil-Request-ID")
		if requestID == "" {
			b := make([]byte, 8)
			rand.Read(b)
			requestID = "veil-" + hex.EncodeToString(b)
		}

		// Set on request and response
		r.Header.Set("X-Veil-Request-ID", requestID)
		w.Header().Set("X-Veil-Request-ID", requestID)

		// Track metrics
		start := time.Now()
		GlobalMetrics.RequestCount.Add(1)

		// Wrap writer to capture status
		sw := &statusWriter{ResponseWriter: w, statusCode: 200}
		next.ServeHTTP(sw, r)

		latency := time.Since(start)
		GlobalMetrics.TotalLatencyMs.Add(latency.Milliseconds())

		if sw.statusCode >= 500 {
			GlobalMetrics.ErrorCount.Add(1)
		}

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.statusCode,
			"latency_ms", latency.Milliseconds(),
			"request_id", requestID)
	})
}

type statusWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (sw *statusWriter) WriteHeader(code int) {
	if !sw.written {
		sw.statusCode = code
		sw.written = true
	}
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.written {
		sw.written = true
	}
	return sw.ResponseWriter.Write(b)
}
