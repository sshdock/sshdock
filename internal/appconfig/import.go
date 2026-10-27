package appconfig

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ImportLines applies KEY=VALUE lines in order and returns the completed count.
func ImportLines(input io.Reader, apply func(name string, value []byte) error) (int, error) {
	if input == nil {
		input = strings.NewReader("")
	}
	scanner := bufio.NewScanner(input)
	lineNumber, count := 0, 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return count, fmt.Errorf("config import line %d must be KEY=VALUE", lineNumber)
		}
		if err := apply(strings.TrimSpace(name), []byte(value)); err != nil {
			return count, fmt.Errorf("config import line %d: %w", lineNumber, err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("read config import after line %d: %w", lineNumber, err)
	}
	return count, nil
}
