package gitrecv

import (
	"fmt"
	"strings"
)

func ParsePostReceiveLine(line string) (string, error) {
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return "", fmt.Errorf("post-receive line must contain old sha, new sha, and ref")
	}
	if fields[2] != mainRef {
		return "", fmt.Errorf("unsupported destination %q: only remote %s is deployable", fields[2], mainRef)
	}
	return fields[1], nil
}
