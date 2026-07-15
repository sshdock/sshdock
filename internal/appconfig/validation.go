package appconfig

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/sshdock/sshdock/internal/store"
)

var configKeyPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

func validateConfigMutationRef(ref ConfigRef) (ConfigRef, error) {
	ref, err := validateConfigRef(ref)
	if err != nil {
		return ConfigRef{}, err
	}
	if isReservedConfigName(ref.Name) {
		return ConfigRef{}, reservedConfigNameError(ref.Name)
	}
	return ref, nil
}

func isReservedConfigName(name string) bool {
	if name == "PATH" || name == "HOME" {
		return true
	}
	for _, prefix := range []string{"SSHDOCK_", "COMPOSE_", "DOCKER_", "SSH_", "LD_", "BUILDKIT_", "BUILDX_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func reservedConfigNameError(name string) error {
	return fmt.Errorf("config key %s is reserved for SSHDock operations", name)
}

func validateConfigRef(ref ConfigRef) (ConfigRef, error) {
	ref.AppID = strings.TrimSpace(ref.AppID)
	ref.Name = strings.TrimSpace(ref.Name)
	if ref.AppID == "" {
		return ConfigRef{}, fmt.Errorf("app name is required")
	}
	if !configKeyPattern.MatchString(ref.Name) {
		return ConfigRef{}, fmt.Errorf("invalid config key %q: use uppercase letters, digits, and underscores", ref.Name)
	}
	return ref, nil
}

func storeRef(ref ConfigRef) store.AppConfigRef {
	return store.AppConfigRef{AppID: ref.AppID, Name: ref.Name}
}
