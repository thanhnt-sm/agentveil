package router

import (
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ProviderHealth tracks health state and circuit breaker for a provider
// N4: Active health probing
// N5: Circuit breaker with error rate tracking
type ProviderHealth struct {
	mu            sync.Mutex
	errorCount    atomic.Int64
	requestCount  atomic.Int64
	lastError     time.Time
	circuitOpen   atomic.Bool
	circuitOpenAt time.Time

	// Config
	ErrorThreshold int           // errors before opening circuit (default 5)
	RecoveryTime   time.Duration // how long to keep circuit open (default 30s)
	WindowSize     time.Duration // rolling window for error counting (default 60s)
}

// NewProviderHealth creates a health tracker with defaults
func NewProviderHealth() *ProviderHealth {
	return &ProviderHealth{
		ErrorThreshold: 5,
		RecoveryTime:   30 * time.Second,
		WindowSize:     60 * time.Second,
	}
}

// RecordSuccess records a successful request
func (ph *ProviderHealth) RecordSuccess() {
	ph.requestCount.Add(1)
	// If circuit is open and recovery time passed, close it (half-open → closed)
	if ph.circuitOpen.Load() {
		ph.mu.Lock()
		if time.Since(ph.circuitOpenAt) > ph.RecoveryTime {
			ph.circuitOpen.Store(false)
			ph.errorCount.Store(0)
			slog.Info("circuit breaker closed (recovered)")
		}
		ph.mu.Unlock()
	}
}

// RecordError records a failed request and may open the circuit
func (ph *ProviderHealth) RecordError() {
	ph.requestCount.Add(1)
	count := ph.errorCount.Add(1)
	ph.mu.Lock()
	ph.lastError = time.Now()
	if int(count) >= ph.ErrorThreshold && !ph.circuitOpen.Load() {
		ph.circuitOpen.Store(true)
		ph.circuitOpenAt = time.Now()
		slog.Warn("circuit breaker OPEN", "errors", count, "threshold", ph.ErrorThreshold)
	}
	ph.mu.Unlock()
}

// IsAvailable returns true if the provider should receive traffic
func (ph *ProviderHealth) IsAvailable() bool {
	if !ph.circuitOpen.Load() {
		return true
	}
	// Allow one probe request after recovery time (half-open state)
	ph.mu.Lock()
	defer ph.mu.Unlock()
	if time.Since(ph.circuitOpenAt) > ph.RecoveryTime {
		return true // half-open: allow probe
	}
	return false
}

// ResetAfterWindow resets error count if outside the window
func (ph *ProviderHealth) ResetAfterWindow() {
	ph.mu.Lock()
	defer ph.mu.Unlock()
	if time.Since(ph.lastError) > ph.WindowSize {
		ph.errorCount.Store(0)
	}
}

// StartHealthProbe starts periodic health checking for a provider
// N4: Active health probes against provider base URL
func StartHealthProbe(name string, baseURL string, provider *Provider, interval time.Duration) func() {
	done := make(chan struct{})
	client := &http.Client{Timeout: 5 * time.Second}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				resp, err := client.Get(baseURL + "/health")
				if err != nil {
					provider.healthy.Store(false)
					slog.Debug("health probe failed", "provider", name, "error", err)
				} else {
					resp.Body.Close()
					healthy := resp.StatusCode < 500
					provider.healthy.Store(healthy)
					if !healthy {
						slog.Warn("provider unhealthy", "provider", name, "status", resp.StatusCode)
					}
				}
			case <-done:
				return
			}
		}
	}()

	return func() { close(done) }
}

// N7: Exponential backoff with jitter for fallback retry
func backoffDelay(attempt int, baseDelay time.Duration) time.Duration {
	if baseDelay == 0 {
		baseDelay = 500 * time.Millisecond
	}
	// Exponential: base * 2^attempt
	delay := baseDelay * time.Duration(math.Pow(2, float64(attempt)))
	// Cap at 10 seconds
	if delay > 10*time.Second {
		delay = 10 * time.Second
	}
	// Add jitter: ±25%
	jitter := time.Duration(rand.Int63n(int64(delay) / 4))
	if rand.Intn(2) == 0 {
		delay += jitter
	} else {
		delay -= jitter
	}
	return delay
}
