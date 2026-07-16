package cli

import (
	"fmt"
	"io"
)

func printRootHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `SSHDock - Git push Compose apps. Operate over SSH.

Usage:
  sshdock <command> [arguments]
  sshdock help [command]

Core:
  version                              Print CLI version
  diagnostics                          Check runtime readiness

Apps:
  apps create <name>                   Create an app repo and print Git remote
  apps list                            List apps
  apps info <name>                     Show app details
  apps health <name>                   Summarize app and Compose runtime health
  apps start <name>                    Start existing Compose containers
  apps stop <name>                     Stop and preserve Compose containers
  apps restart <name> [service]        Restart an app or service
  apps exec <app> <service> -- <cmd>   Execute argv in a running service
  apps run <app> <service> -- <cmd>    Run a removable one-off container
  apps redeploy <name>                 Redeploy current remote main
  apps remove <name> [--force]         Remove an app while preserving volumes

Config:
  config set <app> <key>
  config import <app>
  config list <app>
  config keys <app>
  config get <app> <key>
  config unset <app> <key>

Domains:
  domains attach <app> <service> <domain> --port <port>
  domains list <app>
  domains check <app>
  domains detach <app> <domain>

Operations:
  backup create [--output <archive>]
  backup inspect <archive>
  backup restore <archive>
  logs <app> [service] [-f] [--tail <lines>]
  releases list <app>
  deployments list <app>
  events list <app>

Access:
  ssh-keys add <name>
  ssh-keys list
  ssh-keys remove <name>

Server:
  server domain set <domain>

Use "sshdock help <command>" for details.
`)
}

func printHelpTopic(topic string, stdout io.Writer, stderr io.Writer) int {
	switch topic {
	case "apps":
		printTopicHelp(stdout, "Apps manage Compose app records and lifecycle actions.", []string{
			"sshdock apps create <name>",
			"sshdock apps list",
			"sshdock apps info <name>",
			"sshdock apps health <name>",
			"sshdock apps start <name>",
			"sshdock apps stop <name>",
			"sshdock apps restart <name> [service]",
			"sshdock apps exec <app> <service> -- <command> [args...]",
			"sshdock apps run <app> <service> -- <command> [args...]",
			"sshdock apps redeploy <name>",
			"sshdock apps remove <name> [--force]",
		}, []string{
			"sudo sshdock apps create my-app",
			"ssh sshdock@<host> apps stop my-app",
			"ssh sshdock@<host> apps start my-app",
			"sudo sshdock apps restart my-app web",
			`ssh -tt sshdock@<host> apps exec my-app web -- sh`,
			`ssh sshdock@<host> apps run my-app web -- ./bin/migrate up`,
		})
	case "config":
		printTopicHelp(stdout, "Config commands store encrypted app config.", []string{
			"sshdock config set <app> <key>",
			"sshdock config import <app>",
			"sshdock config list <app>",
			"sshdock config keys <app>",
			"sshdock config get <app> <key>",
			"sshdock config unset <app> <key>",
		}, []string{
			`printf '%s' "$DATABASE_URL" | ssh sshdock@<host> config set my-app DATABASE_URL`,
			"ssh sshdock@<host> config import my-app < .env.production",
			"ssh sshdock@<host> config keys my-app",
			"sudo sshdock config get my-app DATABASE_URL",
		})
	case "domains":
		printTopicHelp(stdout, "Domain commands attach public hostnames to app services.", []string{
			"sshdock domains attach <app> <service> <domain> --port <port>",
			"sshdock domains list <app>",
			"sshdock domains check <app>",
			"sshdock domains detach <app> <domain>",
		}, []string{
			"sudo sshdock domains attach my-app web app.example.com --port 3000",
			"sudo sshdock domains check my-app",
		})
	case "logs":
		printTopicHelp(stdout, "Logs stream recent Compose logs for an app or service.", []string{
			"sshdock logs <app> [service] [-f] [--tail <lines>]",
		}, []string{
			"sudo sshdock logs my-app",
			"sudo sshdock logs my-app web --tail 200 -f",
		})
	case "backup":
		printTopicHelp(stdout, "Backup commands create, inspect, and restore SSHDock state archives.", []string{
			"sshdock backup create [--output <archive>]",
			"sshdock backup inspect <archive>",
			"sshdock backup restore <archive>",
		}, []string{
			"sudo sshdock backup create",
			"sudo sshdock backup inspect /var/lib/sshdock/backups/sshdock-backup-20260709T100000Z.tar.gz",
			"sudo systemctl stop sshdockd && sudo sshdock backup restore /var/lib/sshdock/backups/sshdock-backup-20260709T100000Z.tar.gz",
		})
	case "releases":
		printTopicHelp(stdout, "Release commands inspect deployable release records.", []string{"sshdock releases list <app>"}, []string{"sudo sshdock releases list my-app"})
	case "deployments":
		printTopicHelp(stdout, "Deployment commands inspect every execution attempt.", []string{"sshdock deployments list <app>"}, []string{"sudo sshdock deployments list my-app"})
	case "events":
		printTopicHelp(stdout, "Event commands inspect app runtime and deployment events, including retained removal audit history.", []string{"sshdock events list <app>"}, []string{"sudo sshdock events list my-app"})
	case "server":
		printTopicHelp(stdout, "Server commands configure single-node SSHDock host behavior.", []string{"sshdock server domain set <domain>"}, []string{"sudo sshdock server domain set example.com"})
	case "ssh-keys":
		printTopicHelp(stdout, "SSH key commands manage deploy and operator access keys.", []string{
			"sshdock ssh-keys add <name>",
			"sshdock ssh-keys list",
			"sshdock ssh-keys remove <name>",
		}, []string{
			"cat ~/.ssh/id_ed25519.pub | sudo sshdock ssh-keys add admin",
			"sudo sshdock ssh-keys list",
		})
	case "version":
		printTopicHelp(stdout, "Version prints the CLI version.", []string{"sshdock version"}, []string{"sshdock version"})
	case "diagnostics":
		printTopicHelp(stdout, "Diagnostics checks SSHDock runtime readiness.", []string{"sshdock diagnostics"}, []string{"sudo sshdock diagnostics"})
	default:
		fmt.Fprintf(stderr, "unknown help topic %q\n", topic)
		fmt.Fprintln(stderr, `Run "sshdock help" for available commands.`)
		return 2
	}
	return 0
}

func printTopicHelp(stdout io.Writer, description string, usage []string, examples []string) {
	fmt.Fprintln(stdout, description)
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Usage:")
	for _, line := range usage {
		fmt.Fprintf(stdout, "  %s\n", line)
	}
	if len(examples) == 0 {
		return
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Examples:")
	for _, line := range examples {
		fmt.Fprintf(stdout, "  %s\n", line)
	}
}

func printUnknownCommand(stderr io.Writer, command string) {
	fmt.Fprintf(stderr, "unknown command %q\n", command)
	fmt.Fprintln(stderr, `Run "sshdock help" for available commands.`)
}

func printInvalidUsage(stderr io.Writer, topic string) {
	fmt.Fprintf(stderr, "invalid %s command or arguments\n", topic)
	fmt.Fprintf(stderr, "Run \"sshdock help %s\" for usage.\n", topic)
}
