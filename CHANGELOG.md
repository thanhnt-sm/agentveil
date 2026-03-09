# Changelog

All notable changes to AgentVeil are documented here.

## [Unreleased] — 2026-03-09

### 🔒 Security
- **CRITICAL**: Replace `math/rand` with `crypto/rand` for backoff jitter generation
- Add 3s context timeouts to all 12 Redis operations (prevents indefinite hangs)
- Fix ignored `json.Marshal` error in cache key generation

### 🚀 Features (34 Improvements)
- **PII Protection**: 60+ regex patterns (Vietnam + International + Secrets), AES-256-GCM vault encryption
- **Multi-Provider Router**: OpenAI, Anthropic, Gemini, Ollama with fallback and body preservation
- **Prompt Injection Defense**: 11 detection patterns with configurable blocking
- **Circuit Breaker**: Error rate tracking, half-open recovery, exponential backoff with jitter
- **Active Health Probes**: Periodic provider health checks with context timeout
- **Semantic Caching**: SHA-256 keyed, Redis-backed, 5-minute TTL
- **Prometheus Metrics**: `/metrics` endpoint with 8 counters
- **Token Usage Tracking**: Request/response token counting
- **Request Tracing**: `X-Veil-Request-ID` header on every request
- **Canary Tokens**: Redis-persisted with 24-hour TTL for leak detection
- **Config Hot-Reload**: File watcher with 10s polling interval
- **WebSocket Proxy**: Codex v2 `responses_websockets` support
- **Codex OAuth JWT Rewriting**: IDE-compatible OAuth token passthrough
- **Dashboard UI**: Status, logs, and reports API
- **Webhook Alerts**: Discord + Slack notifications on PII events
- **Rate Limiting**: Token bucket with sliding window
- **API Key Auth**: SHA-256 hashed, 90-day TTL, role-based access

### ♻️ Refactoring
- Extract `main.go` from 215-line monolith → 37-line `main()` + 9 focused helpers
- Add `appConfig` struct for typed environment variable configuration

### 🧪 Testing
- **321 tests** across 16 suites (was ~50)
- `logging`: 100% coverage
- `pkg/pii`: 97.2% coverage (was 36%)
- `proxy`: 68.5% coverage (was 57%)
- `router`: 64% coverage (was 59%)
- `media`: 62.4% coverage (was 46%)
- All tests pass with `-race` flag

### 📝 Documentation
- Updated README with all new features, configuration, and project structure
- Added CHANGELOG.md

## [0.1.0] — 2026-02-28

### Initial Release
- Single-target reverse proxy for OpenAI API
- Basic PII detection and anonymization
- Redis-backed vault for token storage
- Rate limiting middleware
