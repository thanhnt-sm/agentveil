package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/vurakit/agentveil/internal/vault"
)

// --- SemanticCache tests ---

func TestNewSemanticCache_DefaultTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	sc := NewSemanticCache(client, 0)
	if sc == nil {
		t.Fatal("NewSemanticCache should not be nil")
	}
	if sc.ttl != defaultCacheTTL {
		t.Errorf("expected default TTL, got %v", sc.ttl)
	}
}

func TestNewSemanticCache_CustomTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	sc := NewSemanticCache(client, 5*time.Minute)
	if sc.ttl != 5*time.Minute {
		t.Errorf("expected 5m TTL, got %v", sc.ttl)
	}
}

func TestServeCached_BasicResponse(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	sc := NewSemanticCache(client, time.Minute)

	cached := &CachedResponse{
		Body:       `{"id":"cached-1","choices":[{"message":{"content":"cached response"}}]}`,
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		CachedAt: time.Now().Add(-30 * time.Second),
		HitCount: 5,
		Provider: "openai",
	}

	w := httptest.NewRecorder()
	sc.ServeCached(w, cached)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("X-Veil-Cache") != "HIT" {
		t.Error("expected X-Veil-Cache: HIT header")
	}
	if w.Header().Get("X-Veil-Cache-Age") == "" {
		t.Error("expected X-Veil-Cache-Age header")
	}
	if !strings.Contains(w.Body.String(), "cached response") {
		t.Error("body should contain cached response")
	}
}

func TestServeCached_NoStatusCode(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	sc := NewSemanticCache(client, time.Minute)

	cached := &CachedResponse{
		Body:     "cached body",
		CachedAt: time.Now(),
	}

	w := httptest.NewRecorder()
	sc.ServeCached(w, cached)

	// Should default to 200
	if w.Code != 200 {
		t.Errorf("expected default 200, got %d", w.Code)
	}
}

// --- RehydrateResponse tests ---

func TestRehydrateResponse_NoMappings(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	v := vault.NewWithClient(client)

	modifier := RehydrateResponse(v, "admin")

	body := `{"choices":[{"message":{"content":"clean response"}}]}`
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    httptest.NewRequest("POST", "/v1/chat", nil),
	}

	err := modifier(resp)
	if err != nil {
		t.Fatalf("RehydrateResponse error: %v", err)
	}

	result, _ := io.ReadAll(resp.Body)
	if string(result) != body {
		t.Error("body without PII tokens should be unchanged")
	}
}

func TestRehydrateResponse_SSEStream(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	v := vault.NewWithClient(client)

	modifier := RehydrateResponse(v, "admin")

	sseBody := "data: {\"content\":\"hello\"}\n\ndata: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sseBody)),
		Request:    httptest.NewRequest("POST", "/v1/chat", nil),
	}

	err := modifier(resp)
	if err != nil {
		t.Fatalf("RehydrateResponse SSE error: %v", err)
	}

	// Body should be replaced with SSE rehydrator
	if resp.Body == nil {
		t.Error("body should not be nil")
	}
}

func TestRehydrateResponse_NilRequest(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	v := vault.NewWithClient(client)

	modifier := RehydrateResponse(v, "default-role")

	body := `{"content":"test"}`
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    nil,
	}

	err := modifier(resp)
	if err != nil {
		t.Fatalf("RehydrateResponse nil request error: %v", err)
	}
}
