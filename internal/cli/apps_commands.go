package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func (r *Runner) runApps(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 2 && args[0] == "create" {
		app, remoteURL, err := r.backend.CreateApp(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		fmt.Fprintf(stdout, "created app %s\n", app.Name)
		fmt.Fprintf(stdout, "git remote add sshdock %s\n", remoteURL)
		fmt.Fprintln(stdout, "git push sshdock main")
		if app.DefaultURL != "" {
			fmt.Fprintf(stdout, "default URL after first deploy: %s\n", app.DefaultURL)
		}
		return 0
	}

	if len(args) == 1 && args[0] == "list" {
		apps, err := r.backend.ListApps()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(apps) == 0 {
			fmt.Fprintln(stdout, "no apps")
			return 0
		}

		for _, app := range apps {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", app.Name, app.Status, app.NodeID)
		}
		return 0
	}

	if len(args) == 2 && args[0] == "info" {
		app, err := r.backend.GetApp(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		fmt.Fprintf(stdout, "name: %s\n", app.Name)
		fmt.Fprintf(stdout, "status: %s\n", app.Status)
		fmt.Fprintf(stdout, "node: %s\n", app.NodeID)
		return 0
	}

	if len(args) == 2 && args[0] == "health" {
		report, err := r.backend.AppHealth(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		printAppHealth(stdout, report)
		return 0
	}

	if len(args) == 2 && args[0] == "start" {
		if err := r.backend.StartApp(args[1]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "started app %s\n", args[1])
		return 0
	}

	if len(args) == 2 && args[0] == "stop" {
		if err := r.backend.StopApp(args[1]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "stopped app %s\n", args[1])
		return 0
	}

	if len(args) == 3 && args[0] == "restart" {
		if err := r.backend.RestartService(args[1], args[2]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "restarted %s/%s\n", args[1], args[2])
		return 0
	}

	if len(args) == 2 && args[0] == "restart" {
		if err := r.backend.RestartApp(args[1]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "restarted app %s\n", args[1])
		return 0
	}

	if len(args) >= 2 && args[0] == "remove" {
		appName, force, ok := parseRemoveArgs(args[1:])
		if !ok {
			printInvalidUsage(stderr, "apps")
			return 2
		}
		if !force {
			if stdin == nil {
				stdin = strings.NewReader("")
			}
			fmt.Fprintf(stderr, "type %s to confirm app removal: ", appName)
			line, err := bufio.NewReader(stdin).ReadString('\n')
			if err != nil && err != io.EOF {
				fmt.Fprintf(stderr, "read confirmation: %v\n", err)
				return 1
			}
			if strings.TrimSpace(line) != appName {
				fmt.Fprintln(stderr, "app removal aborted")
				return 1
			}
		}
		if err := r.backend.RemoveApp(appName); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "removed app %s\n", appName)
		fmt.Fprintln(stdout, "Docker volumes were not removed; remove app-specific volumes manually after backup if desired.")
		return 0
	}

	if len(args) == 2 && args[0] == "redeploy" {
		if err := r.backend.RedeployApp(args[1]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "redeployed current main for %s\n", args[1])
		return 0
	}

	if len(args) == 3 && args[0] == "rollback" {
		if err := r.backend.RollbackApp(args[1], args[2]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "rolled back %s to %s\n", args[1], args[2])
		return 0
	}

	printInvalidUsage(stderr, "apps")
	return 2
}

func parseRemoveArgs(args []string) (string, bool, bool) {
	var appName string
	var force bool
	for _, arg := range args {
		if arg == "--force" {
			force = true
			continue
		}
		if appName == "" {
			appName = arg
			continue
		}
		return "", false, false
	}
	return appName, force, appName != ""
}
