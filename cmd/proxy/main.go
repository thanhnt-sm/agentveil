package main

import (
	"context"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/vurakit/agentveil/internal/auth"
	"github.com/vurakit/agentveil/internal/detector"
	"github.com/vurakit/agentveil/internal/logging"
	"github.com/vurakit/agentveil/internal/proxy"
	"github.com/vurakit/agentveil/internal/ratelimit"
	"github.com/vurakit/agentveil/internal/router"
	"github.com/vurakit/agentveil/internal/vault"
	"github.com/vurakit/agentveil/internal/webhook"
)

func main() {
	// Structured logging
	logLevel := envOr("LOG_LEVEL", "info")
	logger := logging.Setup(logLevel, os.Stdout)
	logger.Info("starting Agent Veil")

	// Configuration
	cfg := loadConfig()

	// Infrastructure
	redisClient := connectRedis(cfg.redisAddr, cfg.redisPassword, logger)
	v := setupVault(redisClient, cfg.encryptionKey, cfg.vaultTTL, logger)
	det := detector.New()
	authMgr := auth.NewManager(redisClient)
	rl := ratelimit.New(ratelimit.DefaultConfig())
	defer rl.Close()
	dispatcher := setupWebhooks(cfg.discordURL, cfg.slackURL, logger)
	if dispatcher != nil {
		defer dispatcher.Close()
	}

	// Build handler
	handler := buildHandler(cfg, det, v, authMgr, rl, dispatcher, logger)

	// Start server
	httpServer := &http.Server{
		Addr:         cfg.listenAddr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 600 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	gracefulShutdown(httpServer, redisClient, cfg, logger)
}

// appConfig holds all configuration values read from environment
type appConfig struct {
	targetURL     string
	listenAddr    string
	redisAddr     string
	redisPassword string
	encryptionKey string
	defaultRole   string
	tlsCert       string
	tlsKey        string
	routerConfig  string
	vaultTTL      string
	discordURL    string
	slackURL      string
}

// loadConfig reads all environment variables into a typed struct
func loadConfig() appConfig {
	return appConfig{
		targetURL:     envOr("TARGET_URL", "https://api.openai.com"),
		listenAddr:    envOr("LISTEN_ADDR", ":8080"),
		redisAddr:     envOr("REDIS_ADDR", "localhost:6379"),
		redisPassword: envOr("REDIS_PASSWORD", ""),
		encryptionKey: envOr("VEIL_ENCRYPTION_KEY", ""),
		defaultRole:   envOr("VEIL_DEFAULT_ROLE", "viewer"),
		tlsCert:       envOr("TLS_CERT", ""),
		tlsKey:        envOr("TLS_KEY", ""),
		routerConfig:  envOr("VEIL_ROUTER_CONFIG", ""),
		vaultTTL:      envOr("VEIL_VAULT_TTL", ""),
		discordURL:    envOr("VEIL_DISCORD_WEBHOOK_URL", ""),
		slackURL:      envOr("VEIL_SLACK_WEBHOOK_URL", ""),
	}
}

// connectRedis creates a Redis client and tests the connection
func connectRedis(addr, password string, logger *slog.Logger) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		logger.Warn("Redis not available, running without persistence", "error", err)
	} else {
		logger.Info("Redis connected", "addr", addr)
	}

	return client
}

// setupVault creates a vault with optional encryption and TTL configuration
func setupVault(redisClient *redis.Client, encryptionKey, vaultTTL string, logger *slog.Logger) *vault.Vault {
	v := vault.NewWithClient(redisClient)

	if vaultTTL != "" {
		if d, err := time.ParseDuration(vaultTTL); err == nil {
			v.SetTTL(d)
			logger.Info("vault TTL configured", "ttl", d)
		}
	}

	if encryptionKey != "" {
		keyBytes, err := hex.DecodeString(encryptionKey)
		if err != nil || len(keyBytes) != 32 {
			logger.Error("VEIL_ENCRYPTION_KEY must be 64 hex chars (32 bytes)", "len", len(encryptionKey))
			os.Exit(1)
		}
		enc, err := vault.NewEncryptor(keyBytes)
		if err != nil {
			logger.Error("failed to create encryptor", "error", err)
			os.Exit(1)
		}
		v.SetEncryptor(enc)
		logger.Info("vault encryption enabled (AES-256-GCM)")
	} else {
		logger.Warn("⚠️  VEIL_ENCRYPTION_KEY not set — PII stored UNENCRYPTED in Redis!")
		logger.Warn("⚠️  Set VEIL_ENCRYPTION_KEY for production use")
	}

	return v
}

// setupWebhooks creates a webhook dispatcher if Discord or Slack URLs are configured
func setupWebhooks(discordURL, slackURL string, logger *slog.Logger) *webhook.Dispatcher {
	if discordURL == "" && slackURL == "" {
		return nil
	}

	whCfg := webhook.DefaultConfig()
	if discordURL != "" {
		whCfg.Discord = &webhook.DiscordConfig{WebhookURL: discordURL}
		logger.Info("discord webhook enabled")
	}
	if slackURL != "" {
		whCfg.Slack = &webhook.SlackConfig{WebhookURL: slackURL}
		logger.Info("slack webhook enabled")
	}
	return webhook.NewDispatcher(whCfg)
}

// buildHandler creates the HTTP handler based on configuration mode (router or single-target)
func buildHandler(cfg appConfig, det *detector.Detector, v *vault.Vault, authMgr *auth.Manager, rl *ratelimit.Limiter, dispatcher *webhook.Dispatcher, logger *slog.Logger) http.Handler {
	if cfg.routerConfig != "" {
		return buildRouterHandler(cfg, det, v, authMgr, rl, dispatcher, logger)
	}
	return buildProxyHandler(cfg, det, v, authMgr, rl, dispatcher, logger)
}

// buildRouterHandler creates the multi-provider router handler
func buildRouterHandler(cfg appConfig, det *detector.Detector, v *vault.Vault, authMgr *auth.Manager, rl *ratelimit.Limiter, dispatcher *webhook.Dispatcher, logger *slog.Logger) http.Handler {
	routerCfg, err := router.LoadConfig(cfg.routerConfig)
	if err != nil {
		logger.Error("failed to load router config", "path", cfg.routerConfig, "error", err)
		os.Exit(1)
	}

	rt, err := router.New(routerCfg)
	if err != nil {
		logger.Error("failed to create router", "error", err)
		os.Exit(1)
	}

	rt.SetRequestModifier(proxy.AnonymizeRequest(det, v, dispatcher))
	rt.SetResponseModifier(proxy.RehydrateResponse(v, cfg.defaultRole))

	mux := http.NewServeMux()
	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/metrics", proxy.MetricsHandler())
	mux.HandleFunc("/scan", proxy.HandleScan(det))
	mux.HandleFunc("/audit", proxy.HandleAudit())
	mux.HandleFunc("/dashboard", proxy.HandleDashboard())
	mux.HandleFunc("/dashboard/", proxy.HandleDashboard())
	mux.HandleFunc("/dashboard/api/status", proxy.HandleDashboardStatus(rt.GetProviders(), true))
	mux.HandleFunc("/dashboard/api/logs", proxy.HandleDashboardLogs())
	mux.HandleFunc("/dashboard/api/reports", proxy.HandleDashboardReports())

	var routerHandler http.Handler = rt
	routerHandler = proxy.RoleMiddleware(cfg.defaultRole)(routerHandler)
	if authMgr != nil {
		routerHandler = authMgr.Middleware(routerHandler)
	}
	mux.Handle("/", routerHandler)

	// N12: Config hot-reload watcher
	go watchConfigReload(cfg.routerConfig, rt, det, v, dispatcher, cfg.defaultRole, logger)

	logger.Info("router mode enabled", "config", cfg.routerConfig, "providers", rt.GetProviders())
	return proxy.RequestTracingMiddleware(rl.Middleware(mux))
}

// buildProxyHandler creates the single-target proxy handler
func buildProxyHandler(cfg appConfig, det *detector.Detector, v *vault.Vault, authMgr *auth.Manager, rl *ratelimit.Limiter, dispatcher *webhook.Dispatcher, logger *slog.Logger) http.Handler {
	opts := []proxy.Option{proxy.WithAuth(authMgr)}
	if dispatcher != nil {
		opts = append(opts, proxy.WithWebhook(dispatcher))
	}

	srv, err := proxy.New(
		proxy.Config{TargetURL: cfg.targetURL, DefaultRole: cfg.defaultRole},
		det, v,
		opts...,
	)
	if err != nil {
		logger.Error("failed to create proxy", "error", err)
		os.Exit(1)
	}

	return rl.Middleware(srv.Handler())
}

// gracefulShutdown handles server start, signal capture, and graceful stop
func gracefulShutdown(httpServer *http.Server, redisClient *redis.Client, cfg appConfig, logger *slog.Logger) {
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		if cfg.routerConfig != "" {
			logger.Info("proxy listening (router mode)", "addr", cfg.listenAddr)
		} else {
			logger.Info("proxy listening", "addr", cfg.listenAddr, "target", cfg.targetURL)
		}
		if cfg.tlsCert != "" && cfg.tlsKey != "" {
			logger.Info("TLS enabled", "cert", cfg.tlsCert)
			if err := httpServer.ListenAndServeTLS(cfg.tlsCert, cfg.tlsKey); err != nil && err != http.ErrServerClosed {
				logger.Error("server error", "error", err)
				os.Exit(1)
			}
		} else {
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("server error", "error", err)
				os.Exit(1)
			}
		}
	}()

	<-done
	logger.Info("shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
	if err := redisClient.Close(); err != nil {
		logger.Error("redis close error", "error", err)
	}

	logger.Info("stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// N12: Config hot-reload — watches router.yaml for mtime changes
func watchConfigReload(configPath string, rt *router.Router, det *detector.Detector, v *vault.Vault, wh *webhook.Dispatcher, defaultRole string, logger *slog.Logger) {
	absPath, _ := filepath.Abs(configPath)
	var lastMod time.Time
	if info, err := os.Stat(absPath); err == nil {
		lastMod = info.ModTime()
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}
		if info.ModTime().Equal(lastMod) {
			continue
		}
		lastMod = info.ModTime()

		logger.Info("config change detected, reloading", "path", absPath)
		cfg, err := router.LoadConfig(configPath)
		if err != nil {
			logger.Error("reload failed: invalid config", "error", err)
			continue
		}

		newRt, err := router.New(cfg)
		if err != nil {
			logger.Error("reload failed: router init", "error", err)
			continue
		}

		newRt.SetRequestModifier(proxy.AnonymizeRequest(det, v, wh))
		newRt.SetResponseModifier(proxy.RehydrateResponse(v, defaultRole))

		rt.HotSwap(newRt)
		logger.Info("config reloaded successfully", "providers", newRt.GetProviders())
	}
}
