package proxy

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/vurakit/agentveil/internal/detector"
	"github.com/vurakit/agentveil/internal/logging"
	"github.com/vurakit/agentveil/internal/vault"
	"github.com/vurakit/agentveil/internal/webhook"
)

// anonymizeBody is the shared PII anonymization logic used by both
// single-target proxy (director) and multi-provider router (AnonymizeRequest).
// It reads the request body, detects PII, stores tokens in the vault, and
// replaces the body with the anonymized version.
//
// P0 FIX: Bodies exceeding MaxBodySize are REJECTED (not forwarded unanonymized).
func anonymizeBody(req *http.Request, det *detector.Detector, v *vault.Vault, sessionID string, wh *webhook.Dispatcher) {
	// N2: Scan query params for PII
	if rawQuery := req.URL.RawQuery; rawQuery != "" {
		scanQuery, queryMapping := det.Anonymize(rawQuery)
		if len(queryMapping) > 0 {
			slog.Warn("PII detected in query params", "count", len(queryMapping), "session", sessionID)
			req.URL.RawQuery = scanQuery
			ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
			defer cancel()
			if err := v.Store(ctx, sessionID, queryMapping); err != nil {
				slog.Error("vault store (query) error", "error", err)
			}
		}
	}

	limited := io.LimitReader(req.Body, MaxBodySize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		slog.Warn("error reading request body", "error", err, "session", sessionID)
		return
	}
	req.Body.Close()

	// P0 FIX #1: Block oversized bodies — do NOT forward unanonymized
	if int64(len(body)) > MaxBodySize {
		slog.Warn("request body too large, blocking",
			"size", len(body), "max", MaxBodySize, "session", sessionID)
		errBody := `{"error":"body_too_large","message":"request body exceeds maximum size for PII scanning"}`
		req.Body = io.NopCloser(bytes.NewBufferString(errBody))
		req.ContentLength = int64(len(errBody))
		return
	}

	anonymized, mapping := det.Anonymize(string(body))

	if len(mapping) > 0 {
		// Extract PII categories from tokens like [EMAIL_1], [CCCD_2]
		categories := make(map[string]bool)
		for token := range mapping {
			if len(token) > 2 && token[0] == '[' {
				inner := token[1 : len(token)-1]
				for i := len(inner) - 1; i >= 0; i-- {
					if inner[i] == '_' {
						categories[inner[:i]] = true
						break
					}
				}
			}
		}
		catList := make([]string, 0, len(categories))
		for c := range categories {
			catList = append(catList, c)
		}

		slog.Info("PII anonymized", "count", len(mapping), "session", sessionID)

		// P2 #15: Emit structured audit event for compliance trail
		logging.AuditEvent{
			Action:     "anonymize",
			SessionID:  sessionID,
			PIICount:   len(mapping),
			Categories: catList,
			Method:     req.Method,
			Path:       req.URL.Path,
		}.Log(slog.Default())

		ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
		defer cancel()
		if err := v.Store(ctx, sessionID, mapping); err != nil {
			slog.Error("vault store error", "error", err, "session", sessionID)
		}

		if wh != nil {
			wh.Emit(webhook.Event{
				Type:      webhook.EventPIIDetected,
				SessionID: sessionID,
				Data:      map[string]any{"count": len(mapping), "categories": catList},
			})
		}
	}

	req.Body = io.NopCloser(bytes.NewBufferString(anonymized))
	req.ContentLength = int64(len(anonymized))
}
