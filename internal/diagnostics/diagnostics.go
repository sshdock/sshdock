package diagnostics

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/iketiunn/rumbase/internal/config"
	"github.com/iketiunn/rumbase/internal/store"
)

type Command struct {
	Name string
	Args []string
}

type CommandExecutor interface {
	Run(ctx context.Context, command Command) (string, error)
}

type Report struct {
	Checks []Check
	OK     bool
}

type Check struct {
	Name    string
	OK      bool
	Message string
}

func Run(ctx context.Context, cfg config.Config, executor CommandExecutor) Report {
	report := Report{OK: true}

	if err := cfg.Validate(); err != nil {
		report.add("config", false, err.Error())
	} else {
		report.add("config", true, "valid")
	}

	report.checkDir("data dir", cfg.DataDir)
	report.checkDir("apps dir", cfg.AppsDir)
	report.checkDir("sqlite dir", filepath.Dir(cfg.SQLiteDBPath))
	report.checkDir("git home dir", cfg.GitHomeDir)
	report.checkDir("git authorized_keys dir", filepath.Dir(cfg.GitAuthorizedKeysPath))
	report.checkDir("dashboard host key dir", filepath.Dir(cfg.DashboardHostKeyPath))
	report.checkDir("dashboard authorized_keys dir", filepath.Dir(cfg.DashboardAuthorizedKeysPath))
	report.checkDir("caddy config dir", filepath.Dir(cfg.CaddyConfigPath))

	report.checkCommand(ctx, executor, "docker", Command{Name: "docker", Args: []string{"version"}})
	report.checkCommand(ctx, executor, "docker compose", Command{Name: "docker", Args: []string{"compose", "version"}})
	report.checkCommand(ctx, executor, "caddy", Command{Name: "caddy", Args: []string{"version"}})
	report.checkCommand(ctx, executor, "ssh", Command{Name: "ssh", Args: []string{"-V"}})
	report.checkCommand(ctx, executor, "sshd", Command{Name: "sshd", Args: []string{"-V"}})
	report.checkCommand(ctx, executor, "git", Command{Name: "git", Args: []string{"--version"}})
	report.checkStore(ctx, cfg.SQLiteDBPath)

	return report
}

func (r Report) String() string {
	var builder strings.Builder
	for _, check := range r.Checks {
		status := "ok"
		if !check.OK {
			status = "fail"
		}
		builder.WriteString(status)
		builder.WriteString(" ")
		builder.WriteString(check.Name)
		if check.Message != "" {
			builder.WriteString(": ")
			builder.WriteString(check.Message)
		}
		builder.WriteByte('\n')
	}
	if r.OK {
		builder.WriteString("diagnostics ok\n")
	} else {
		builder.WriteString("diagnostics failed\n")
	}
	return builder.String()
}

func (r *Report) add(name string, ok bool, message string) {
	r.Checks = append(r.Checks, Check{Name: name, OK: ok, Message: message})
	if !ok {
		r.OK = false
	}
}

func (r *Report) checkDir(name string, path string) {
	info, err := os.Stat(path)
	if err != nil {
		r.add(name, false, fmt.Sprintf("%s: %v", path, err))
		return
	}
	if !info.IsDir() {
		r.add(name, false, path+" is not a directory")
		return
	}
	r.add(name, true, path)
}

func (r *Report) checkCommand(ctx context.Context, executor CommandExecutor, name string, command Command) {
	if executor == nil {
		r.add(name, false, "command executor is not configured")
		return
	}
	if _, err := executor.Run(ctx, command); err != nil {
		r.add(name, false, err.Error())
		return
	}
	r.add(name, true, strings.TrimSpace(command.Name+" "+strings.Join(command.Args, " ")))
}

func (r *Report) checkStore(ctx context.Context, dbPath string) {
	sqlite, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		r.add("sqlite migrations", false, err.Error())
		return
	}
	if err := sqlite.Close(); err != nil {
		r.add("sqlite migrations", false, err.Error())
		return
	}
	r.add("sqlite migrations", true, dbPath)
}
