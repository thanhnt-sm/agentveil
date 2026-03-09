package promptguard

import (
	"testing"
)

func TestContains(t *testing.T) {
	tests := []struct {
		text  string
		token string
		want  bool
	}{
		{"hello world with secret token inside", "secret token", true},
		{"hello world", "missing", false},
		{"", "token", false},
		{"text", "", false},
		{"exact", "exact", true},
		{"prefix_match_here", "prefix", true},
		{"here_suffix_match", "suffix_match", true},
		{"short", "this is much longer than the text", false},
	}

	for _, tt := range tests {
		got := contains(tt.text, tt.token)
		if got != tt.want {
			t.Errorf("contains(%q, %q) = %v, want %v", tt.text, tt.token, got, tt.want)
		}
	}
}

func TestContainsSubstring(t *testing.T) {
	tests := []struct {
		s    string
		sub  string
		want bool
	}{
		{"abcdefghij", "cde", true},
		{"abcdefghij", "xyz", false},
		{"abcdefghij", "abcdefghij", true},
		{"abc", "abcd", false},
		{"hello", "lo", true},
		{"hello", "he", true},
	}

	for _, tt := range tests {
		got := contains(tt.s, tt.sub)
		if got != tt.want {
			t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.sub, got, tt.want)
		}
	}
}

func TestCanaryStore_GenerateAndCheckExtra(t *testing.T) {
	store := NewCanaryStore()

	// Generate canary
	canary := store.Generate("session-1")
	if canary.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if canary.SessionID != "session-1" {
		t.Errorf("expected session-1, got %s", canary.SessionID)
	}

	// Check leaked — should find token in text containing it
	text := "some output containing " + canary.Token + " leaked data"
	leaked := store.CheckLeaked(text)
	if len(leaked) == 0 {
		t.Error("expected to detect leaked canary token")
	}

	// Text without token — should not detect
	clean := "clean text without any tokens"
	leaked = store.CheckLeaked(clean)
	if len(leaked) != 0 {
		t.Error("should not detect leak in clean text")
	}
}

func TestCanaryStore_InjectCanary(t *testing.T) {
	store := NewCanaryStore()

	original := "This is a test prompt for the AI"
	injected, canary := store.InjectCanary(original, "sess-2")

	if canary.Token == "" {
		t.Fatal("expected non-empty token")
	}

	// Injected text might differ from original (contains zero-width chars)
	if len(injected) < len(original) {
		t.Error("injected text should be at least as long as original")
	}
}

func TestCanaryStore_RemoveExtra(t *testing.T) {
	store := NewCanaryStore()

	canary := store.Generate("session-1")
	text := "text with " + canary.Token + " in it"

	// Should detect before remove
	leaked := store.CheckLeaked(text)
	if len(leaked) == 0 {
		t.Fatal("should detect canary before removal")
	}

	// Remove
	store.Remove(canary.Token)

	// Should not detect after remove
	leaked = store.CheckLeaked(text)
	if len(leaked) != 0 {
		t.Error("should not detect canary after removal")
	}
}

func TestCanaryStore_MultipleTokens(t *testing.T) {
	store := NewCanaryStore()

	c1 := store.Generate("s1")
	c2 := store.Generate("s2")
	c3 := store.Generate("s3")

	// All tokens should be unique
	if c1.Token == c2.Token || c2.Token == c3.Token {
		t.Error("all canary tokens should be unique")
	}

	// Detect c1 and c3 but not c2
	text := "output: " + c1.Token + " and also " + c3.Token
	leaked := store.CheckLeaked(text)
	if len(leaked) != 2 {
		t.Errorf("expected 2 leaked tokens, got %d", len(leaked))
	}
}

func TestTruncateStr(t *testing.T) {
	// truncateStr truncates strings and may add "..." suffix
	short := truncateStr("hello", 10)
	if short != "hello" {
		t.Errorf("short string should not be truncated, got %q", short)
	}

	long := truncateStr("hello world this is very long", 10)
	if long == "hello world this is very long" {
		t.Error("long string should be truncated")
	}
	t.Logf("truncateStr result: %q", long)

	empty := truncateStr("", 5)
	if empty != "" {
		t.Errorf("empty string should return empty, got %q", empty)
	}
}
