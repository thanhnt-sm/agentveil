package proxy

import (
	"log/slog"
	"net/http"
	"strings"
)

// RoleMiddleware returns middleware that sets/validates X-User-Role header.
// This is the SINGLE exported version used by both single-target and router modes.
// P2 FIX #9: Removed duplicate unexported roleMiddleware method.
func RoleMiddleware(defaultRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := r.Header.Get("X-User-Role")
			if role == "" {
				role = defaultRole
				r.Header.Set("X-User-Role", role)
			}

			role = strings.ToLower(role)

			// Validate role
			switch role {
			case "admin", "viewer", "operator":
				// allowed
			default:
				slog.Warn("rejected unknown role", "role", role, "path", r.URL.Path)
				http.Error(w, `{"error":"forbidden","message":"unknown role"}`, http.StatusForbidden)
				return
			}

			slog.Info("request",
				"method", r.Method, "path", r.URL.Path,
				"role", role, "session", extractSessionID(r))

			next.ServeHTTP(w, r)
		})
	}
}

// securityEnforcer checks for blatant data exfiltration attempts in headers
func securityEnforcer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for key, values := range r.Header {
			for _, v := range values {
				if containsSuspiciousPayload(key) || containsSuspiciousPayload(v) {
					slog.Warn("blocked suspicious header", "header", key, "path", r.URL.Path)
					http.Error(w, `{"error":"forbidden","message":"security violation detected"}`, http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

var suspiciousPatterns = []string{
	"curl ",
	"wget ",
	"nc ",
	"/etc/passwd",
	"/etc/shadow",
	"base64 -d",
	"eval(",
	"exec(",
}

func containsSuspiciousPayload(s string) bool {
	lower := strings.ToLower(s)
	for _, pat := range suspiciousPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}
