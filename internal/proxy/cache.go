package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	cachePrefix     = "cache:"
	defaultCacheTTL = 5 * time.Minute
)

// CacheConfig configures the semantic cache behavior
type CacheConfig struct {
	Enabled bool          `yaml:"enabled"`
	TTL     time.Duration `yaml:"ttl"`
}

// SemanticCache stores and retrieves LLM responses based on request content hash.
// P3 #21: Identical prompts with the same model return cached responses.
type SemanticCache struct {
	client *redis.Client
	ttl    time.Duration
}

// CachedResponse is the stored response data
type CachedResponse struct {
	StatusCode  int               `json:"status_code"`
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body"`
	CachedAt    time.Time         `json:"cached_at"`
	Provider    string            `json:"provider"`
	HitCount    int               `json:"hit_count"`
}

// NewSemanticCache creates a new semantic cache backed by Redis
func NewSemanticCache(client *redis.Client, ttl time.Duration) *SemanticCache {
	if ttl == 0 {
		ttl = defaultCacheTTL
	}
	return &SemanticCache{client: client, ttl: ttl}
}

// CacheKey generates a deterministic hash key from the request body + model.
// Only caches non-streaming, non-tool requests.
func CacheKey(body []byte) string {
	// Parse to extract deterministic fields (model + messages/input)
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}

	// Don't cache streaming requests
	if stream, ok := data["stream"].(bool); ok && stream {
		return ""
	}

	// Build deterministic key from: model + messages/input + temperature
	parts := make(map[string]any)
	for _, key := range []string{"model", "messages", "input", "prompt", "temperature", "max_tokens"} {
		if v, ok := data[key]; ok {
			parts[key] = v
		}
	}

	if len(parts) == 0 {
		return ""
	}

	keyData, err := json.Marshal(parts)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(keyData)
	return cachePrefix + hex.EncodeToString(hash[:16]) // 128-bit key
}

// Get retrieves a cached response. Returns nil if not cached.
func (sc *SemanticCache) Get(ctx context.Context, key string) *CachedResponse {
	if key == "" {
		return nil
	}

	data, err := sc.client.Get(ctx, key).Result()
	if err != nil {
		return nil
	}

	var cached CachedResponse
	if err := json.Unmarshal([]byte(data), &cached); err != nil {
		return nil
	}

	// Increment hit count
	cached.HitCount++
	updated, _ := json.Marshal(cached)
	sc.client.Set(ctx, key, updated, sc.ttl)

	return &cached
}

// Set stores a response in the cache
func (sc *SemanticCache) Set(ctx context.Context, key string, resp *CachedResponse) {
	if key == "" {
		return
	}
	resp.CachedAt = time.Now().UTC()
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	sc.client.Set(ctx, key, data, sc.ttl)
}

// ServeCached writes a cached response to the HTTP response writer
func (sc *SemanticCache) ServeCached(w http.ResponseWriter, cached *CachedResponse) {
	for k, v := range cached.Headers {
		w.Header().Set(k, v)
	}
	w.Header().Set("X-Veil-Cache", "HIT")
	w.Header().Set("X-Veil-Cache-Age", time.Since(cached.CachedAt).Round(time.Second).String())

	if cached.StatusCode > 0 {
		w.WriteHeader(cached.StatusCode)
	}
	w.Write([]byte(cached.Body))

	slog.Info("cache hit",
		"age", time.Since(cached.CachedAt).Round(time.Second),
		"hits", cached.HitCount,
		"provider", cached.Provider)
}

// CacheableResponse checks if a response should be cached
func CacheableResponse(statusCode int, contentType string) bool {
	if statusCode != http.StatusOK {
		return false
	}
	// Only cache JSON responses (not SSE streams)
	return strings.Contains(contentType, "application/json")
}
