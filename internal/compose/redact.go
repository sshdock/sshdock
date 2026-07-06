package compose

import "strings"

func RedactValues(text string, values map[string]string) string {
	redacted := text
	for _, value := range values {
		if value == "" {
			continue
		}
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
