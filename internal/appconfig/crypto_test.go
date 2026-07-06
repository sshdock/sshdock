package appconfig

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAESGCMEncryptsAndDecryptsWithMetadataBinding(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	box, err := Encrypt(ConfigRef{AppID: "my-app", Name: "DATABASE_URL"}, key, []byte("postgres://secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(box.Ciphertext, []byte("secret")) {
		t.Fatalf("ciphertext contains plaintext: %q", box.Ciphertext)
	}

	plaintext, err := Decrypt(ConfigRef{AppID: "my-app", Name: "DATABASE_URL"}, key, box)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(plaintext) != "postgres://secret" {
		t.Fatalf("plaintext = %q", plaintext)
	}

	if _, err := Decrypt(ConfigRef{AppID: "other-app", Name: "DATABASE_URL"}, key, box); err == nil {
		t.Fatal("Decrypt with copied app metadata error = nil, want authentication failure")
	}
}

func TestLoadOrCreateHostKeyCreatesStrictRawKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "config.key")

	key, err := LoadOrCreateHostKey(path)
	if err != nil {
		t.Fatalf("LoadOrCreateHostKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}

	again, err := LoadOrCreateHostKey(path)
	if err != nil {
		t.Fatalf("LoadOrCreateHostKey existing: %v", err)
	}
	if !bytes.Equal(again, key) {
		t.Fatal("existing key changed")
	}
}

func TestLoadOrCreateHostKeyRejectsWrongSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.key")
	if err := os.WriteFile(path, []byte("too-short"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	_, err := LoadOrCreateHostKey(path)
	if err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("LoadOrCreateHostKey error = %v, want size error", err)
	}
}
