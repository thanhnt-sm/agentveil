package guardrail

import (
	"regexp"
	"testing"
)

func TestExtractOutputText_OpenAIFormat(t *testing.T) {
	body := `{"choices":[{"message":{"content":"hello world"}}]}`
	result := extractOutputText(body)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestExtractOutputText_OpenAIMultiChoice(t *testing.T) {
	body := `{"choices":[{"message":{"content":"first"}},{"message":{"content":"second"}}]}`
	result := extractOutputText(body)
	if result != "first\nsecond" {
		t.Errorf("expected 'first\\nsecond', got %q", result)
	}
}

func TestExtractOutputText_StreamDelta(t *testing.T) {
	body := `{"choices":[{"delta":{"content":"streaming chunk"}}]}`
	result := extractOutputText(body)
	if result != "streaming chunk" {
		t.Errorf("expected 'streaming chunk', got %q", result)
	}
}

func TestExtractOutputText_AnthropicFormat(t *testing.T) {
	body := `{"content":[{"type":"text","text":"anthropic response"}]}`
	result := extractOutputText(body)
	if result != "anthropic response" {
		t.Errorf("expected 'anthropic response', got %q", result)
	}
}

func TestExtractOutputText_AnthropicMultiBlock(t *testing.T) {
	body := `{"content":[{"type":"text","text":"block1"},{"type":"text","text":"block2"}]}`
	result := extractOutputText(body)
	if result != "block1\nblock2" {
		t.Errorf("expected 'block1\\nblock2', got %q", result)
	}
}

func TestExtractOutputText_InvalidJSON(t *testing.T) {
	result := extractOutputText("{invalid json")
	if result != "{invalid json" {
		t.Errorf("invalid JSON should return raw body, got %q", result)
	}
}

func TestExtractOutputText_EmptyChoices(t *testing.T) {
	result := extractOutputText(`{"choices":[]}`)
	if result != "" {
		t.Errorf("empty choices should return empty, got %q", result)
	}
}

func TestExtractOutputText_NoContent(t *testing.T) {
	result := extractOutputText(`{"choices":[{"message":{}}]}`)
	if result != "" {
		t.Errorf("no content should return empty, got %q", result)
	}
}

func TestExtractMatch_BasicMatch(t *testing.T) {
	pattern := regexp.MustCompile(`\b\d{3}-\d{3}-\d{4}\b`)
	result := extractMatch("Call 555-123-4567 for info", pattern, 100)
	if result == "" {
		t.Error("should match phone number pattern")
	}
}

func TestExtractMatch_NoMatch(t *testing.T) {
	pattern := regexp.MustCompile(`\b\d{3}-\d{3}-\d{4}\b`)
	result := extractMatch("no numbers here", pattern, 100)
	if result != "" {
		t.Error("should return empty for no match")
	}
}

func TestExtractMatch_TruncatedMatch(t *testing.T) {
	pattern := regexp.MustCompile(`.+`)
	longText := "this is a very long string that exceeds the max length limit for matching purposes"
	result := extractMatch(longText, pattern, 20)
	if len(result) > 20 {
		t.Errorf("result should be truncated to 20 chars, got %d", len(result))
	}
}
