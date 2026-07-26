package compose

import (
	"io"
	"strings"
)

type RedactingWriter struct {
	target  io.Writer
	values  map[string]string
	pending string
}

func NewRedactingWriter(target io.Writer, values map[string]string) *RedactingWriter {
	return &RedactingWriter{target: target, values: values}
}

func (w *RedactingWriter) Write(p []byte) (int, error) {
	combined := w.pending + string(p)
	pendingLength := pendingSecretPrefixLength(combined, w.values)
	emitLength := len(combined) - pendingLength
	w.pending = combined[emitLength:]
	if w.target == nil {
		return len(p), nil
	}
	_, err := io.WriteString(w.target, RedactValues(combined[:emitLength], w.values))
	return len(p), err
}

func (w *RedactingWriter) Flush() error {
	if w.target == nil || w.pending == "" {
		w.pending = ""
		return nil
	}
	_, err := io.WriteString(w.target, RedactValues(w.pending, w.values))
	w.pending = ""
	return err
}

func pendingSecretPrefixLength(text string, values map[string]string) int {
	longest := 0
	for _, value := range values {
		limit := len(value) - 1
		if limit > len(text) {
			limit = len(text)
		}
		for length := limit; length > longest; length-- {
			if strings.HasSuffix(text, value[:length]) {
				longest = length
				break
			}
		}
	}
	return longest
}
