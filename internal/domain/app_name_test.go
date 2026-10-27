package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateAppNameAcceptsOnlyNormalizedDNSLabels(t *testing.T) {
	validNames := []string{
		"a",
		"0",
		"app",
		"my-app",
		"app1",
		"foo--bar",
		strings.Repeat("a", 63),
	}
	for _, name := range validNames {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAppName(name); err != nil {
				t.Fatalf("ValidateAppName(%q): %v", name, err)
			}
		})
	}
}

func TestValidateAppNameReturnsDeterministicSuggestionForInvalidInputs(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		suggestion string
	}{
		{name: "empty", input: "", suggestion: "app"},
		{name: "uppercase", input: "App", suggestion: "app"},
		{name: "underscore", input: "my_app", suggestion: "my-app"},
		{name: "dot", input: "my.app", suggestion: "my-app"},
		{name: "space", input: "my app", suggestion: "my-app"},
		{name: "leading hyphen", input: "-app", suggestion: "app"},
		{name: "trailing hyphen", input: "app-", suggestion: "app"},
		{name: "only hyphen", input: "-", suggestion: "app"},
		{name: "uppercase and space", input: "MY APP", suggestion: "my-app"},
		{name: "punctuation at boundaries", input: "-my.app_", suggestion: "my-app"},
		{name: "surrounding whitespace", input: " my-app ", suggestion: "my-app"},
		{name: "too long lowercase", input: strings.Repeat("a", 64), suggestion: strings.Repeat("a", 63)},
		{name: "too long uppercase", input: strings.Repeat("A", 64), suggestion: strings.Repeat("a", 63)},
		{name: "non ASCII", input: "應用", suggestion: "app"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			err := ValidateAppName(test.input)

			// Then
			var nameError *InvalidAppNameError
			if !errors.As(err, &nameError) {
				t.Fatalf("ValidateAppName(%q) error = %v, want *InvalidAppNameError", test.input, err)
			}
			if err := ValidateAppName(nameError.Suggestion); err != nil {
				t.Fatalf("suggestion %q is invalid: %v", nameError.Suggestion, err)
			}
			if nameError.Suggestion != test.suggestion {
				t.Fatalf("ValidateAppName(%q) suggestion = %q, want %q", test.input, nameError.Suggestion, test.suggestion)
			}
		})
	}
}

func TestValidateAppNameSuggestsNormalizedDNSLabelWhenInvalid(t *testing.T) {
	// Given
	name := "My_App"

	// When
	err := ValidateAppName(name)

	// Then
	if err == nil {
		t.Fatal("ValidateAppName error = nil, want invalid app name error")
	}
	var nameError *InvalidAppNameError
	if !errors.As(err, &nameError) {
		t.Fatalf("ValidateAppName error = %T, want *InvalidAppNameError", err)
	}
	if nameError.Suggestion != "my-app" {
		t.Fatalf("ValidateAppName suggestion = %q, want %q", nameError.Suggestion, "my-app")
	}
}

func TestAppHostUsesCanonicalAppNameBoundary(t *testing.T) {
	if host, err := AppHost("my-app", "example.com"); err != nil || host != "my-app.example.com" {
		t.Fatalf("AppHost = %q, %v", host, err)
	}
	host, err := AppHost("My_App", "example.com")
	var invalid *InvalidAppNameError
	if host != "" || !errors.As(err, &invalid) || invalid.Suggestion != "my-app" {
		t.Fatalf("AppHost invalid name = %q, %v", host, err)
	}
}
