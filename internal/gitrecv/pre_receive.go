package gitrecv

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

const mainRef = "refs/heads/main"

func ValidatePreReceive(input io.Reader) error {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 {
			return fmt.Errorf("pre-receive line must contain old sha, new sha, and ref")
		}
		if fields[2] != mainRef {
			return fmt.Errorf("unsupported destination %q: push to remote main with <source>:main", fields[2])
		}
		if isZeroObjectID(fields[1]) {
			return fmt.Errorf("cannot delete remote main: push a commit, branch, or tag to main instead")
		}
	}

	return scanner.Err()
}

func isZeroObjectID(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char != '0' {
			return false
		}
	}
	return true
}
