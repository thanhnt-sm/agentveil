package webhook

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSendWebhook_HTTPServer(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(200)
	}))
	defer server.Close()

	d := NewDispatcher(Config{
		Destinations: []Destination{
			{Name: "test", URL: server.URL, Enabled: true},
		},
		RetryCount: 1,
		TimeoutSec: 5, BufferSize: 100,
	})
	defer d.Close()

	event := Event{
		Type:      EventPIIDetected,
		SessionID: "session-1",
		Data:      map[string]any{"count": 3, "categories": []string{"EMAIL", "PHONE"}},
	}

	d.Emit(event)
	time.Sleep(300 * time.Millisecond)

	if receivedBody == "" {
		t.Error("webhook should have received the event")
	}
	if !strings.Contains(receivedBody, "pii.detected") {
		t.Error("event body should contain event type")
	}
}

func TestSendWebhook_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	d := NewDispatcher(Config{
		Destinations: []Destination{
			{Name: "err-test", URL: server.URL, Enabled: true},
		},
		RetryCount: 0,
		TimeoutSec: 2, BufferSize: 100,
	})
	defer d.Close()

	d.Emit(Event{
		Type:      EventAuditComplete,
		SessionID: "session-err",
		Data:      map[string]any{"message": "test error"},
	})
	time.Sleep(200 * time.Millisecond)
	// Should not panic
}

func TestSendDiscord_HTTPServer(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(204)
	}))
	defer server.Close()

	d := NewDispatcher(Config{
		Discord: &DiscordConfig{
			WebhookURL: server.URL,
		},
		TimeoutSec: 5, BufferSize: 100,
	})
	defer d.Close()

	d.Emit(Event{
		Type:      EventPIIDetected,
		SessionID: "discord-test",
		Data:      map[string]any{"count": 2},
	})
	time.Sleep(300 * time.Millisecond)

	if receivedBody == "" {
		t.Error("discord webhook should have received the event")
	}
	if !strings.Contains(receivedBody, "embeds") {
		t.Error("discord body should contain embeds")
	}
}

func TestSendSlack_HTTPServer(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(200)
	}))
	defer server.Close()

	d := NewDispatcher(Config{
		Slack: &SlackConfig{
			WebhookURL: server.URL,
		},
		TimeoutSec: 5, BufferSize: 100,
	})
	defer d.Close()

	d.Emit(Event{
		Type:      EventAuditHighRisk,
		SessionID: "slack-test",
		Data:      map[string]any{"message": "alert"},
	})
	time.Sleep(300 * time.Millisecond)

	if receivedBody == "" {
		t.Error("slack webhook should have received the event")
	}
}

func TestEmit_MultipleDestinations(t *testing.T) {
	var count int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(200)
	}))
	defer server.Close()

	d := NewDispatcher(Config{
		Destinations: []Destination{
			{Name: "d1", URL: server.URL, Enabled: true},
			{Name: "d2", URL: server.URL, Enabled: true},
		},
		TimeoutSec: 5, BufferSize: 100,
	})
	defer d.Close()

	d.Emit(Event{Type: EventPIIDetected, SessionID: "multi"})
	time.Sleep(500 * time.Millisecond)

	if count < 2 {
		t.Errorf("expected ≥2 webhook calls, got %d", count)
	}
}

func TestEmit_DisabledDestination(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	defer server.Close()

	d := NewDispatcher(Config{
		Destinations: []Destination{
			{Name: "disabled", URL: server.URL, Enabled: false},
		},
		TimeoutSec: 5, BufferSize: 100,
	})
	defer d.Close()

	d.Emit(Event{Type: EventPIIDetected, SessionID: "disabled-test"})
	time.Sleep(200 * time.Millisecond)

	if called {
		t.Error("disabled destination should not be called")
	}
}
