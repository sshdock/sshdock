package main

import (
	"fmt"
	"io"
	"strings"
	"unicode"
)

func operatorOriginalCommandArgs(command string) ([]string, error) {
	args, err := splitSSHOriginalCommand(command)
	if err != nil {
		return nil, fmt.Errorf("parse SSH command: %w", err)
	}
	if !operatorCommandAllowed(args) {
		return nil, fmt.Errorf("command is not available over SSH; run `ssh sshdock@<host> help` for supported commands")
	}
	return args, nil
}

func splitSSHOriginalCommand(command string) ([]string, error) {
	const (
		unquoted = iota
		singleQuoted
		doubleQuoted
	)

	var args []string
	var word strings.Builder
	state := unquoted
	escaped := false
	wordStarted := false

	flush := func() {
		args = append(args, word.String())
		word.Reset()
		wordStarted = false
	}

	for _, current := range command {
		if escaped {
			word.WriteRune(current)
			wordStarted = true
			escaped = false
			continue
		}

		switch state {
		case unquoted:
			switch {
			case current == '\\':
				escaped = true
				wordStarted = true
			case current == '\'':
				state = singleQuoted
				wordStarted = true
			case current == '"':
				state = doubleQuoted
				wordStarted = true
			case unicode.IsSpace(current):
				if wordStarted {
					flush()
				}
			default:
				word.WriteRune(current)
				wordStarted = true
			}
		case singleQuoted:
			if current == '\'' {
				state = unquoted
			} else {
				word.WriteRune(current)
			}
		case doubleQuoted:
			switch current {
			case '"':
				state = unquoted
			case '\\':
				escaped = true
			default:
				word.WriteRune(current)
			}
		}
	}

	if escaped {
		return nil, fmt.Errorf("unfinished escape")
	}
	if state != unquoted {
		return nil, fmt.Errorf("unterminated quote")
	}
	if wordStarted {
		flush()
	}
	return args, nil
}

func operatorCommandAllowed(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if len(args) == 1 {
		return args[0] == "help" || args[0] == "-h" || args[0] == "--help" || args[0] == "version"
	}
	if args[0] == "help" {
		return len(args) == 2 && operatorHelpTopicAllowed(args[1])
	}
	if len(args) == 2 && (args[1] == "-h" || args[1] == "--help") {
		return operatorHelpTopicAllowed(args[0])
	}

	switch args[0] {
	case "apps":
		return len(args) == 2 && args[1] == "list" ||
			len(args) == 3 && (args[1] == "info" || args[1] == "health")
	case "config":
		return len(args) == 3 && (args[1] == "import" || args[1] == "list" || args[1] == "keys") ||
			len(args) == 4 && (args[1] == "set" || args[1] == "get" || args[1] == "unset")
	case "domains":
		return len(args) == 3 && (args[1] == "list" || args[1] == "check")
	case "deployments", "events", "releases":
		return len(args) == 3 && args[1] == "list"
	case "logs":
		return len(args) >= 2
	default:
		return false
	}
}

func operatorHelpTopicAllowed(topic string) bool {
	switch topic {
	case "apps", "config", "domains", "deployments", "events", "logs", "releases", "version":
		return true
	default:
		return false
	}
}

func operatorHelpRequested(args []string) bool {
	return len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") ||
		len(args) == 2 && (args[0] == "help" || args[1] == "-h" || args[1] == "--help")
}

func printOperatorHelp(stdout io.Writer, args []string) {
	topic := ""
	if len(args) == 2 {
		if args[0] == "help" {
			topic = args[1]
		} else {
			topic = args[0]
		}
	}

	switch topic {
	case "apps":
		fmt.Fprint(stdout, `Inspect apps over restricted SSH.

Usage:
  apps list
  apps info <app>
  apps health <app>
`)
	case "config":
		fmt.Fprint(stdout, `Manage encrypted app config over restricted SSH.

Usage:
  config set <app> <key>
  config import <app>
  config list <app>
  config keys <app>
  config get <app> <key>
  config unset <app> <key>
`)
	case "domains":
		fmt.Fprint(stdout, `Inspect app domains over restricted SSH.

Usage:
  domains list <app>
  domains check <app>
`)
	case "deployments", "events", "logs", "releases", "version":
		fmt.Fprintf(stdout, "Run `ssh sshdock@<host> %s ...` for restricted %s access.\n", topic, topic)
	default:
		fmt.Fprint(stdout, `SSHDock restricted SSH commands

Usage:
  ssh sshdock@<host> [command]

Inspection:
  apps list
  apps info <app>
  apps health <app>
  domains list <app>
  domains check <app>
  releases list <app>
  deployments list <app>
  events list <app>
  logs <app> [service] [-f] [--tail <lines>]

Config:
  config set <app> <key>
  config import <app>
  config list <app>
  config keys <app>
  config get <app> <key>
  config unset <app> <key>

Host administration remains local through sudo sshdock.
`)
	}
}
