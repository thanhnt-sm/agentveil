package router

import (
	"net/http"
	"testing"
	"time"
)

func TestProviderHealth_RecordSuccess(t *testing.T) {
	ph := NewProviderHealth()
	ph.RecordSuccess()

	if ph.requestCount.Load() != 1 {
		t.Errorf("expected request count 1, got %d", ph.requestCount.Load())
	}
	if !ph.IsAvailable() {
		t.Error("provider should be available after success")
	}
}

func TestProviderHealth_CircuitBreaker(t *testing.T) {
	ph := NewProviderHealth()
	ph.ErrorThreshold = 3
	ph.RecoveryTime = 100 * time.Millisecond

	// Record errors below threshold
	ph.RecordError()
	ph.RecordError()
	if !ph.IsAvailable() {
		t.Error("should be available below threshold")
	}
	if ph.circuitOpen.Load() {
		t.Error("circuit should not be open below threshold")
	}

	// Trigger circuit open
	ph.RecordError()
	if !ph.circuitOpen.Load() {
		t.Error("circuit should be open after reaching threshold")
	}
	if ph.IsAvailable() {
		t.Error("should not be available when circuit is open")
	}

	// Wait for recovery
	time.Sleep(150 * time.Millisecond)
	if !ph.IsAvailable() {
		t.Error("should be available after recovery time (half-open)")
	}

	// Success closes circuit
	ph.RecordSuccess()
	if ph.circuitOpen.Load() {
		t.Error("circuit should be closed after recovery success")
	}
}

func TestProviderHealth_ResetAfterWindow(t *testing.T) {
	ph := NewProviderHealth()
	ph.WindowSize = 100 * time.Millisecond

	ph.RecordError()
	ph.RecordError()
	if ph.errorCount.Load() != 2 {
		t.Errorf("expected error count 2, got %d", ph.errorCount.Load())
	}

	time.Sleep(150 * time.Millisecond)
	ph.ResetAfterWindow()

	if ph.errorCount.Load() != 0 {
		t.Errorf("expected error count reset to 0, got %d", ph.errorCount.Load())
	}
}

func TestBackoffDelay(t *testing.T) {
	base := 100 * time.Millisecond

	d0 := backoffDelay(0, base) // ~100ms
	d1 := backoffDelay(1, base) // ~200ms
	d2 := backoffDelay(2, base) // ~400ms
	d3 := backoffDelay(3, base) // ~800ms

	// Check exponential growth (with jitter tolerance)
	if d0 < 50*time.Millisecond || d0 > 200*time.Millisecond {
		t.Errorf("d0 out of range: %v", d0)
	}
	if d1 < 100*time.Millisecond || d1 > 400*time.Millisecond {
		t.Errorf("d1 out of range: %v", d1)
	}
	if d2 < 200*time.Millisecond || d2 > 800*time.Millisecond {
		t.Errorf("d2 out of range: %v", d2)
	}
	if d3 < 400*time.Millisecond || d3 > 1600*time.Millisecond {
		t.Errorf("d3 out of range: %v", d3)
	}
}

func TestBackoffDelayCap(t *testing.T) {
	base := 1 * time.Second
	d := backoffDelay(20, base) // would be 2^20 seconds without cap
	if d > 15*time.Second {
		t.Errorf("backoff should be capped at ~10s, got %v", d)
	}
}

func TestIsWebSocketUpgrade(t *testing.T) {
	tests := []struct {
		name       string
		connection string
		upgrade    string
		want       bool
	}{
		{"valid", "upgrade", "websocket", true},
		{"valid-mixed-case", "Upgrade", "WebSocket", true},
		{"no-upgrade-header", "", "websocket", false},
		{"no-ws-header", "upgrade", "", false},
		{"keep-alive", "keep-alive", "websocket", false},
		{"empty", "", "", false},
		{"connection-contains-upgrade", "keep-alive, upgrade", "websocket", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/v1/responses", nil)
			if tt.connection != "" {
				req.Header.Set("Connection", tt.connection)
			}
			if tt.upgrade != "" {
				req.Header.Set("Upgrade", tt.upgrade)
			}

			got := IsWebSocketUpgrade(req)
			if got != tt.want {
				t.Errorf("IsWebSocketUpgrade() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildUpgradeRequest(t *testing.T) {
	original, _ := http.NewRequest("GET", "/v1/responses", nil)
	original.Header.Set("Authorization", "Bearer test-token")
	original.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	original.Header.Set("Sec-WebSocket-Version", "13")
	original.Header.Set("Connection", "upgrade")
	original.Header.Set("Upgrade", "websocket")

	target, _ := http.NewRequest("GET", "https://api.openai.com", nil)

	upgraded := buildUpgradeRequest(original, target.URL)

	if upgraded.Header.Get("Authorization") != "Bearer test-token" {
		t.Error("Authorization header not copied")
	}
	if upgraded.Header.Get("Sec-WebSocket-Key") != "dGhlIHNhbXBsZSBub25jZQ==" {
		t.Error("Sec-WebSocket-Key not copied")
	}
	if upgraded.Header.Get("Sec-WebSocket-Version") != "13" {
		t.Error("Sec-WebSocket-Version not copied")
	}
	if upgraded.Host != "api.openai.com" {
		t.Errorf("expected host api.openai.com, got %s", upgraded.Host)
	}
	if upgraded.URL.Path != "/v1/responses" {
		t.Errorf("expected path /v1/responses, got %s", upgraded.URL.Path)
	}
}
