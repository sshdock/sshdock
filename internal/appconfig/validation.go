package appconfig

import (
	"fmt"
	"strings"

	"github.com/sshdock/sshdock/internal/store"
)

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
	ref.Scope = strings.TrimSpace(ref.Scope)
	if ref.AppID == "" {
		return ConfigRef{}, fmt.Errorf("app name is required")
	}
	key, err := validateRequiredKey(RequiredKey{Name: ref.Name, Scope: ref.Scope})
	if err != nil {
		return ConfigRef{}, err
	}
	ref.Name = key.Name
	ref.Scope = key.Scope
	return ref, nil
}

func storeRef(ref ConfigRef) store.AppConfigRef {
	return store.AppConfigRef{AppID: ref.AppID, Name: ref.Name, Scope: ref.Scope}
}
