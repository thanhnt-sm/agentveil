package promptguard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	canaryRedisPrefix = "canary:"
	canaryTTL         = 24 * time.Hour
	canaryCtxTimeout  = 3 * time.Second
)

// RedisCanaryStore is a Redis-backed CanaryStore that persists across restarts.
// P3 #19: Canary tokens are now stored in Redis with a 24-hour TTL.
type RedisCanaryStore struct {
	client *redis.Client
	mem    *CanaryStore // in-memory fallback for fast CheckLeaked
}

// NewRedisCanaryStore creates a Redis-backed canary store.
func NewRedisCanaryStore(client *redis.Client) *RedisCanaryStore {
	return &RedisCanaryStore{
		client: client,
		mem:    NewCanaryStore(),
	}
}

// Generate creates a canary token and persists it to Redis.
func (rcs *RedisCanaryStore) Generate(sessionID string) CanaryToken {
	canary := rcs.mem.Generate(sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), canaryCtxTimeout)
	defer cancel()
	data, _ := json.Marshal(canary)
	rcs.client.Set(ctx, canaryRedisPrefix+canary.Token, data, canaryTTL)

	return canary
}

// InjectCanary adds a canary token to text and persists it.
func (rcs *RedisCanaryStore) InjectCanary(text, sessionID string) (string, CanaryToken) {
	injected, canary := rcs.mem.InjectCanary(text, sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), canaryCtxTimeout)
	defer cancel()
	data, _ := json.Marshal(canary)
	rcs.client.Set(ctx, canaryRedisPrefix+canary.Token, data, canaryTTL)

	return injected, canary
}

// CheckLeaked checks both in-memory and Redis for leaked canary tokens.
func (rcs *RedisCanaryStore) CheckLeaked(text string) []CanaryToken {
	// Fast path: in-memory check
	leaked := rcs.mem.CheckLeaked(text)
	if len(leaked) > 0 {
		return leaked
	}

	// Slow path: scan Redis for any canary tokens in the text
	ctx, cancel := context.WithTimeout(context.Background(), canaryCtxTimeout)
	defer cancel()
	var cursor uint64
	for {
		keys, nextCursor, err := rcs.client.Scan(ctx, cursor, canaryRedisPrefix+"*", 100).Result()
		if err != nil {
			break
		}
		for _, key := range keys {
			data, err := rcs.client.Get(ctx, key).Result()
			if err != nil {
				continue
			}
			var canary CanaryToken
			if err := json.Unmarshal([]byte(data), &canary); err != nil {
				continue
			}
			if len(canary.Token) > 0 && contains(text, canary.Token) {
				leaked = append(leaked, canary)
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return leaked
}

// Remove deletes a canary token from both memory and Redis.
func (rcs *RedisCanaryStore) Remove(token string) {
	rcs.mem.Remove(token)
	ctx, cancel := context.WithTimeout(context.Background(), canaryCtxTimeout)
	defer cancel()
	rcs.client.Del(ctx, canaryRedisPrefix+token)
}

// LoadFromRedis loads all existing canary tokens from Redis into memory.
// Call this on startup to restore state after a restart.
func (rcs *RedisCanaryStore) LoadFromRedis() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var cursor uint64
	loaded := 0

	for {
		keys, nextCursor, err := rcs.client.Scan(ctx, cursor, canaryRedisPrefix+"*", 100).Result()
		if err != nil {
			return loaded, fmt.Errorf("scan canary keys: %w", err)
		}
		for _, key := range keys {
			data, err := rcs.client.Get(ctx, key).Result()
			if err != nil {
				continue
			}
			var canary CanaryToken
			if err := json.Unmarshal([]byte(data), &canary); err != nil {
				continue
			}
			rcs.mem.mu.Lock()
			rcs.mem.tokens[canary.Token] = canary
			rcs.mem.mu.Unlock()
			loaded++
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return loaded, nil
}

func contains(text, token string) bool {
	return len(token) > 0 && len(text) >= len(token) && (text == token || findSubstring(text, token))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
