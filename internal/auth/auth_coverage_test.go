package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupAuthManager(t *testing.T) (*Manager, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	m := NewManager(client)
	return m, mr
}

func TestGenerateKey_Success(t *testing.T) {
	m, _ := setupAuthManager(t)
	ctx := context.Background()

	plaintext, key, err := m.GenerateKey(ctx, RoleAdmin, "test-key")
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}
	if plaintext == "" {
		t.Error("plaintext should not be empty")
	}
	if key.ID == "" {
		t.Error("key ID should not be empty")
	}
	if key.Role != RoleAdmin {
		t.Errorf("expected role admin, got %s", key.Role)
	}
	if key.Label != "test-key" {
		t.Errorf("expected label test-key, got %s", key.Label)
	}
	if !key.Active {
		t.Error("new key should be active")
	}
}

func TestGenerateKey_MultipleUnique(t *testing.T) {
	m, _ := setupAuthManager(t)
	ctx := context.Background()

	p1, _, _ := m.GenerateKey(ctx, RoleAdmin, "key-1")
	p2, _, _ := m.GenerateKey(ctx, RoleViewer, "key-2")

	if p1 == p2 {
		t.Error("keys should be unique")
	}
}

func TestValidate_Success(t *testing.T) {
	m, _ := setupAuthManager(t)
	ctx := context.Background()

	plaintext, _, err := m.GenerateKey(ctx, RoleAdmin, "validate-test")
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}

	key, err := m.Validate(ctx, plaintext)
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if key.Role != RoleAdmin {
		t.Errorf("expected admin, got %s", key.Role)
	}
	if !key.Active {
		t.Error("key should be active")
	}
}

func TestValidate_NonexistentKey(t *testing.T) {
	m, _ := setupAuthManager(t)
	ctx := context.Background()

	_, err := m.Validate(ctx, "veil_sk_nonexistent_key_12345")
	if err == nil {
		t.Error("should error for invalid key")
	}
}

func TestValidate_RevokedKey(t *testing.T) {
	m, _ := setupAuthManager(t)
	ctx := context.Background()

	plaintext, key, _ := m.GenerateKey(ctx, RoleAdmin, "revoke-test")

	// Revoke
	err := m.RevokeByID(ctx, key.ID)
	if err != nil {
		t.Fatalf("RevokeByID error: %v", err)
	}

	// Validate should fail
	_, err = m.Validate(ctx, plaintext)
	if err == nil {
		t.Error("revoked key should fail validation")
	}
}

func TestRevokeByID_Success(t *testing.T) {
	m, _ := setupAuthManager(t)
	ctx := context.Background()

	_, key, _ := m.GenerateKey(ctx, RoleViewer, "revoke-me")

	err := m.RevokeByID(ctx, key.ID)
	if err != nil {
		t.Fatalf("RevokeByID error: %v", err)
	}
}

func TestRevokeByID_NotFound(t *testing.T) {
	m, _ := setupAuthManager(t)
	ctx := context.Background()

	err := m.RevokeByID(ctx, "nonexistent-id-12345")
	if err == nil {
		t.Error("should error for nonexistent ID")
	}
}

func TestRevokeByID_FallbackScan(t *testing.T) {
	m, _ := setupAuthManager(t)
	ctx := context.Background()

	// Generate key — removes secondary index to force fallback scan
	plaintext, key, _ := m.GenerateKey(ctx, RoleAdmin, "scan-test")

	// Delete the secondary index to force scan path
	m.client.Del(ctx, "auth:id2key:"+key.ID)

	// Should still find via scan fallback
	err := m.RevokeByID(ctx, key.ID)
	if err != nil {
		t.Fatalf("RevokeByID scan fallback error: %v", err)
	}

	// Verify revoked
	_, err = m.Validate(ctx, plaintext)
	if err == nil {
		t.Error("should be revoked")
	}
}

func TestMiddleware_ValidVeilKey(t *testing.T) {
	m, _ := setupAuthManager(t)
	ctx := context.Background()

	plaintext, _, _ := m.GenerateKey(ctx, RoleAdmin, "mw-test")

	var innerCalled bool
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !innerCalled {
		t.Error("valid veil key should pass through")
	}
	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestMiddleware_RevokedVeilKey(t *testing.T) {
	m, _ := setupAuthManager(t)
	ctx := context.Background()

	plaintext, key, _ := m.GenerateKey(ctx, RoleAdmin, "revoked-mw")
	m.RevokeByID(ctx, key.ID)

	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("revoked key should not pass through")
	}))

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for revoked key, got %d", w.Code)
	}
}
