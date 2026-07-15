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

type operatorCommandSpec struct {
	allowed func(args []string) bool
	help    string
}

var operatorCommandSpecs = map[string]operatorCommandSpec{
	"apps": {
		allowed: appOperatorCommandAllowed,
		help: `Inspect and operate apps over restricted SSH.

Usage:
  apps list
  apps info <app>
  apps health <app>
  apps start <app>
  apps stop <app>
  apps restart <app> [service]
  apps redeploy <app>
  apps remove <app> --force
`,
	},
	"config": {
		allowed: func(args []string) bool {
			return len(args) == 2 && (args[0] == "import" || args[0] == "list" || args[0] == "keys") ||
				len(args) == 3 && (args[0] == "set" || args[0] == "get" || args[0] == "unset")
		},
		help: `Manage encrypted app config over restricted SSH.

Usage:
  config set <app> <key>
  config import <app>
  config list <app>
  config keys <app>
  config get <app> <key>
  config unset <app> <key>
`,
	},
	"domains": {
		allowed: func(args []string) bool {
			return len(args) == 2 && (args[0] == "list" || args[0] == "check")
		},
		help: `Inspect app domains over restricted SSH.

Usage:
  domains list <app>
  domains check <app>
`,
	},
	"deployments": listOperatorCommandSpec("deployment attempts", "deployments list <app>"),
	"events":      listOperatorCommandSpec("app events", "events list <app>"),
	"logs": {
		allowed: func(args []string) bool { return len(args) >= 1 },
		help: `Inspect Compose logs over restricted SSH.

Usage:
  logs <app> [service] [-f] [--tail <lines>]
`,
	},
	"releases": listOperatorCommandSpec("release records", "releases list <app>"),
	"version": {
		allowed: func(args []string) bool { return len(args) == 0 },
		help:    "Usage:\n  version\n",
	},
}

func appOperatorCommandAllowed(args []string) bool {
	if len(args) == 1 {
		return args[0] == "list"
	}
	if len(args) == 2 {
		switch args[0] {
		case "info", "health", "start", "stop", "restart", "redeploy":
			return true
		default:
			return false
		}
	}
	if len(args) == 3 {
		return args[0] == "restart" || args[0] == "remove" && args[2] == "--force"
	}
	return false
}

func listOperatorCommandSpec(description string, usage string) operatorCommandSpec {
	return operatorCommandSpec{
		allowed: func(args []string) bool { return len(args) == 2 && args[0] == "list" },
		help:    fmt.Sprintf("Inspect %s over restricted SSH.\n\nUsage:\n  %s\n", description, usage),
	}
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

	spec, ok := operatorCommandSpecs[args[0]]
	return ok && spec.allowed(args[1:])
}

func operatorHelpTopicAllowed(topic string) bool {
	_, ok := operatorCommandSpecs[topic]
	return ok
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

	if spec, ok := operatorCommandSpecs[topic]; ok {
		fmt.Fprint(stdout, spec.help)
		return
	}

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

Lifecycle:
  apps start <app>
  apps stop <app>
  apps restart <app> [service]
  apps redeploy <app>
  apps remove <app> --force

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
