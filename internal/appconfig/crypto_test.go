package appconfig

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
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

func TestFlatCiphertextUsesExistingLiteralAuthenticationData(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{3}, aead.NonceSize())
	// This literal is the on-disk flat format, independent of additionalData.
	aad := []byte("sshdock-config-v1\x00my-app\x00\x00API_TOKEN\x001")
	box := Box{Ciphertext: aead.Seal(nil, nonce, []byte("existing-flat-secret"), aad), Nonce: nonce, KeyVersion: 1}
	ref := ConfigRef{AppID: "my-app", Name: "API_TOKEN"}
	plaintext, err := Decrypt(ref, key, box)
	if err != nil || string(plaintext) != "existing-flat-secret" {
		t.Fatalf("Decrypt existing flat value = %q, %v", plaintext, err)
	}
	created, err := Encrypt(ref, key, []byte("new-flat-secret"))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err = aead.Open(nil, created.Nonce, created.Ciphertext, aad)
	if err != nil || string(plaintext) != "new-flat-secret" {
		t.Fatalf("new ciphertext with literal AAD = %q, %v", plaintext, err)
	}
}

func TestAESGCMRejectsTamperingAndChangedBinding(t *testing.T) {
	ref := ConfigRef{AppID: "my-app", Name: "TOKEN"}
	key := bytes.Repeat([]byte{7}, 32)
	box, err := Encrypt(ref, key, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app", "name", "version", "key", "ciphertext", "nonce", "short nonce", "short key"} {
		t.Run(name, func(t *testing.T) {
			changedRef, changedKey := ref, append([]byte(nil), key...)
			changed := Box{Ciphertext: append([]byte(nil), box.Ciphertext...), Nonce: append([]byte(nil), box.Nonce...), KeyVersion: box.KeyVersion}
			switch name {
			case "app":
				changedRef.AppID = "other-app"
			case "name":
				changedRef.Name = "OTHER_TOKEN"
			case "version":
				changed.KeyVersion++
			case "key":
				changedKey[0] ^= 1
			case "ciphertext":
				changed.Ciphertext[0] ^= 1
			case "nonce":
				changed.Nonce[0] ^= 1
			case "short nonce":
				changed.Nonce = changed.Nonce[:1]
			case "short key":
				changedKey = changedKey[:1]
			}
			if _, err := Decrypt(changedRef, changedKey, changed); err == nil {
				t.Fatal("Decrypt accepted changed ciphertext or binding")
			}
		})
	}
	second, err := Encrypt(ref, key, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(box.Nonce, second.Nonce) || bytes.Equal(box.Ciphertext, second.Ciphertext) {
		t.Fatal("repeated encryption reused a nonce or ciphertext")
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
