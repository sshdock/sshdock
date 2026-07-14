package compose

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type ValidationResult struct {
	Services []string
}

func ValidateFile(path string) (ValidationResult, error) {
	return ValidateFileWithEnv(path, nil)
}

func ValidateFileWithEnv(path string, env map[string]string) (ValidationResult, error) {
	rawModel, err := loadComposeModel(path)
	if err != nil {
		return ValidationResult{}, err
	}
	model, err := interpolateComposeModel(path, rawModel, env)
	if err != nil {
		return ValidationResult{}, err
	}
	if err := validateExternalFilePolicy(path, model, rawModel); err != nil {
		return ValidationResult{}, err
	}

	servicesModel := modelServices(model)
	services := make([]string, 0, len(servicesModel))
	for serviceName := range servicesModel {
		services = append(services, serviceName)
	}
	sort.Strings(services)
	return ValidationResult{Services: services}, nil
}

func validateExternalFilePolicy(composePath string, model composeModel, rawModel composeModel) error {
	if _, found := model["include"]; found {
		rawInclude := rawModel["include"]
		files := strings.Join(scalarValues(rawInclude), ", ")
		if files == "" {
			files = "the configured file"
		}
		return fmt.Errorf("top-level include references %s; external Compose files are not supported; keep the app in one root Compose file", files)
	}

	for serviceName, serviceValue := range modelServices(model) {
		service := modelMapping(serviceValue)
		extends := modelMapping(service["extends"])
		if extends == nil {
			continue
		}
		file, found := extends["file"]
		if found && !referencesSelectedComposeFile(composePath, file) {
			rawService := modelMapping(modelServices(rawModel)[serviceName])
			rawFile := modelMapping(rawService["extends"])["file"]
			if rawFile == nil {
				rawFile = "the configured value"
			}
			return fmt.Errorf("services.%s.extends.file references external Compose file %q; external Compose files are not supported; define the base service in the selected root Compose file", serviceName, fmt.Sprint(rawFile))
		}
	}

	return nil
}

func referencesSelectedComposeFile(composePath string, value any) bool {
	file, ok := modelString(value)
	if !ok {
		return false
	}
	selected, err := filepath.Abs(composePath)
	if err != nil {
		return false
	}
	referenced := file
	if !filepath.IsAbs(referenced) {
		referenced = filepath.Join(filepath.Dir(selected), referenced)
	}
	referenced, err = filepath.Abs(referenced)
	return err == nil && filepath.Clean(referenced) == filepath.Clean(selected)
}

func scalarValues(value any) []string {
	switch value := value.(type) {
	case string:
		return []string{value}
	case []any:
		var values []string
		for _, child := range value {
			values = append(values, scalarValues(child)...)
		}
		return values
	case composeModel:
		return scalarValues(map[string]any(value))
	case map[string]any:
		var values []string
		for _, child := range value {
			values = append(values, scalarValues(child)...)
		}
		return values
	case map[any]any:
		var values []string
		for _, child := range value {
			values = append(values, scalarValues(child)...)
		}
		return values
	case nil:
		return nil
	default:
		return []string{fmt.Sprint(value)}
	}
}
