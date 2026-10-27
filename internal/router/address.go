package router

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/sshdock/sshdock/internal/domain"
)

func canonicalAddress(value string) (string, bool, error) {
	invalid := func() (string, bool, error) {
		return "", false, fmt.Errorf("domain %q must be a hostname or an http:// or https:// site address without paths, credentials, or whitespace", value)
	}
	if value == "" || strings.ContainsAny(value, "?#") {
		return invalid()
	}
	for _, char := range value {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return invalid()
		}
	}
	explicitScheme := strings.Contains(value, "://")
	input := value
	if !explicitScheme {
		input = "https://" + value
	}
	parsed, err := url.Parse(input)
	if err != nil || parsed.User != nil || parsed.Path != "" || parsed.Opaque != "" || strings.HasSuffix(parsed.Host, ":") {
		return invalid()
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return invalid()
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" || len(host) > 253 {
		return invalid()
	}
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	} else {
		for _, label := range strings.Split(strings.TrimPrefix(host, "*."), ".") {
			if !domain.IsDNSLabelSafe(label) {
				return invalid()
			}
		}
	}
	port := 443
	if scheme == "http" {
		port = 80
	}
	if portText := parsed.Port(); portText != "" {
		port, err = strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return invalid()
		}
		if !explicitScheme && port == 80 {
			scheme = "http"
		}
	}
	address := host
	if strings.Contains(host, ":") {
		address = "[" + host + "]"
	}
	if (scheme == "http" && port != 80) || (scheme == "https" && port != 443) {
		address = net.JoinHostPort(host, strconv.Itoa(port))
	}
	if scheme == "http" || port != 443 {
		address = scheme + "://" + address
	}
	return address, scheme == "https", nil
}

func publicAddressKey(value string) (string, error) {
	address, https, err := canonicalAddress(value)
	if err != nil {
		return "", err
	}
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return "", err
	}
	port := parsed.Port()
	if port == "" {
		port = "80"
		if https {
			port = "443"
		}
	}
	return net.JoinHostPort(parsed.Hostname(), port), nil
}

func validateTarget(serviceName string, port int) error {
	if serviceName == "" {
		return fmt.Errorf("route service name is required")
	}
	for _, char := range serviceName {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return fmt.Errorf("route service name cannot contain whitespace or control characters")
		}
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}
