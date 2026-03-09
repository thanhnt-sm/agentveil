package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouter_HotSwap(t *testing.T) {
	cfg1 := &RouterConfig{
		Providers: []ProviderConfig{
			{Name: "p1", BaseURL: "http://p1.test", Enabled: true, Priority: 1, Weight: 1},
		},
		LoadBalance:  StrategyPriority,
		DefaultRoute: "p1",
	}
	r1, err := New(cfg1)
	if err != nil {
		t.Fatal(err)
	}

	// Verify initial state
	providers := r1.GetProviders()
	if len(providers) != 1 || providers[0] != "p1" {
		t.Fatalf("expected [p1], got %v", providers)
	}

	// Create new router with different providers
	cfg2 := &RouterConfig{
		Providers: []ProviderConfig{
			{Name: "p1", BaseURL: "http://p1.test", Enabled: true, Priority: 1, Weight: 1},
			{Name: "p2", BaseURL: "http://p2.test", Enabled: true, Priority: 2, Weight: 1},
		},
		LoadBalance:  StrategyPriority,
		DefaultRoute: "p1",
	}
	r2, err := New(cfg2)
	if err != nil {
		t.Fatal(err)
	}

	// Hot swap
	r1.HotSwap(r2)

	// Verify swapped state
	providers = r1.GetProviders()
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers after swap, got %d: %v", len(providers), providers)
	}
}

func TestRouter_ResolveProvider_Default(t *testing.T) {
	cfg := &RouterConfig{
		Providers: []ProviderConfig{
			{Name: "default-prov", BaseURL: "http://default.test", Enabled: true, Priority: 1, Weight: 1},
		},
		LoadBalance:  StrategyPriority,
		DefaultRoute: "default-prov",
	}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	name := r.resolveProvider(req)
	if name != "default-prov" {
		t.Errorf("expected default-prov, got %s", name)
	}
}

func TestRouter_ResolveProvider_HeaderOverride(t *testing.T) {
	cfg := &RouterConfig{
		Providers: []ProviderConfig{
			{Name: "openai", BaseURL: "http://openai.test", Enabled: true, Priority: 1, Weight: 1},
			{Name: "anthropic", BaseURL: "http://anthropic.test", Enabled: true, Priority: 2, Weight: 1},
		},
		LoadBalance:  StrategyPriority,
		DefaultRoute: "openai",
	}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("X-Veil-Provider", "anthropic")
	name := r.resolveProvider(req)
	if name != "anthropic" {
		t.Errorf("expected anthropic via header override, got %s", name)
	}
}

func TestRouter_ResolveProvider_PathRouting(t *testing.T) {
	cfg := &RouterConfig{
		Providers: []ProviderConfig{
			{Name: "openai", BaseURL: "http://openai.test", Enabled: true, Priority: 1, Weight: 1},
			{Name: "gemini", BaseURL: "http://gemini.test", Enabled: true, Priority: 2, Weight: 1},
		},
		Routes: []RouteConfig{
			{PathPrefix: "/gemini", Provider: "gemini", StripPrefix: true},
		},
		LoadBalance:  StrategyPriority,
		DefaultRoute: "openai",
	}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/gemini/v1beta/models", nil)
	name := r.resolveProvider(req)
	if name != "gemini" {
		t.Errorf("expected gemini via path routing, got %s", name)
	}
}

func TestRouter_ServeHTTP_Health(t *testing.T) {
	cfg := &RouterConfig{
		Providers: []ProviderConfig{
			{Name: "test", BaseURL: "http://test.local", Enabled: true, Priority: 1, Weight: 1},
		},
		DefaultRoute: "test",
	}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Router should route requests — test that it doesn't panic
	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()

	// This will fail to connect but shouldn't panic
	r.ServeHTTP(w, req)

	if w.Code == 0 {
		t.Error("expected a response code")
	}
}

func TestRouter_SetModifiers(t *testing.T) {
	cfg := &RouterConfig{
		Providers: []ProviderConfig{
			{Name: "test", BaseURL: "http://test.local", Enabled: true, Priority: 1, Weight: 1},
		},
		DefaultRoute: "test",
	}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	reqModified := false
	r.SetRequestModifier(func(req *http.Request) {
		reqModified = true
	})

	respModified := false
	r.SetResponseModifier(func(resp *http.Response) error {
		respModified = true
		return nil
	})

	if r.requestModifier == nil {
		t.Error("request modifier not set")
	}
	if r.responseModifier == nil {
		t.Error("response modifier not set")
	}
	_ = reqModified
	_ = respModified
}

func TestRouter_NoProviders(t *testing.T) {
	cfg := &RouterConfig{
		Providers: []ProviderConfig{},
	}
	_, err := New(cfg)
	if err == nil {
		t.Error("expected error for empty providers")
	}
}

func TestRouter_DisabledProviders(t *testing.T) {
	cfg := &RouterConfig{
		Providers: []ProviderConfig{
			{Name: "disabled", BaseURL: "http://test.local", Enabled: false},
		},
	}
	_, err := New(cfg)
	if err == nil {
		t.Error("expected error when all providers disabled")
	}
}

func TestRouter_WebSocketConfig(t *testing.T) {
	cfg := &RouterConfig{
		Providers: []ProviderConfig{
			{Name: "test", BaseURL: "http://test.local", Enabled: true, Priority: 1, Weight: 1},
		},
		DefaultRoute: "test",
		WebSocket:    WebSocketConfig{Enabled: true},
	}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if r.wsProxy == nil {
		t.Error("expected websocket proxy to be initialized")
	}
}
