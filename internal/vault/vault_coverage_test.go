package vault

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// --- NewEncryptor ---

func TestNewEncryptor_ValidKey(t *testing.T) {
	key := make([]byte, 32) // zero key is valid
	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor error: %v", err)
	}
	if enc == nil {
		t.Fatal("encryptor should not be nil")
	}
}

func TestNewEncryptor_InvalidKeyLength(t *testing.T) {
	_, err := NewEncryptor([]byte("short"))
	if err == nil {
		t.Error("should error with wrong key length")
	}
}

func TestNewEncryptor_16ByteKey(t *testing.T) {
	_, err := NewEncryptor(make([]byte, 16))
	if err == nil {
		t.Error("should error for 16-byte key (needs 32)")
	}
}

// --- Encrypt + Decrypt round-trip ---

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	enc, _ := NewEncryptor(key)

	original := "john.doe@example.com"
	ciphertext, err := enc.Encrypt(original)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}

	if ciphertext == original {
		t.Error("ciphertext should differ from plaintext")
	}

	decrypted, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}

	if decrypted != original {
		t.Errorf("expected %q, got %q", original, decrypted)
	}
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	key := make([]byte, 32)
	enc, _ := NewEncryptor(key)

	_, err := enc.Decrypt("not!valid!base64!")
	if err == nil {
		t.Error("should error on invalid base64")
	}
}

func TestDecrypt_TooShortCiphertext(t *testing.T) {
	key := make([]byte, 32)
	enc, _ := NewEncryptor(key)

	_, err := enc.Decrypt("YWJj") // "abc" in base64 — too short
	if err == nil {
		t.Error("should error on ciphertext shorter than nonce")
	}
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	enc, _ := NewEncryptor(key)

	ct, _ := enc.Encrypt("hello world")

	// Tamper with the ciphertext
	tampered := []byte(ct)
	if len(tampered) > 5 {
		tampered[5] ^= 0xFF
	}

	_, err := enc.Decrypt(string(tampered))
	if err == nil {
		t.Error("should error on tampered ciphertext")
	}
}

// --- Vault Lookup ---

func TestVault_Lookup_Success(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	v := NewWithClient(client)

	ctx := context.Background()

	// Store a value first
	err := v.Store(ctx, "session-1", map[string]string{"<<TOKEN_1>>": "secret@email.com"})
	if err != nil {
		t.Fatalf("Store error: %v", err)
	}

	// Lookup
	val, err := v.Lookup(ctx, "session-1", "<<TOKEN_1>>")
	if err != nil {
		t.Fatalf("Lookup error: %v", err)
	}
	if val != "secret@email.com" {
		t.Errorf("expected secret@email.com, got %s", val)
	}
}

func TestVault_Lookup_NotFound(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	v := NewWithClient(client)

	ctx := context.Background()
	_, err := v.Lookup(ctx, "no-session", "<<MISSING>>")
	if err == nil {
		t.Error("should error when token not found")
	}
}

func TestVault_Close(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	v := NewWithClient(client)

	err := v.Close()
	if err != nil {
		t.Fatalf("Close error: %v", err)
	}
}

func TestVault_LookupAll_Empty(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	v := NewWithClient(client)

	ctx := context.Background()
	result, err := v.LookupAll(ctx, "empty-session")
	if err != nil {
		t.Fatalf("LookupAll error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results, got %d", len(result))
	}
}

func TestVault_StoreAndLookupAll(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	v := NewWithClient(client)

	ctx := context.Background()

	mappings := map[string]string{
		"<<EMAIL>>": "user@example.com",
		"<<PHONE>>": "+1-555-0100",
	}
	v.Store(ctx, "multi-session", mappings)

	all, err := v.LookupAll(ctx, "multi-session")
	if err != nil {
		t.Fatalf("LookupAll error: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 mappings, got %d", len(all))
	}
}
