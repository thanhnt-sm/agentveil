package promptguard

import (
	"context"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

func setupMiniRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return client, mr
}

func TestNewRedisCanaryStore(t *testing.T) {
	client, _ := setupMiniRedis(t)
	store := NewRedisCanaryStore(client)
	if store == nil {
		t.Fatal("NewRedisCanaryStore returned nil")
	}
	if store.client == nil {
		t.Error("client should be set")
	}
	if store.mem == nil {
		t.Error("mem should be initialized")
	}
}

func TestRedisCanary_Generate(t *testing.T) {
	client, _ := setupMiniRedis(t)
	store := NewRedisCanaryStore(client)

	canary := store.Generate("session-1")
	if canary.Token == "" {
		t.Error("token should not be empty")
	}
	if canary.SessionID != "session-1" {
		t.Errorf("expected session-1, got %s", canary.SessionID)
	}

	// Verify persisted in Redis
	keys := client.Keys(ctx, canaryRedisPrefix+"*").Val()
	if len(keys) != 1 {
		t.Errorf("expected 1 key in Redis, got %d", len(keys))
	}
}

func TestRedisCanary_GenerateMultiple(t *testing.T) {
	client, _ := setupMiniRedis(t)
	store := NewRedisCanaryStore(client)

	c1 := store.Generate("sess-a")
	c2 := store.Generate("sess-b")
	if c1.Token == c2.Token {
		t.Error("tokens should be unique")
	}

	keys := client.Keys(ctx, canaryRedisPrefix+"*").Val()
	if len(keys) != 2 {
		t.Errorf("expected 2 keys in Redis, got %d", len(keys))
	}
}

func TestRedisCanary_InjectCanary(t *testing.T) {
	client, _ := setupMiniRedis(t)
	store := NewRedisCanaryStore(client)

	text := "This is a prompt"
	injected, canary := store.InjectCanary(text, "session-2")

	if canary.Token == "" {
		t.Error("canary token should not be empty")
	}
	if !strings.Contains(injected, canary.Token) {
		t.Error("injected text should contain the canary token")
	}
	if !strings.Contains(injected, text) {
		t.Error("injected text should contain the original text")
	}

	// Verify persisted in Redis
	keys := client.Keys(ctx, canaryRedisPrefix+"*").Val()
	if len(keys) != 1 {
		t.Errorf("expected 1 key in Redis, got %d", len(keys))
	}
}

func TestRedisCanary_CheckLeaked_InMemory(t *testing.T) {
	client, _ := setupMiniRedis(t)
	store := NewRedisCanaryStore(client)

	// Generate a canary (stored in both memory and Redis)
	canary := store.Generate("session-3")

	// Check with text containing the token (fast path: in-memory)
	leaked := store.CheckLeaked("response contains " + canary.Token + " leaked data")
	if len(leaked) != 1 {
		t.Fatalf("expected 1 leaked canary, got %d", len(leaked))
	}
	if leaked[0].Token != canary.Token {
		t.Errorf("leaked token mismatch: got %s", leaked[0].Token)
	}
}

func TestRedisCanary_CheckLeaked_NoLeak(t *testing.T) {
	client, _ := setupMiniRedis(t)
	store := NewRedisCanaryStore(client)

	store.Generate("session-4")

	// Check with text NOT containing any token
	leaked := store.CheckLeaked("clean response without any canary")
	if len(leaked) != 0 {
		t.Errorf("expected 0 leaked canaries, got %d", len(leaked))
	}
}

func TestRedisCanary_CheckLeaked_RedisSlowPath(t *testing.T) {
	client, _ := setupMiniRedis(t)
	store := NewRedisCanaryStore(client)

	// Generate canary (stored in memory + Redis)
	canary := store.Generate("session-5")

	// Create a NEW store pointing to same Redis (no in-memory tokens)
	store2 := NewRedisCanaryStore(client)

	// store2 has no in-memory tokens, so CheckLeaked hits Redis slow path
	leaked := store2.CheckLeaked("output " + canary.Token + " found")
	if len(leaked) != 1 {
		t.Fatalf("expected 1 leaked from Redis slow path, got %d", len(leaked))
	}
	if leaked[0].SessionID != "session-5" {
		t.Errorf("expected session-5, got %s", leaked[0].SessionID)
	}
}

func TestRedisCanary_Remove(t *testing.T) {
	client, _ := setupMiniRedis(t)
	store := NewRedisCanaryStore(client)

	canary := store.Generate("session-6")

	// Verify present
	keys := client.Keys(ctx, canaryRedisPrefix+"*").Val()
	if len(keys) != 1 {
		t.Fatalf("expected 1 key before remove, got %d", len(keys))
	}

	// Remove
	store.Remove(canary.Token)

	// Verify removed from Redis
	keys = client.Keys(ctx, canaryRedisPrefix+"*").Val()
	if len(keys) != 0 {
		t.Errorf("expected 0 keys after remove, got %d", len(keys))
	}

	// Verify removed from memory
	leaked := store.CheckLeaked("test " + canary.Token + " here")
	if len(leaked) != 0 {
		t.Error("should not detect removed canary in memory")
	}
}

func TestRedisCanary_LoadFromRedis(t *testing.T) {
	client, _ := setupMiniRedis(t)
	store1 := NewRedisCanaryStore(client)

	// Generate 3 canaries with store1
	c1 := store1.Generate("sess-a")
	c2 := store1.Generate("sess-b")
	c3 := store1.Generate("sess-c")

	// Create a fresh store — no in-memory tokens
	store2 := NewRedisCanaryStore(client)

	// Load from Redis
	loaded, err := store2.LoadFromRedis()
	if err != nil {
		t.Fatalf("LoadFromRedis error: %v", err)
	}
	if loaded != 3 {
		t.Errorf("expected 3 loaded, got %d", loaded)
	}

	// Verify in-memory lookup works for all 3 (fast path)
	for _, c := range []CanaryToken{c1, c2, c3} {
		leaked := store2.CheckLeaked("contains " + c.Token)
		if len(leaked) != 1 {
			t.Errorf("expected 1 leaked for %s, got %d", c.Token, len(leaked))
		}
	}
}

func TestRedisCanary_LoadFromRedis_Empty(t *testing.T) {
	client, _ := setupMiniRedis(t)
	store := NewRedisCanaryStore(client)

	loaded, err := store.LoadFromRedis()
	if err != nil {
		t.Fatalf("LoadFromRedis error: %v", err)
	}
	if loaded != 0 {
		t.Errorf("expected 0 loaded from empty Redis, got %d", loaded)
	}
}
