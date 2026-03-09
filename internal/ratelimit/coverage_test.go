package ratelimit

import (
	"testing"
	"time"
)

func TestRetryAfter_NoWindow(t *testing.T) {
	l := New(Config{RequestsPerMinute: 10, WindowSize: time.Minute, CleanupInterval: time.Minute})
	retry := l.RetryAfter("unknown-key")
	if retry != 0 {
		t.Errorf("expected 0 for unknown key, got %d", retry)
	}
}

func TestRetryAfter_ActiveWindow(t *testing.T) {
	l := New(Config{RequestsPerMinute: 2, WindowSize: time.Minute, CleanupInterval: time.Minute})

	l.Allow("test-key")
	l.Allow("test-key")

	retry := l.RetryAfter("test-key")
	if retry <= 0 {
		t.Errorf("expected positive retry-after, got %d", retry)
	}
	if retry > 61 {
		t.Errorf("retry-after too large: %d", retry)
	}
}

func TestRetryAfter_ExpiredWindow(t *testing.T) {
	l := New(Config{RequestsPerMinute: 2, WindowSize: 10 * time.Millisecond, CleanupInterval: time.Minute})

	l.Allow("expire-key")
	l.Allow("expire-key")

	time.Sleep(20 * time.Millisecond)

	retry := l.RetryAfter("expire-key")
	if retry != 0 {
		t.Errorf("expected 0 after window expiry, got %d", retry)
	}
}

func TestLimiter_AllowAfterWindowReset(t *testing.T) {
	l := New(Config{RequestsPerMinute: 1, WindowSize: 10 * time.Millisecond, CleanupInterval: time.Minute})

	if !l.Allow("reset-key") {
		t.Error("first request should be allowed")
	}
	if l.Allow("reset-key") {
		t.Error("second request should be rejected")
	}

	time.Sleep(20 * time.Millisecond)

	if !l.Allow("reset-key") {
		t.Error("should be allowed after window reset")
	}
}

func TestLimiter_MultipleKeys(t *testing.T) {
	l := New(Config{RequestsPerMinute: 1, WindowSize: time.Minute, CleanupInterval: time.Minute})

	if !l.Allow("key-a") {
		t.Error("key-a first should be allowed")
	}
	if !l.Allow("key-b") {
		t.Error("key-b first should be allowed (separate window)")
	}
	if l.Allow("key-a") {
		t.Error("key-a second should be rejected")
	}
	if l.Allow("key-b") {
		t.Error("key-b second should be rejected")
	}
}

func TestLimiter_HighLimit(t *testing.T) {
	l := New(Config{RequestsPerMinute: 100, WindowSize: time.Minute, CleanupInterval: time.Minute})
	for i := 0; i < 100; i++ {
		if !l.Allow("bulk-key") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if l.Allow("bulk-key") {
		t.Error("request 101 should be rejected")
	}
}
