package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestMiddleware_MissingAuth(t *testing.T) {
	m := &Manager{prefix: "auth:keys:"}
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach inner handler")
	}))

	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestMiddleware_InvalidAuthFormat(t *testing.T) {
	m := &Manager{prefix: "auth:keys:"}
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach inner handler")
	}))

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz") // Basic auth, not Bearer
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for non-Bearer, got %d", w.Code)
	}
}

func TestMiddleware_PassthroughNonVeilKey(t *testing.T) {
	m := &Manager{prefix: "auth:keys:"}
	var innerCalled bool
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer sk-openai-key-12345")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !innerCalled {
		t.Error("non-veil key should pass through to inner handler")
	}
	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestMiddleware_XAPIKeyPassthrough(t *testing.T) {
	m := &Manager{prefix: "auth:keys:"}
	var innerCalled bool
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("x-api-key", "sk-ant-key-12345")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !innerCalled {
		t.Error("x-api-key should pass through to inner handler")
	}
	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestMiddleware_VeilKeyWithoutRedis(t *testing.T) {
	// veil_sk_ key with Redis pointing to invalid addr → Validate fails → 401
	m := NewManager(redis.NewClient(&redis.Options{Addr: "localhost:1"}))
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach inner handler with invalid veil key")
	}))

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer veil_sk_test_key_12345")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unvalidatable veil key, got %d", w.Code)
	}
}
