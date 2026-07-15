package compose

import (
	"sort"
	"strings"
)

func RedactValues(text string, values map[string]string) string {
	redacted := text
	unique := map[string]bool{}
	for _, value := range values {
		if value != "" {
			unique[value] = true
		}
	}
	ordered := make([]string, 0, len(unique))
	for value := range unique {
		ordered = append(ordered, value)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if len(ordered[i]) == len(ordered[j]) {
			return ordered[i] < ordered[j]
		}
		return len(ordered[i]) > len(ordered[j])
	})
	for _, value := range ordered {
		redacted = strings.ReplaceAll(redacted, value, "<redacted>")
	}
	return redacted
}

func RedactError(err error, values map[string]string) error {
	if err == nil {
		return nil
	}
	message := RedactValues(err.Error(), values)
	if message == err.Error() {
		return err
	}
	return redactedError{message: message, err: err}
}

type redactedError struct {
	message string
	err     error
}

func (e redactedError) Error() string {
	return e.message
}

func (e redactedError) Unwrap() error {
	return e.err
}
