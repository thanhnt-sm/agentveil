package proxy

import (
	"testing"
)

func TestCacheKey_ValidRequest(t *testing.T) {
	body := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hello"}],"temperature":0.7}`)
	key := CacheKey(body)
	if key == "" {
		t.Fatal("expected non-empty cache key")
	}
	if len(key) < 10 {
		t.Errorf("cache key too short: %q", key)
	}

	// Same input → same key
	key2 := CacheKey(body)
	if key != key2 {
		t.Error("identical inputs should produce identical cache keys")
	}
}

func TestCacheKey_DifferentInputs(t *testing.T) {
	body1 := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`)
	body2 := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"goodbye"}]}`)

	key1 := CacheKey(body1)
	key2 := CacheKey(body2)

	if key1 == key2 {
		t.Error("different inputs should produce different cache keys")
	}
}

func TestCacheKey_StreamingRequest(t *testing.T) {
	body := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	key := CacheKey(body)
	if key != "" {
		t.Error("streaming requests should not be cached")
	}
}

func TestCacheKey_EmptyBody(t *testing.T) {
	key := CacheKey([]byte(""))
	if key != "" {
		t.Error("empty body should not produce a cache key")
	}
}

func TestCacheKey_InvalidJSON(t *testing.T) {
	key := CacheKey([]byte("not json"))
	if key != "" {
		t.Error("invalid JSON should not produce a cache key")
	}
}

func TestCacheKey_NoModel(t *testing.T) {
	body := []byte(`{"prompt":"hello"}`)
	key := CacheKey(body)
	if key == "" {
		t.Error("prompt field should generate a cache key")
	}
}

func TestCacheableResponse(t *testing.T) {
	tests := []struct {
		status      int
		contentType string
		want        bool
	}{
		{200, "application/json", true},
		{200, "application/json; charset=utf-8", true},
		{200, "text/event-stream", false},
		{500, "application/json", false},
		{404, "application/json", false},
		{200, "text/plain", false},
	}

	for _, tt := range tests {
		got := CacheableResponse(tt.status, tt.contentType)
		if got != tt.want {
			t.Errorf("CacheableResponse(%d, %q) = %v, want %v", tt.status, tt.contentType, got, tt.want)
		}
	}
}
