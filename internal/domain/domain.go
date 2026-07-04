package domain

import (
	"fmt"
	"strings"
)

func NormalizeBaseDomain(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.TrimSuffix(normalized, ".")
	if normalized == "" {
		return "", fmt.Errorf("server base domain is required")
	}
	if strings.Contains(normalized, "://") || strings.ContainsAny(normalized, "/:") {
		return "", fmt.Errorf("server base domain must be a DNS name, not a URL")
	}
	if strings.HasPrefix(normalized, "*.") {
		return "", fmt.Errorf("server base domain should omit the wildcard prefix")
	}
	if !isDNSName(normalized) || !strings.Contains(normalized, ".") {
		return "", fmt.Errorf("server base domain %q is not a valid DNS name", value)
	}
	return normalized, nil
}

func ControlHost(baseDomain string) string {
	return "rhumbase." + strings.TrimSpace(baseDomain)
}

func AppHost(appName string, baseDomain string) (string, error) {
	if !IsDNSLabelSafe(appName) {
		return "", fmt.Errorf("app name %q is not a DNS label; use lowercase letters, numbers, and hyphens for automatic routing", appName)
	}
	baseDomain = strings.TrimSpace(baseDomain)
	if baseDomain == "" {
		return "", fmt.Errorf("server base domain is required")
	}
	return appName + "." + baseDomain, nil
}

func IsDNSLabelSafe(value string) bool {
	if len(value) == 0 || len(value) > 63 {
		return false
	}
	for i, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
			if i == 0 || i == len(value)-1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func isDNSName(value string) bool {
	labels := strings.Split(value, ".")
	for _, label := range labels {
		if !IsDNSLabelSafe(label) {
			return false
		}
	}
	return true
}
