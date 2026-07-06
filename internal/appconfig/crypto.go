package appconfig

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const currentKeyVersion = 1

type ConfigRef struct {
	AppID string
	Name  string
	Scope string
}

type Box struct {
	Ciphertext []byte
	Nonce      []byte
	KeyVersion int
}

func Encrypt(ref ConfigRef, key []byte, plaintext []byte) (Box, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return Box{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return Box{}, fmt.Errorf("generate config nonce: %w", err)
	}

	keyVersion := currentKeyVersion
	return Box{
		Ciphertext: aead.Seal(nil, nonce, plaintext, additionalData(ref, keyVersion)),
		Nonce:      nonce,
		KeyVersion: keyVersion,
	}, nil
}

func Decrypt(ref ConfigRef, key []byte, box Box) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(box.Nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("config nonce is %d bytes, want %d", len(box.Nonce), aead.NonceSize())
	}

	plaintext, err := aead.Open(nil, box.Nonce, box.Ciphertext, additionalData(ref, box.KeyVersion))
	if err != nil {
		return nil, fmt.Errorf("decrypt config value %s: authentication failed", ref.display())
	}
	return plaintext, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("config encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func additionalData(ref ConfigRef, keyVersion int) []byte {
	parts := []string{"sshdock-config-v1", ref.AppID, ref.Scope, ref.Name, strconv.Itoa(keyVersion)}
	return []byte(strings.Join(parts, "\x00"))
}

func (r ConfigRef) display() string {
	if r.Scope == "" {
		return r.AppID + "/" + r.Name
	}
	return r.AppID + "/" + r.Scope + "/" + r.Name
}
