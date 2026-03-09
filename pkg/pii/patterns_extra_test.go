package pii

import "testing"

func TestLuhnCheck(t *testing.T) {
	tests := []struct {
		name   string
		number string
		want   bool
	}{
		{"valid visa", "4111111111111111", true},
		{"valid mastercard", "5500000000000004", true},
		{"valid amex", "378282246310005", true},
		{"invalid", "1234567890123456", false},
		{"too short", "0000000000", false},
		{"single digit", "0", false},
		{"empty", "", false},
		{"with spaces (raw)", "4111 1111 1111 1111", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LuhnCheck(tt.number)
			if got != tt.want {
				t.Errorf("LuhnCheck(%q) = %v, want %v", tt.number, got, tt.want)
			}
		})
	}
}

func TestAllPatterns(t *testing.T) {
	all := AllPatterns()

	// AllPatterns returns a combined, possibly deduplicated set
	if len(all) == 0 {
		t.Fatal("AllPatterns() returned empty")
	}

	// Should have at least as many as the largest single group
	vn := VietnamPatterns()
	if len(all) < len(vn) {
		t.Errorf("AllPatterns() (%d) should be >= VietnamPatterns() (%d)", len(all), len(vn))
	}

	// Each pattern should have non-nil regex
	for i, p := range all {
		if p.Regex == nil {
			t.Errorf("AllPatterns[%d] (%s) has nil Regex", i, p.Label)
		}
	}
}

func TestVietnamPatterns_Match(t *testing.T) {
	patterns := VietnamPatterns()
	tests := []struct {
		input    string
		category Category
	}{
		{"012345678901", CatCCCD},
		{"0912345678", CatPhone},
		{"user@example.com", CatEmail},
	}

	for _, tt := range tests {
		found := false
		for _, p := range patterns {
			if p.Category == tt.category && p.Regex.MatchString(tt.input) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no %s pattern matched %q", tt.category, tt.input)
		}
	}
}

func TestSecretPatterns_Match(t *testing.T) {
	patterns := SecretPatterns()
	tests := []struct {
		input    string
		category Category
	}{
		{"sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDEF", CatAPIKeyOpenAI},
		{"sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab", CatAPIKeyAnthropic},
		{"AIzaSyABCDEFGHIJKLMNOPQRSTUVWXYZ1234567", CatAPIKeyGoogle},
	}

	for _, tt := range tests {
		found := false
		for _, p := range patterns {
			if p.Category == tt.category && p.Regex.MatchString(tt.input) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no %s pattern matched %q", tt.category, tt.input)
		}
	}
}

func TestPartialMask_EdgeCases(t *testing.T) {
	// Empty string
	got := PartialMask("")
	if got != "" {
		t.Errorf("PartialMask(\"\") should return \"\", got %q", got)
	}

	// Single char
	got = PartialMask("x")
	if len(got) != 1 {
		t.Errorf("PartialMask(\"x\") should return 1 char, got %d", len(got))
	}
}
