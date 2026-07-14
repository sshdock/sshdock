package compose

import (
	"errors"
	"fmt"
	"os"

	"github.com/compose-spec/compose-go/v2/interpolation"
	"github.com/compose-spec/compose-go/v2/template"
	"gopkg.in/yaml.v3"
)

type composeModel map[string]any

func loadComposeModel(path string) (composeModel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var model composeModel
	if err := yaml.Unmarshal(data, &model); err != nil {
		return nil, fmt.Errorf("invalid YAML in %s: %w", path, err)
	}
	return model, nil
}

func loadInterpolatedComposeModel(path string, env map[string]string) (composeModel, error) {
	model, err := loadComposeModel(path)
	if err != nil {
		return nil, err
	}
	return interpolateComposeModel(path, model, env)
}

func interpolateComposeModel(path string, model composeModel, env map[string]string) (composeModel, error) {
	environment, err := interpolationEnvironment(path, env)
	if err != nil {
		return nil, err
	}
	plainModel := plainComposeValue(model).(map[string]any)
	interpolated, err := interpolation.Interpolate(plainModel, interpolation.Options{
		LookupValue: func(name string) (string, bool) {
			value, found := environment[name]
			return value, found
		},
	})
	if err != nil {
		message := fmt.Sprintf("interpolate Compose file %s: invalid variable expression", path)
		var missing *template.MissingRequiredError
		if errors.As(err, &missing) {
			message = fmt.Sprintf("interpolate Compose file %s: required variable %s is missing a value", path, missing.Variable)
		}
		return nil, redactedError{message: message, err: err}
	}
	return composeModel(interpolated), nil
}

func plainComposeValue(value any) any {
	switch value := value.(type) {
	case composeModel:
		plain := make(map[string]any, len(value))
		for key, child := range value {
			plain[key] = plainComposeValue(child)
		}
		return plain
	case map[string]any:
		plain := make(map[string]any, len(value))
		for key, child := range value {
			plain[key] = plainComposeValue(child)
		}
		return plain
	case []any:
		plain := make([]any, len(value))
		for index, child := range value {
			plain[index] = plainComposeValue(child)
		}
		return plain
	default:
		return value
	}
}

func modelServices(model composeModel) map[string]any {
	return modelMapping(model["services"])
}

func modelMapping(value any) map[string]any {
	switch mapping := value.(type) {
	case composeModel:
		return map[string]any(mapping)
	case map[string]any:
		return mapping
	case map[any]any:
		converted := make(map[string]any, len(mapping))
		for key, value := range mapping {
			name, ok := key.(string)
			if ok {
				converted[name] = value
			}
		}
		return converted
	default:
		return nil
	}
}

func modelString(value any) (string, bool) {
	text, ok := value.(string)
	return text, ok
}

func documentNode(document *yaml.Node) *yaml.Node {
	if document.Kind == yaml.DocumentNode && len(document.Content) == 1 {
		return document.Content[0]
	}
	return document
}
