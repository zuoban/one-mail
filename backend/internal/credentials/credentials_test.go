package credentials

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialEncryptionAuthenticatesAndPersistsKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "private", "credentials.key")
	if err := Init("", keyPath); err != nil {
		t.Fatal(err)
	}
	plain := "mailbox-password-你好"
	first, err := Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first == plain {
		t.Fatal("credentials require randomized ciphertext")
	}
	if err := Init("", keyPath); err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(first)
	if err != nil || got != plain {
		t.Fatalf("persisted key cannot recover credential: %v", err)
	}
	info, err := os.Stat(keyPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("key permissions must be private")
	}
	sealed, err := base64.StdEncoding.DecodeString(first[len(prefix):])
	if err != nil {
		t.Fatal(err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := Decrypt(prefix + base64.StdEncoding.EncodeToString(sealed)); err == nil {
		t.Fatal("tampered credential must not decrypt")
	}
	otherKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := Init(otherKey, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(first); err == nil {
		t.Fatal("wrong deployment key must not decrypt existing credentials")
	}
}

func TestCredentialKeyRejectsInvalidConfiguration(t *testing.T) {
	for _, key := range []string{"bad-base64", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if err := Init(key, ""); err == nil {
			t.Fatal("invalid deployment key accepted")
		}
	}
	keyPath := filepath.Join(t.TempDir(), "credentials.key")
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(make([]byte, 32))), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Init("", keyPath); err == nil {
		t.Fatal("world-readable key file accepted")
	}
}
