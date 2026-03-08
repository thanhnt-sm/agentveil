package proxy

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/vurakit/agentveil/internal/detector"
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
		// Replace body with error JSON so upstream gets a clear rejection
		errBody := `{"error":"body_too_large","message":"request body exceeds maximum size for PII scanning"}`
		req.Body = io.NopCloser(bytes.NewBufferString(errBody))
		req.ContentLength = int64(len(errBody))
		return
	}

	anonymized, mapping := det.Anonymize(string(body))

	if len(mapping) > 0 {
		slog.Info("PII anonymized", "count", len(mapping), "session", sessionID)

		if err := v.Store(context.Background(), sessionID, mapping); err != nil {
			slog.Error("vault store error", "error", err, "session", sessionID)
		}

		if wh != nil {
			wh.Emit(webhook.Event{
				Type:      webhook.EventPIIDetected,
				SessionID: sessionID,
				Data:      map[string]any{"count": len(mapping)},
			})
		}
	}

	req.Body = io.NopCloser(bytes.NewBufferString(anonymized))
	req.ContentLength = int64(len(anonymized))
}
