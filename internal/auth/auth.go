package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultKeyTTL = 90 * 24 * time.Hour // P2 #12: 90-day TTL on API keys

// Role determines the user's access level
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
)

// APIKey represents a registered API key with its metadata
type APIKey struct {
	ID        string    `json:"id"`
	KeyHash   string    `json:"key_hash"` // SHA-256 hash, never store plaintext
	Role      Role      `json:"role"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
	Active    bool      `json:"active"`
}

// Manager handles API key operations
type Manager struct {
	client *redis.Client
	prefix string
}

// NewManager creates an auth Manager
func NewManager(client *redis.Client) *Manager {
	return &Manager{client: client, prefix: "auth:apikey:"}
}

// GenerateKey creates a new API key and stores its hash in Redis.
// Returns the plaintext key (show once to user) and the APIKey metadata.
func (m *Manager) GenerateKey(ctx context.Context, role Role, label string) (string, *APIKey, error) {
	// Generate random key: veil_sk_<32 hex chars>
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate random: %w", err)
	}
	plaintext := "veil_sk_" + hex.EncodeToString(raw)

	hash := hashKey(plaintext)
	id := hash[:16] // BUG-16 FIX: 64-bit ID (collision at ~4B keys vs 16M)

	key := &APIKey{
		ID:        id,
		KeyHash:   hash,
		Role:      role,
		Label:     label,
		CreatedAt: time.Now().UTC(),
		Active:    true,
	}

	// Store in Redis hash
	redisKey := m.prefix + hash
	err := m.client.HSet(ctx, redisKey,
		"id", key.ID,
		"role", string(key.Role),
		"label", key.Label,
		"created_at", key.CreatedAt.Format(time.RFC3339),
		"active", "true",
	).Err()
	if err != nil {
		return "", nil, fmt.Errorf("store key: %w", err)
	}

	// P2 #12: Set TTL on API keys
	m.client.Expire(ctx, redisKey, defaultKeyTTL)

	// P2 #11: Secondary index for O(1) RevokeByID
	idxKey := "auth:id2key:" + key.ID
	m.client.Set(ctx, idxKey, hash, defaultKeyTTL)

	return plaintext, key, nil
}

// Validate checks a plaintext API key and returns its metadata if valid
func (m *Manager) Validate(ctx context.Context, plaintext string) (*APIKey, error) {
	hash := hashKey(plaintext)
	redisKey := m.prefix + hash

	data, err := m.client.HGetAll(ctx, redisKey).Result()
	if err != nil {
		return nil, fmt.Errorf("lookup key: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("invalid or revoked API key")
	}
	if data["active"] != "true" {
		return nil, fmt.Errorf("invalid or revoked API key")
	}

	createdAt, _ := time.Parse(time.RFC3339, data["created_at"])

	return &APIKey{
		ID:        data["id"],
		KeyHash:   hash,
		Role:      Role(data["role"]),
		Label:     data["label"],
		CreatedAt: createdAt,
		Active:    true,
	}, nil
}

// Revoke deactivates an API key by its hash
func (m *Manager) Revoke(ctx context.Context, plaintext string) error {
	hash := hashKey(plaintext)
	redisKey := m.prefix + hash
	return m.client.HSet(ctx, redisKey, "active", "false").Err()
}

// RevokeByID deactivates an API key by its ID using the secondary index
func (m *Manager) RevokeByID(ctx context.Context, id string) error {
	// P2 #11: Use secondary index for O(1) lookup
	idxKey := "auth:id2key:" + id
	hash, err := m.client.Get(ctx, idxKey).Result()
	if err == nil && hash != "" {
		redisKey := m.prefix + hash
		return m.client.HSet(ctx, redisKey, "active", "false").Err()
	}

	// Fallback: scan (backwards compat for keys created before index)
	var cursor uint64
	for {
		keys, nextCursor, scanErr := m.client.Scan(ctx, cursor, m.prefix+"*", 100).Result()
		if scanErr != nil {
			return scanErr
		}
		for _, key := range keys {
			storedID, _ := m.client.HGet(ctx, key, "id").Result()
			if storedID == id {
				return m.client.HSet(ctx, key, "active", "false").Err()
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return fmt.Errorf("key ID %s not found", id)
}

func hashKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}
