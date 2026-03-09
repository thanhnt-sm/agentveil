package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallCheckCompliance_Valid(t *testing.T) {
	// Mock compliance endpoint
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		framework := r.URL.Query().Get("framework")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"framework": framework,
			"compliant": true,
			"checks":    []string{"data_retention", "encryption"},
		})
	}))
	defer backend.Close()

	s := &Server{config: Config{ProxyURL: backend.URL}}

	params, _ := json.Marshal(map[string]string{"framework": "gdpr"})
	result := s.callCheckCompliance(params)

	if len(result.Content) == 0 {
		t.Fatal("should return content")
	}
	if !strings.Contains(result.Content[0].Text, "gdpr") {
		t.Error("result should contain framework name")
	}
}

func TestCallCheckCompliance_InvalidParams(t *testing.T) {
	s := &Server{config: Config{ProxyURL: "http://localhost:1"}}

	result := s.callCheckCompliance(json.RawMessage(`{invalid json`))

	if len(result.Content) == 0 || !result.IsError {
		t.Error("should return error for invalid params")
	}
}

func TestCallCheckCompliance_BackendError(t *testing.T) {
	s := &Server{config: Config{ProxyURL: "http://localhost:1"}}

	params, _ := json.Marshal(map[string]string{"framework": "hipaa"})
	result := s.callCheckCompliance(params)

	if len(result.Content) == 0 || !result.IsError {
		t.Error("should return error when backend unreachable")
	}
}
