package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
)

func interpolationEnvironment(composePath string, extra map[string]string) (map[string]string, error) {
	environment := make(map[string]string)
	for _, entry := range os.Environ() {
		name, value, found := strings.Cut(entry, "=")
		if found {
			environment[name] = value
		}
	}
	for name, value := range extra {
		environment[name] = value
	}
	projectDir := filepath.Dir(composePath)
	environment["PWD"] = projectDir

	disabled, _ := strconv.ParseBool(environment["COMPOSE_DISABLE_ENV_FILE"])
	if disabled {
		return environment, nil
	}
	filenames := []string{filepath.Join(projectDir, ".env")}
	customFiles, custom := environment["COMPOSE_ENV_FILES"]
	if custom {
		filenames = nil
		for _, filename := range strings.Split(customFiles, ",") {
			filename = strings.TrimSpace(filename)
			if filename == "" {
				continue
			}
			if !filepath.IsAbs(filename) {
				filename = filepath.Join(projectDir, filename)
			}
			filenames = append(filenames, filename)
		}
	}

	fileValues := make(map[string]string)
	lookup := func(name string) (string, bool) {
		if value, found := environment[name]; found {
			return value, true
		}
		value, found := fileValues[name]
		return value, found
	}
	for _, filename := range filenames {
		values, err := dotenv.ReadFile(filename, lookup)
		if err != nil {
			if !custom && os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read Compose environment file %s: %w", filename, err)
		}
		for name, value := range values {
			fileValues[name] = value
		}
	}
	for name, value := range fileValues {
		if _, found := environment[name]; !found {
			environment[name] = value
		}
	}
	return environment, nil
}
