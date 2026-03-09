package router

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Provider wraps config with runtime state
type Provider struct {
	Config  ProviderConfig
	Target  *url.URL
	Proxy   *httputil.ReverseProxy
	healthy atomic.Bool
}

// Router routes requests to multiple LLM providers
type Router struct {
	providers    map[string]*Provider
	routes       map[string]RouteConfig // path prefix → route config
	sortedRoutes []string               // P2 #10: sorted by prefix length desc
	defaultRoute string
	strategy     LoadBalanceStrategy
	fallback     FallbackConfig

	// Codex OAuth URL rewriter (nil if disabled)
	codexRewriter *codexRewriter

	// P3 #22: WebSocket proxy (nil if disabled)
	wsProxy *WebSocketProxy

	// Round-robin state
	mu       sync.Mutex
	rrIndex  int
	rrList   []string // provider names for round-robin

	// Weighted state
	weightedList []string // expanded list based on weights

	// Request modifier — applied before forwarding (e.g. PII anonymization)
	requestModifier func(*http.Request)
	// Response modifier — applied after receiving response (e.g. PII rehydration)
	responseModifier func(*http.Response) error
}

// New creates a Router from config
func New(cfg *RouterConfig) (*Router, error) {
	r := &Router{
		providers:    make(map[string]*Provider),
		routes:       make(map[string]RouteConfig),
		defaultRoute: cfg.DefaultRoute,
		strategy:     cfg.LoadBalance,
		fallback:     cfg.Fallback,
	}

	for _, pc := range cfg.Providers {
		if !pc.Enabled {
			continue
		}
		p, err := r.buildProvider(pc)
		if err != nil {
			return nil, err
		}
		r.providers[pc.Name] = p
	}


	if len(r.providers) == 0 {
		return nil, fmt.Errorf("no enabled providers")
	}

	// Build routes
	for _, rc := range cfg.Routes {
		r.routes[rc.PathPrefix] = rc
		r.sortedRoutes = append(r.sortedRoutes, rc.PathPrefix)
	}
	// P2 #10: Sort by prefix length descending for deterministic matching
	sort.Slice(r.sortedRoutes, func(i, j int) bool {
		return len(r.sortedRoutes[i]) > len(r.sortedRoutes[j])
	})

	// Set default if not configured
	if r.defaultRoute == "" {
		for name := range r.providers {
			r.defaultRoute = name
			break
		}
	}

	// Build round-robin and weighted lists
	r.buildLoadBalanceLists()

	// Initialize Codex OAuth URL rewriter if enabled
	if cfg.CodexRewrite.Enabled {
		backendURL := cfg.CodexRewrite.BackendURL
		if backendURL == "" {
			backendURL = DefaultCodexBackendURL
		}
		cr, err := newCodexRewriter(backendURL)
		if err != nil {
			return nil, err
		}
		r.codexRewriter = cr
		slog.Info("codex OAuth rewrite enabled", "backend", backendURL)
	}

	// P3 #22: Initialize WebSocket proxy if enabled
	if cfg.WebSocket.Enabled {
		r.wsProxy = NewWebSocketProxy(cfg.WebSocket)
		slog.Info("websocket proxy enabled")
	}

	return r, nil
}

// buildProvider creates a Provider with its reverse proxy from a ProviderConfig
func (r *Router) buildProvider(pc ProviderConfig) (*Provider, error) {
	target, err := url.Parse(pc.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("provider %s: invalid URL %s: %w", pc.Name, pc.BaseURL, err)
	}

	p := &Provider{Config: pc, Target: target}
	p.healthy.Store(true)

	p.Proxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host

			if target.Path != "" && target.Path != "/" {
				req.URL.Path = singleJoiningSlash(target.Path, req.URL.Path)
			}

			if pc.AuthMethod != "passthrough" && pc.APIKey != "" {
				switch pc.AuthMethod {
				case "query":
					q := req.URL.Query()
					q.Set(pc.AuthParam, pc.APIKey)
					req.URL.RawQuery = q.Encode()
				case "x-api-key":
					req.Header.Set("x-api-key", pc.APIKey)
				default:
					req.Header.Set("Authorization", "Bearer "+pc.APIKey)
				}
			}

			if r.requestModifier != nil {
				slog.Debug("applying request modifier", "provider", pc.Name, "path", req.URL.Path)
				r.requestModifier(req)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			if r.responseModifier != nil {
				return r.responseModifier(resp)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			slog.Warn("provider error", "provider", pc.Name, "error", err)
			p.healthy.Store(false)
			go func() {
				time.Sleep(30 * time.Second)
				p.healthy.Store(true)
				slog.Info("provider health restored", "provider", pc.Name)
			}()
			http.Error(w, fmt.Sprintf(`{"error":"provider_error","provider":"%s"}`, pc.Name), http.StatusBadGateway)
		},
		Transport: &http.Transport{
			ResponseHeaderTimeout: time.Duration(pc.TimeoutSec) * time.Second,
		},
	}

	return p, nil
}

// SetRequestModifier sets a function that modifies requests before forwarding
func (r *Router) SetRequestModifier(fn func(*http.Request)) {
	r.requestModifier = fn
}

// SetResponseModifier sets a function that modifies responses before returning to client
func (r *Router) SetResponseModifier(fn func(*http.Response) error) {
	r.responseModifier = fn
}

// HotSwap atomically swaps this router's internals with those from another router.
// N12: Enables config hot-reload without restart.
func (r *Router) HotSwap(other *Router) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.providers = other.providers
	r.routes = other.routes
	r.sortedRoutes = other.sortedRoutes
	r.defaultRoute = other.defaultRoute
	r.strategy = other.strategy
	r.fallback = other.fallback
	r.codexRewriter = other.codexRewriter
	r.wsProxy = other.wsProxy
	r.rrList = other.rrList
	r.rrIndex = 0
	r.weightedList = other.weightedList
}

func (r *Router) buildLoadBalanceLists() {
	// Priority-sorted list
	var names []string
	for name := range r.providers {
		names = append(names, name)
	}

	// Sort by priority (lower = higher priority)
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if r.providers[names[j]].Config.Priority < r.providers[names[i]].Config.Priority {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	r.rrList = names

	// Weighted list
	r.weightedList = nil
	for _, name := range names {
		p := r.providers[name]
		for range p.Config.Weight {
			r.weightedList = append(r.weightedList, name)
		}
	}
}

// ServeHTTP routes the request to the appropriate provider
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// P3 #22: WebSocket upgrade — proxy as a raw TCP tunnel
	if r.wsProxy != nil && IsWebSocketUpgrade(req) {
		providerName := r.resolveProvider(req)
		if p, ok := r.providers[providerName]; ok {
			slog.Info("websocket routing", "provider", providerName, "path", req.URL.Path)
			r.wsProxy.ServeHTTP(w, req, p.Target)
			return
		}
	}

	// Codex OAuth rewrite: detect JWT tokens and redirect /v1/responses to ChatGPT backend
	if r.codexRewriter != nil && strings.HasPrefix(req.URL.Path, "/v1/responses") {
		authHeader := req.Header.Get("Authorization")
		if IsCodexOAuthToken(authHeader) {
			r.codexRewriter.ServeHTTP(w, req)
			return
		}
	}

	providerName := r.resolveProvider(req)

	if r.fallback.Enabled {
		r.serveWithFallback(w, req, providerName)
		return
	}

	p, ok := r.providers[providerName]
	if !ok || !p.healthy.Load() {
		http.Error(w, `{"error":"no_healthy_provider"}`, http.StatusServiceUnavailable)
		return
	}

	// Strip the route prefix from the path
	req.URL.Path = r.stripRoutePrefix(req.URL.Path)

	slog.Debug("routing request", "provider", providerName, "path", req.URL.Path)
	p.Proxy.ServeHTTP(w, req)
}

func (r *Router) serveWithFallback(w http.ResponseWriter, req *http.Request, primaryName string) {
	// Build fallback order: primary first, then others by priority
	order := []string{primaryName}
	for _, name := range r.rrList {
		if name != primaryName {
			order = append(order, name)
		}
	}

	attempts := r.fallback.MaxAttempts
	if attempts > len(order) {
		attempts = len(order)
	}

	// P1 #7: Save request body for retries (bounded to 10MB to prevent OOM)
	var savedBody []byte
	if req.Body != nil {
		limited := io.LimitReader(req.Body, 10*1024*1024+1)
		savedBody, _ = io.ReadAll(limited)
		req.Body.Close()
		if int64(len(savedBody)) > 10*1024*1024 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			w.Write([]byte(`{"error":"body_too_large"}`))
			return
		}
		req.Body = io.NopCloser(bytes.NewReader(savedBody))
	}

	originalPath := req.URL.Path

	for i := 0; i < attempts; i++ {
		name := order[i]
		p, ok := r.providers[name]
		if !ok || !p.healthy.Load() {
			slog.Warn("provider unhealthy, trying next", "provider", name, "attempt", i+1)
			continue
		}

		// Use a response recorder to detect errors without writing to real response
		rec := &fallbackRecorder{
			ResponseWriter: w,
			statusCode:     0,
			headerWritten:  false,
		}

		req.URL.Path = r.stripRoutePrefix(originalPath)

		// P1 #7: Restore body for this attempt
		if savedBody != nil {
			req.Body = io.NopCloser(bytes.NewReader(savedBody))
			req.ContentLength = int64(len(savedBody))
		}

		slog.Debug("routing request (fallback)", "provider", name, "attempt", i+1, "path", req.URL.Path)
		p.Proxy.ServeHTTP(rec, req)

		// If successful or client error, flush to real writer and return (don't retry on 4xx)
		if rec.statusCode > 0 && rec.statusCode < 500 {
			rec.flush()
			return
		}

		// If statusCode is 0 but body was written, proxy wrote directly — treat as done
		if rec.statusCode == 0 && len(rec.body) > 0 {
			rec.flush()
			return
		}

		// Server error — try next provider
		slog.Warn("provider returned error, falling back",
			"provider", name, "status", rec.statusCode, "attempt", i+1)
		req.URL.Path = originalPath

		if i < attempts-1 && r.fallback.RetryDelaySec > 0 {
			select {
			case <-req.Context().Done():
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusGatewayTimeout)
				w.Write([]byte(`{"error":"client_cancelled"}`))
				return
			case <-time.After(time.Duration(r.fallback.RetryDelaySec) * time.Second):
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	w.Write([]byte(`{"error":"all_providers_failed"}`))
}

// resolveProvider determines which provider to use for a request
func (r *Router) resolveProvider(req *http.Request) string {
	// 1. Check explicit provider header
	if provider := req.Header.Get("X-Veil-Provider"); provider != "" {
		if _, ok := r.providers[provider]; ok {
			return provider
		}
	}

	// 2. Check path-based routes (sorted by prefix length desc for deterministic matching)
	for _, prefix := range r.sortedRoutes {
		if strings.HasPrefix(req.URL.Path, prefix) {
			return r.routes[prefix].Provider
		}
	}

	// 3. Load balancing across providers
	switch r.strategy {
	case StrategyRoundRobin:
		return r.nextRoundRobin()
	case StrategyWeighted:
		return r.nextWeighted()
	default: // StrategyPriority
		return r.nextPriority()
	}
}

func (r *Router) nextRoundRobin() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.rrList) == 0 {
		return r.defaultRoute
	}

	// Find next healthy provider
	for range r.rrList {
		name := r.rrList[r.rrIndex%len(r.rrList)]
		r.rrIndex++
		if p := r.providers[name]; p != nil && p.healthy.Load() {
			return name
		}
	}
	return r.defaultRoute
}

func (r *Router) nextWeighted() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.weightedList) == 0 {
		return r.defaultRoute
	}

	for range r.weightedList {
		name := r.weightedList[r.rrIndex%len(r.weightedList)]
		r.rrIndex++
		if p := r.providers[name]; p != nil && p.healthy.Load() {
			return name
		}
	}
	return r.defaultRoute
}

func (r *Router) nextPriority() string {
	for _, name := range r.rrList {
		if p := r.providers[name]; p != nil && p.healthy.Load() {
			return name
		}
	}
	return r.defaultRoute
}

// singleJoiningSlash joins two URL path segments with exactly one slash.
func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

// stripRoutePrefix removes the route prefix from the path, only if the route has StripPrefix=true.
// E.g. /gemini/v1beta → /v1beta (strip_prefix: true), but /v1/responses stays as-is.
func (r *Router) stripRoutePrefix(path string) string {
	for prefix, rc := range r.routes {
		if strings.HasPrefix(path, prefix) && rc.StripPrefix {
			stripped := strings.TrimPrefix(path, prefix)
			if stripped == "" {
				return "/"
			}
			if !strings.HasPrefix(stripped, "/") {
				stripped = "/" + stripped
			}
			return stripped
		}
	}
	return path
}

// GetProviders returns the list of provider names
func (r *Router) GetProviders() []string {
	var names []string
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// IsHealthy returns the health status of a provider
func (r *Router) IsHealthy(name string) bool {
	if p, ok := r.providers[name]; ok {
		return p.healthy.Load()
	}
	return false
}

// SetHealthy manually sets provider health (for testing)
func (r *Router) SetHealthy(name string, healthy bool) {
	if p, ok := r.providers[name]; ok {
		p.healthy.Store(healthy)
	}
}

// fallbackRecorder buffers the response to detect server errors.
// Headers and body are buffered so fallback retries don't leak partial
// responses to the real ResponseWriter (which would cause "superfluous
// response.WriteHeader call" warnings from net/http).
type fallbackRecorder struct {
	http.ResponseWriter
	statusCode    int
	headerWritten bool
	flushed       bool // guard against double flush
	headers       http.Header
	body          []byte
}

func (fr *fallbackRecorder) Header() http.Header {
	if fr.headers == nil {
		fr.headers = make(http.Header)
	}
	return fr.headers
}

func (fr *fallbackRecorder) WriteHeader(code int) {
	if fr.headerWritten {
		return // guard: ignore duplicate WriteHeader calls
	}
	fr.statusCode = code
	fr.headerWritten = true
}

func (fr *fallbackRecorder) Write(b []byte) (int, error) {
	if !fr.headerWritten {
		fr.statusCode = http.StatusOK
		fr.headerWritten = true
	}
	fr.body = append(fr.body, b...)
	return len(b), nil
}

// flush writes the buffered response to the real ResponseWriter.
func (fr *fallbackRecorder) flush() {
	if fr.flushed {
		return // guard: prevent superfluous WriteHeader
	}
	fr.flushed = true

	dst := fr.ResponseWriter.Header()
	for k, vv := range fr.headers {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
	if fr.statusCode > 0 {
		fr.ResponseWriter.WriteHeader(fr.statusCode)
	}
	if len(fr.body) > 0 {
		fr.ResponseWriter.Write(fr.body) //nolint:errcheck
	}
}

// Flush implements http.Flusher so that httputil.ReverseProxy can
// stream SSE events through the fallback recorder without hanging.
func (fr *fallbackRecorder) Flush() {
	if f, ok := fr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
