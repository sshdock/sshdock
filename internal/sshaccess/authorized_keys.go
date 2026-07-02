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
		receiveCommand = "rhumbased git-receive"
	}

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
			`command="exec %s",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding,no-user-rc %s rhumbase-key:%s`+"\n",
			escapeAuthorizedKeysCommand(receiveCommand),
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

func escapeAuthorizedKeysCommand(command string) string {
	return strings.ReplaceAll(command, `"`, `\"`)
}
