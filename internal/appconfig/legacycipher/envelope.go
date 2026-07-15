package legacycipher

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

var prefix = []byte("sshdock-legacy-scope-v1\x00")

func Wrap(scope string, ciphertext []byte) ([]byte, error) {
	if uint64(len(scope)) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("legacy config scope is too long")
	}

	wrapped := make([]byte, len(prefix)+4+len(scope)+len(ciphertext))
	copy(wrapped, prefix)
	binary.BigEndian.PutUint32(wrapped[len(prefix):], uint32(len(scope)))
	copy(wrapped[len(prefix)+4:], scope)
	copy(wrapped[len(prefix)+4+len(scope):], ciphertext)
	return wrapped, nil
}

func Unwrap(payload []byte) (string, []byte, bool, error) {
	if !bytes.HasPrefix(payload, prefix) {
		return "", payload, false, nil
	}
	if len(payload) < len(prefix)+4 {
		return "", nil, true, fmt.Errorf("legacy config ciphertext envelope is truncated")
	}

	scopeLength := uint64(binary.BigEndian.Uint32(payload[len(prefix):]))
	headerLength := uint64(len(prefix) + 4)
	if scopeLength > uint64(len(payload))-headerLength {
		return "", nil, true, fmt.Errorf("legacy config ciphertext envelope has invalid scope length")
	}
	ciphertextOffset := int(headerLength + scopeLength)
	return string(payload[len(prefix)+4 : ciphertextOffset]), payload[ciphertextOffset:], true, nil
}
