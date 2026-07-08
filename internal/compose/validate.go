package compose

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

type ValidationResult struct {
	Services []string
}

func ValidateFile(path string) (ValidationResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ValidationResult{}, err
	}

	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return ValidationResult{}, fmt.Errorf("invalid YAML in %s: %w", path, err)
	}

	root := documentNode(&document)
	if root == nil || root.Kind != yaml.MappingNode {
		return ValidationResult{}, fmt.Errorf("compose file %s must be a mapping with services", path)
	}

	servicesNode, err := validateTopLevel(root)
	if err != nil {
		return ValidationResult{}, err
	}
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode || len(servicesNode.Content) == 0 {
		return ValidationResult{}, fmt.Errorf("compose file must define at least one service")
	}

	services, err := validateServices(servicesNode)
	if err != nil {
		return ValidationResult{}, err
	}

	sort.Strings(services)
	return ValidationResult{Services: services}, nil
}

func validateTopLevel(root *yaml.Node) (*yaml.Node, error) {
	var servicesNode *yaml.Node
	supported := map[string]bool{
		"services": true,
		"volumes":  true,
	}

	for i := 0; i < len(root.Content); i += 2 {
		key := root.Content[i].Value
		value := root.Content[i+1]
		if !supported[key] {
			return nil, fmt.Errorf("unsupported top-level field %q; see docs/COMPOSE_SUPPORT.md for SSHDock's supported Compose subset", key)
		}
		if key == "services" {
			servicesNode = value
		}
	}

	return servicesNode, nil
}

func validateServices(servicesNode *yaml.Node) ([]string, error) {
	supported := map[string]bool{
		"build":       true,
		"image":       true,
		"environment": true,
		"env_file":    true,
		"depends_on":  true,
		"volumes":     true,
		"ports":       true,
		"expose":      true,
		"healthcheck": true,
		"restart":     true,
	}

	services := make([]string, 0, len(servicesNode.Content)/2)
	for i := 0; i < len(servicesNode.Content); i += 2 {
		serviceName := servicesNode.Content[i].Value
		serviceNode := servicesNode.Content[i+1]
		if serviceNode.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("service %q must be a mapping", serviceName)
		}

		services = append(services, serviceName)
		for j := 0; j < len(serviceNode.Content); j += 2 {
			field := serviceNode.Content[j].Value
			if !supported[field] {
				return nil, fmt.Errorf("unsupported field %q in service %s.%s; see docs/COMPOSE_SUPPORT.md for SSHDock's supported Compose subset", field, serviceName, field)
			}
		}
	}

	return services, nil
}

func documentNode(document *yaml.Node) *yaml.Node {
	if document.Kind == yaml.DocumentNode && len(document.Content) == 1 {
		return document.Content[0]
	}
	return document
}
