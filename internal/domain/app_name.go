package domain

import (
	"fmt"
	"strings"
)

type InvalidAppNameError struct {
	Name       string
	Suggestion string
}

func (e *InvalidAppNameError) Error() string {
	return fmt.Sprintf("app name %q is not a normalized DNS label; use %q", e.Name, e.Suggestion)
}

func ValidateAppName(name string) error {
	if IsDNSLabelSafe(name) {
		return nil
	}

	return &InvalidAppNameError{Name: name, Suggestion: SuggestAppName(name)}
}

func SuggestAppName(name string) string {
	var suggestion strings.Builder
	separatorPending := false
	for _, char := range strings.ToLower(strings.TrimSpace(name)) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			if separatorPending && suggestion.Len() > 0 && suggestion.Len() < 63 {
				suggestion.WriteByte('-')
			}
			separatorPending = false
			if suggestion.Len() < 63 {
				suggestion.WriteRune(char)
			}
			continue
		}
		separatorPending = suggestion.Len() > 0
	}

	value := strings.TrimRight(suggestion.String(), "-")
	if value == "" {
		return "app"
	}
	return value
}

func AppIsolationName(name string) string {
	name = strings.ToLower(name)
	var normalized strings.Builder
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			normalized.WriteRune(char)
			continue
		}
		normalized.WriteByte('-')
	}

	value := strings.Trim(normalized.String(), "-_")
	if value == "" {
		value = "app"
	}
	return "sshdock_" + value
}

func ValidateAppIsolation(name string, existingNames []string) error {
	isolationName := AppIsolationName(name)
	collision := ""
	for _, existingName := range existingNames {
		if existingName == name || AppIsolationName(existingName) != isolationName {
			continue
		}
		if collision == "" || existingName < collision {
			collision = existingName
		}
	}
	if collision == "" {
		return nil
	}
	return fmt.Errorf("app name %q conflicts with existing app %q because both use runtime identity %q; choose another app name", name, collision, isolationName)
}
