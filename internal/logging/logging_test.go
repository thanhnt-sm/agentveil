package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestSetup_DefaultLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("info", &buf)
	if logger == nil {
		t.Fatal("Setup returned nil logger")
	}

	logger.Info("test message")
	if !strings.Contains(buf.String(), "test message") {
		t.Error("info message should be logged at info level")
	}
}

func TestSetup_DebugLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("debug", &buf)
	logger.Debug("debug msg")
	if !strings.Contains(buf.String(), "debug msg") {
		t.Error("debug message should be logged at debug level")
	}
}

func TestSetup_WarnLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("warn", &buf)
	logger.Info("should not appear")
	logger.Warn("warning message")

	output := buf.String()
	if strings.Contains(output, "should not appear") {
		t.Error("info messages should not appear at warn level")
	}
	if !strings.Contains(output, "warning message") {
		t.Error("warn messages should appear at warn level")
	}
}

func TestSetup_WarningAlias(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("warning", &buf)
	logger.Warn("test warn")
	if !strings.Contains(buf.String(), "test warn") {
		t.Error("'warning' should be an alias for warn level")
	}
}

func TestSetup_ErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("error", &buf)
	logger.Warn("should not appear")
	logger.Error("error message")

	output := buf.String()
	if strings.Contains(output, "should not appear") {
		t.Error("warn messages should not appear at error level")
	}
	if !strings.Contains(output, "error message") {
		t.Error("error messages should appear at error level")
	}
}

func TestSetup_UnknownLevelDefaultsToInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("unknown", &buf)
	logger.Info("info msg")
	if !strings.Contains(buf.String(), "info msg") {
		t.Error("unknown level should default to info")
	}
}

func TestSetup_NilWriter(t *testing.T) {
	// Should not panic with nil writer (defaults to stdout)
	logger := Setup("info", nil)
	if logger == nil {
		t.Fatal("Setup returned nil logger with nil writer")
	}
}

func TestSetup_OutputIsJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("info", &buf)
	logger.Info("json test", "key", "value")

	// Should produce valid JSON
	var parsed map[string]any
	output := buf.String()
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("output should be valid JSON, got: %s", output)
	}
}

func TestAuditEvent_Log(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	event := AuditEvent{
		Action:     "anonymize",
		SessionID:  "sess-123",
		PIICount:   5,
		Categories: []string{"EMAIL", "PHONE"},
		Method:     "POST",
		Path:       "/v1/chat/completions",
	}

	event.Log(logger)

	output := buf.String()
	if !strings.Contains(output, "anonymize") {
		t.Error("should contain action")
	}
	if !strings.Contains(output, "sess-123") {
		t.Error("should contain session_id")
	}
	if !strings.Contains(output, "EMAIL") {
		t.Error("should contain categories")
	}
}

func TestAuditEvent_LogWithRole(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	event := AuditEvent{
		Action: "rehydrate",
		Role:   "admin",
	}
	event.Log(logger)

	if !strings.Contains(buf.String(), "admin") {
		t.Error("should contain role when set")
	}
}

func TestAuditEvent_LogWithRiskScore(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	event := AuditEvent{
		Action:    "audit",
		RiskScore: 8.5,
		KeyID:     "key-abc",
	}
	event.Log(logger)

	output := buf.String()
	if !strings.Contains(output, "8.5") {
		t.Error("should contain risk_score")
	}
	if !strings.Contains(output, "key-abc") {
		t.Error("should contain key_id")
	}
}

func TestAuditEvent_LogMinimalFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	event := AuditEvent{
		Action: "test",
	}
	event.Log(logger)

	if !strings.Contains(buf.String(), "audit") {
		t.Error("should log with 'audit' message")
	}
}
