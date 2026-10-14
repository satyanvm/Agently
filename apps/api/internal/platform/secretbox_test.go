package platform

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

const testKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func withKey(t *testing.T, key string) {
	t.Helper()
	t.Setenv("AGENTLY_CREDENTIALS_KEY", key)
	cipherOnce = sync.Once{}
	t.Cleanup(func() { cipherOnce = sync.Once{} })
}

func TestSecretboxRoundTrip(t *testing.T) {
	withKey(t, testKey)
	stored, err := sealCredentialData(map[string]any{"token": "xoxb-123"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.(string), "xoxb-123") {
		t.Fatalf("sealed value leaks plaintext: %s", stored)
	}
	got, err := openCredentialData([]byte(stored.(string)))
	if err != nil || got["token"] != "xoxb-123" {
		t.Fatalf("open = %v, %v", got, err)
	}
}

func TestSecretboxReadsLegacyPlaintext(t *testing.T) {
	withKey(t, testKey)
	raw, _ := json.Marshal(map[string]any{"token": "plain"})
	got, err := openCredentialData(raw)
	if err != nil || got["token"] != "plain" {
		t.Fatalf("open = %v, %v", got, err)
	}
}

func TestSecretboxWithoutKey(t *testing.T) {
	withKey(t, "")
	stored, _ := sealCredentialData(map[string]any{"token": "plain"})
	if !strings.Contains(stored.(string), "plain") {
		t.Fatalf("expected plaintext without a key, got %s", stored)
	}
	if _, err := openCredentialData([]byte(`{"$enc":"v1:AAAA"}`)); err == nil {
		t.Fatal("expected an error opening a sealed value without a key")
	}
}

func TestSecretboxRejectsBadKey(t *testing.T) {
	withKey(t, "too-short")
	if _, err := CredentialsKeyConfigured(); err == nil {
		t.Fatal("expected malformed key to be rejected")
	}
}
