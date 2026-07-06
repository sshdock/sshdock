package appconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const ManifestName = ".sshdock.yml"

var (
	configKeyPattern   = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	configScopePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

type Manifest struct {
	Required []RequiredKey
}

type RequiredKey struct {
	Name  string
	Scope string
}

func LoadManifest(projectDir string) (Manifest, error) {
	path := filepath.Join(projectDir, ManifestName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, nil
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("read %s: %w", ManifestName, err)
	}

	var raw struct {
		Config struct {
			Required []yaml.Node `yaml:"required"`
		} `yaml:"config"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", ManifestName, err)
	}

	required := make([]RequiredKey, 0, len(raw.Config.Required))
	seen := map[string]bool{}
	for _, node := range raw.Config.Required {
		key, err := parseRequiredKey(node)
		if err != nil {
			return Manifest{}, err
		}
		id := key.identity()
		if seen[id] {
			return Manifest{}, fmt.Errorf("%s declares duplicate config key %s", ManifestName, key.display())
		}
		seen[id] = true
		required = append(required, key)
	}
	sort.Slice(required, func(i, j int) bool {
		if required[i].Scope == required[j].Scope {
			return required[i].Name < required[j].Name
		}
		return required[i].Scope < required[j].Scope
	})

	return Manifest{Required: required}, nil
}

func parseRequiredKey(node yaml.Node) (RequiredKey, error) {
	switch node.Kind {
	case yaml.ScalarNode:
		return validateRequiredKey(RequiredKey{Name: strings.TrimSpace(node.Value)})
	case yaml.MappingNode:
		var key RequiredKey
		for i := 0; i < len(node.Content); i += 2 {
			switch node.Content[i].Value {
			case "name":
				key.Name = strings.TrimSpace(node.Content[i+1].Value)
			case "scope":
				key.Scope = strings.TrimSpace(node.Content[i+1].Value)
			}
		}
		return validateRequiredKey(key)
	default:
		return RequiredKey{}, fmt.Errorf("%s config.required entries must be strings or mappings", ManifestName)
	}
}

func validateRequiredKey(key RequiredKey) (RequiredKey, error) {
	if !configKeyPattern.MatchString(key.Name) {
		return RequiredKey{}, fmt.Errorf("invalid config key %q in %s: use uppercase letters, digits, and underscores", key.Name, ManifestName)
	}
	if key.Scope != "" && !configScopePattern.MatchString(key.Scope) {
		return RequiredKey{}, fmt.Errorf("invalid config scope %q in %s", key.Scope, ManifestName)
	}
	return key, nil
}

func (k RequiredKey) identity() string {
	return k.Scope + "\x00" + k.Name
}

func (k RequiredKey) display() string {
	if k.Scope == "" {
		return k.Name
	}
	return k.Scope + "/" + k.Name
}
