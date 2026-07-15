package sshaccess

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Key struct {
	Name      string
	PublicKey string
	CreatedAt time.Time
}

func RenderAuthorizedKeys(keys []Key, receiveCommand string) string {
	if receiveCommand == "" {
		receiveCommand = "sshdockd git-receive"
	}
	return renderAuthorizedKeys(keys, receiveCommand, []string{
		"no-pty",
		"no-port-forwarding",
		"no-agent-forwarding",
		"no-X11-forwarding",
		"no-user-rc",
	})
}

func RenderOperatorAuthorizedKeys(keys []Key, operatorCommand string) string {
	if operatorCommand == "" {
		operatorCommand = "sshdockd operator"
	}
	return renderAuthorizedKeys(keys, operatorCommand, []string{
		"no-port-forwarding",
		"no-agent-forwarding",
		"no-X11-forwarding",
		"no-user-rc",
	})
}

func renderAuthorizedKeys(keys []Key, command string, options []string) string {
	sorted := append([]Key(nil), keys...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	var builder strings.Builder
	for _, key := range sorted {
		publicKey := strings.TrimSpace(key.PublicKey)
		if publicKey == "" {
			continue
		}
		fmt.Fprintf(
			&builder,
			`command="exec %s",%s %s sshdock-key:%s`+"\n",
			escapeAuthorizedKeysCommand(command),
			strings.Join(options, ","),
			publicKey,
			key.Name,
		)
	}

	return builder.String()
}

func WriteAuthorizedKeys(path string, keys []Key, receiveCommand string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	content := RenderAuthorizedKeys(keys, receiveCommand)
	return os.WriteFile(path, []byte(content), 0o600)
}

func WriteOperatorAuthorizedKeys(path string, keys []Key, operatorCommand string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	content := RenderOperatorAuthorizedKeys(keys, operatorCommand)
	return os.WriteFile(path, []byte(content), 0o600)
}

func escapeAuthorizedKeysCommand(command string) string {
	return strings.ReplaceAll(command, `"`, `\"`)
}
