package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sshdock/sshdock/internal/config"
	domaincfg "github.com/sshdock/sshdock/internal/domain"
	"github.com/sshdock/sshdock/internal/store"
)

type Command struct {
	Name string
	Args []string
	Dir  string
	Env  map[string]string
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
	Warning bool
	Message string
	Why     string
	Fix     string
}

func Run(ctx context.Context, cfg config.Config, executor CommandExecutor) Report {
	report := Report{OK: true}

	if err := cfg.Validate(); err != nil {
		report.addFix("config", false, err.Error(), "SSHDock needs complete runtime paths and host settings before it can deploy apps.", "set the missing SSHDOCK_* environment values or rerun scripts/bootstrap.sh")
	} else {
		report.add("config", true, "valid")
	}

	report.checkDir("data dir", cfg.DataDir)
	report.checkDir("apps dir", cfg.AppsDir)
	report.checkDir("config key dir", filepath.Dir(cfg.ConfigKeyPath))
	report.checkDir("sqlite dir", filepath.Dir(cfg.SQLiteDBPath))
	report.checkDir("git home dir", cfg.GitHomeDir)
	report.checkDir("git authorized_keys dir", filepath.Dir(cfg.GitAuthorizedKeysPath))
	report.checkDir("operator host key dir", filepath.Dir(cfg.OperatorHostKeyPath))
	report.checkDir("operator authorized_keys dir", filepath.Dir(cfg.OperatorAuthorizedKeysPath))
	report.checkDir("caddy config dir", filepath.Dir(cfg.CaddyConfigPath))

	serverConfig, hasServerConfig := report.checkStore(ctx, cfg, executor)

	report.checkLinux(ctx, executor)
	report.checkCommand(ctx, executor, "docker", Command{Name: "docker", Args: []string{"version"}}, "Docker Engine runs app containers.", "install or start Docker Engine, then rerun sudo sshdock diagnostics")
	report.checkCommand(ctx, executor, "docker compose", Command{Name: "docker", Args: []string{"compose", "version"}}, "SSHDock deploys apps through the Docker Compose plugin.", "install the Docker Compose plugin so docker compose version succeeds")
	report.checkCommand(ctx, executor, "caddy", Command{Name: "caddy", Args: []string{"version"}}, "Caddy terminates HTTP and HTTPS traffic for app routes.", "install or repair Caddy, then rerun sudo sshdock diagnostics")
	report.checkCommand(ctx, executor, "ssh", Command{Name: "ssh", Args: []string{"-V"}}, "The SSH client is useful for dashboard and smoke checks from the host.", "install OpenSSH client")
	report.checkCommand(ctx, executor, "sshd", Command{Name: "sshd", Args: []string{"-V"}}, "OpenSSH serves Git push and dashboard forced-command access.", "install openssh-server and make sure sshd is available")
	report.checkCommand(ctx, executor, "git", Command{Name: "git", Args: []string{"--version"}}, "Git receives app pushes and manages bare repositories.", "install Git")
	report.checkCommand(ctx, executor, "systemd", Command{Name: "systemctl", Args: []string{"--version"}}, "The installer manages sshdockd, Docker, Caddy, and sshd through systemd.", "use a systemd-based Ubuntu or Debian VPS")
	report.checkCommand(ctx, executor, "sshdockd service", Command{Name: "systemctl", Args: []string{"is-active", "sshdockd.service"}}, "The daemon must be active for Git receive hooks and operator access.", "run sudo systemctl enable --now sshdockd.service and inspect sudo systemctl status sshdockd.service")
	report.checkPorts(ctx, executor)
	report.checkDNS(ctx, executor, serverConfig, hasServerConfig)
	report.checkCaddyImport(cfg)
	report.checkCommand(ctx, executor, "caddy config", Command{Name: "caddy", Args: []string{"validate", "--config", cfg.CaddyMainConfigPath}}, "Caddy must be able to load the main config and SSHDock's generated import.", "fix the Caddyfile error, then run sudo systemctl reload caddy")
	report.checkAuthorizedKeys("git authorized_keys", cfg.GitAuthorizedKeysPath, cfg.GitReceiveCommand)
	report.checkAuthorizedKeys("operator authorized_keys", cfg.OperatorAuthorizedKeysPath, cfg.OperatorCommand)
	report.checkRuntimePermissions(cfg)
	report.checkConfigKey(cfg.ConfigKeyPath)

	return report
}

func (r Report) String() string {
	var builder strings.Builder
	for _, check := range r.Checks {
		status := "ok"
		if check.Warning {
			status = "warn"
		} else if !check.OK {
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
		if !check.OK {
			if check.Why != "" {
				builder.WriteString("why ")
				builder.WriteString(check.Name)
				builder.WriteString(": ")
				builder.WriteString(check.Why)
				builder.WriteByte('\n')
			}
			if check.Fix != "" {
				builder.WriteString("fix ")
				builder.WriteString(check.Name)
				builder.WriteString(": ")
				builder.WriteString(check.Fix)
				builder.WriteByte('\n')
			}
		}
	}
	if r.OK {
		builder.WriteString("diagnostics ok\n")
	} else {
		builder.WriteString("diagnostics failed\n")
	}
	return builder.String()
}

func (r *Report) add(name string, ok bool, message string) {
	r.addFix(name, ok, message, "", "")
}

func (r *Report) addWarning(name string, message string) {
	r.Checks = append(r.Checks, Check{Name: name, OK: true, Warning: true, Message: message})
}

func (r *Report) addFix(name string, ok bool, message string, why string, fix string) {
	r.Checks = append(r.Checks, Check{Name: name, OK: ok, Message: message, Why: why, Fix: fix})
	if !ok {
		r.OK = false
	}
}

func (r *Report) checkDir(name string, path string) {
	info, err := os.Stat(path)
	if err != nil {
		r.addFix(name, false, fmt.Sprintf("%s: %v", path, err), "SSHDock runtime files must be present before deploy and lifecycle commands can work.", "rerun scripts/bootstrap.sh or create the missing directory with the ownership documented in docs/INSTALL.md")
		return
	}
	if !info.IsDir() {
		r.addFix(name, false, path+" is not a directory", "SSHDock expected this path to be a directory.", "move the file aside and create the directory")
		return
	}
	r.add(name, true, path)
}

func (r *Report) checkCommand(ctx context.Context, executor CommandExecutor, name string, command Command, why string, fix string) {
	if executor == nil {
		r.addFix(name, false, "command executor is not configured", why, fix)
		return
	}
	if _, err := executor.Run(ctx, command); err != nil {
		r.addFix(name, false, err.Error(), why, fix)
		return
	}
	r.add(name, true, strings.TrimSpace(command.Name+" "+strings.Join(command.Args, " ")))
}

func (r *Report) checkStore(ctx context.Context, cfg config.Config, executor CommandExecutor) (store.ServerConfig, bool) {
	sqlite, err := store.OpenSQLite(ctx, cfg.SQLiteDBPath)
	if err != nil {
		r.addFix("sqlite migrations", false, err.Error(), "SSHDock stores app, release, route, key, and config metadata in SQLite.", "restore or repair sshdock.db, then rerun sudo sshdock diagnostics")
		return store.ServerConfig{}, false
	}
	serverConfig, err := sqlite.GetServerConfig(ctx)
	hasServerConfig := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		r.addFix("server config", false, err.Error(), "Diagnostics need the stored base domain to validate DNS.", "repair sshdock.db or rerun sudo sshdock server domain set <domain>")
	}
	r.checkRestartPolicies(ctx, cfg, executor, sqlite)
	if closeErr := sqlite.Close(); closeErr != nil {
		r.addFix("sqlite migrations", false, closeErr.Error(), "SSHDock stores app, release, route, key, and config metadata in SQLite.", "close other database users and rerun sudo sshdock diagnostics")
		return serverConfig, hasServerConfig
	}
	r.add("sqlite migrations", true, cfg.SQLiteDBPath)
	return serverConfig, hasServerConfig
}

func (r *Report) checkLinux(ctx context.Context, executor CommandExecutor) {
	output, err := runDiagnosticCommand(ctx, executor, Command{Name: "uname", Args: []string{"-s"}})
	if err != nil {
		r.addFix("operating system", false, err.Error(), "SSHDock v0 is supported on Linux VPS hosts.", "run SSHDock on Ubuntu LTS or Debian stable")
		return
	}
	if strings.TrimSpace(output) != "Linux" {
		r.addFix("operating system", false, strings.TrimSpace(output), "SSHDock v0 is supported on Linux VPS hosts.", "run SSHDock on Ubuntu LTS or Debian stable")
		return
	}
	r.add("operating system", true, "Linux")
}

func (r *Report) checkPorts(ctx context.Context, executor CommandExecutor) {
	output, err := runDiagnosticCommand(ctx, executor, Command{Name: "ss", Args: []string{"-ltn"}})
	for _, port := range []string{"22", "80", "443"} {
		name := "port " + port
		if err != nil {
			r.addFix(name, false, err.Error(), "Public SSHDock installs need SSH, HTTP, and HTTPS listeners reachable.", "install iproute2 so ss -ltn works, then confirm TCP port "+port+" is open")
			continue
		}
		if listensOnPort(output, port) {
			r.add(name, true, "listening")
			continue
		}
		r.addFix(name, false, "not listening", "Public SSHDock installs need SSH, HTTP, and HTTPS listeners reachable.", "open TCP port "+port+" in the host firewall and VPS provider firewall")
	}
}

func (r *Report) checkDNS(ctx context.Context, executor CommandExecutor, serverConfig store.ServerConfig, hasServerConfig bool) {
	if !hasServerConfig || strings.TrimSpace(serverConfig.BaseDomain) == "" {
		r.addFix("base-domain DNS", false, "server base domain is not configured", "SSHDock derives public Git, dashboard, and app hostnames from the base domain.", "run sudo sshdock server domain set example.com")
		r.addFix("wildcard DNS", false, "server base domain is not configured", "Wildcard app DNS is required for automatic app routes.", "point *.example.com at this server after setting the base domain")
		return
	}

	controlHost := domaincfg.ControlHost(serverConfig.BaseDomain)
	checkDNSHost(ctx, r, executor, "base-domain DNS", controlHost, "The control host receives Git SSH and operator SSH connections.", "create an A or AAAA record for "+controlHost+" pointing at this server")

	wildcardProbe := "sshdock-diagnostics." + serverConfig.BaseDomain
	checkDNSHost(ctx, r, executor, "wildcard DNS", wildcardProbe, "Wildcard DNS lets SSHDock route app hosts automatically.", "create a wildcard A or AAAA record for *."+serverConfig.BaseDomain+" pointing at this server")
}

func (r *Report) checkCaddyImport(cfg config.Config) {
	data, err := os.ReadFile(cfg.CaddyMainConfigPath)
	if err != nil {
		r.addFix("caddy import", false, err.Error(), "Caddy's main config must import SSHDock's generated route file.", "add this exact line to "+cfg.CaddyMainConfigPath+": import "+cfg.CaddyConfigPath)
		return
	}
	want := "import " + cfg.CaddyConfigPath
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == want {
			r.add("caddy import", true, want)
			return
		}
	}
	r.addFix("caddy import", false, "missing "+want, "Caddy's main config must import SSHDock's generated route file.", "add this exact line to "+cfg.CaddyMainConfigPath+": "+want)
}

func (r *Report) checkAuthorizedKeys(name string, path string, command string) {
	info, err := os.Stat(path)
	if err != nil {
		r.addFix(name, false, err.Error(), "OpenSSH forced-command keys restrict users to SSHDock deploy and dashboard commands.", "run sudo sshdock ssh-keys add <name> after install")
		return
	}
	if info.Mode().Perm()&0o077 != 0 {
		r.addFix(name, false, fmt.Sprintf("%s mode is %04o", path, info.Mode().Perm()), "OpenSSH may reject loose authorized_keys files.", "run sudo chmod 0600 "+path)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		r.addFix(name, false, err.Error(), "Diagnostics need to verify forced-command key restrictions.", "repair permissions for "+path)
		return
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		r.add(name, true, "no keys installed yet")
		return
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.Contains(line, `command="exec `+command+`"`) {
			r.addFix(name, false, "missing forced command for "+command, "OpenSSH keys must be restricted to SSHDock forced commands.", "rerun sudo sshdock ssh-keys add <name> to rewrite authorized_keys")
			return
		}
	}
	r.add(name, true, path)
}

func (r *Report) checkRuntimePermissions(cfg config.Config) {
	for _, path := range []string{
		cfg.DataDir,
		cfg.AppsDir,
		cfg.GitHomeDir,
		filepath.Dir(cfg.GitAuthorizedKeysPath),
		filepath.Dir(cfg.OperatorAuthorizedKeysPath),
		filepath.Dir(cfg.CaddyConfigPath),
	} {
		info, err := os.Stat(path)
		if err != nil {
			r.addFix("runtime permissions", false, fmt.Sprintf("%s: %v", path, err), "Runtime directories must exist with predictable ownership before deploys can mutate state.", "rerun scripts/bootstrap.sh to recreate runtime directories")
			return
		}
		if !info.IsDir() {
			r.addFix("runtime permissions", false, path+" is not a directory", "Runtime directories must be directories.", "move the file aside and rerun scripts/bootstrap.sh")
			return
		}
		if info.Mode().Perm()&0o022 != 0 {
			r.addFix("runtime permissions", false, fmt.Sprintf("%s mode is %04o", path, info.Mode().Perm()), "Group- or world-writable runtime directories can break OpenSSH strict modes and weaken state isolation.", "rerun scripts/bootstrap.sh or tighten the directory mode")
			return
		}
	}
	r.add("runtime permissions", true, "directories are not group- or world-writable")
}

func (r *Report) checkConfigKey(path string) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		r.add("config key", true, "not created yet")
		return
	}
	if err != nil {
		r.addFix("config key", false, err.Error(), "Encrypted app config needs the host-local key to decrypt stored values.", "restore "+path+" from backup or repair its permissions")
		return
	}
	if !info.Mode().IsRegular() {
		r.addFix("config key", false, path+" is not a regular file", "Encrypted app config needs a regular 32-byte host-local key file.", "replace "+path+" with the backed-up config.key")
		return
	}
	if info.Mode().Perm()&0o077 != 0 {
		r.addFix("config key", false, fmt.Sprintf("%s mode is %04o", path, info.Mode().Perm()), "The config key decrypts stored app secrets and must not be group- or world-readable.", "run sudo chmod 0600 "+path)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		r.addFix("config key", false, err.Error(), "Encrypted app config needs the host-local key to decrypt stored values.", "repair read permissions for "+path)
		return
	}
	if len(data) != 32 {
		r.addFix("config key", false, fmt.Sprintf("%s is %d bytes", path, len(data)), "SSHDock config encryption keys must be 32 bytes.", "restore a valid config.key from backup")
		return
	}
	r.add("config key", true, path)
}

func runDiagnosticCommand(ctx context.Context, executor CommandExecutor, command Command) (string, error) {
	if executor == nil {
		return "", fmt.Errorf("command executor is not configured")
	}
	output, err := executor.Run(ctx, command)
	if err != nil {
		return "", err
	}
	return output, nil
}

func listensOnPort(output string, port string) bool {
	for _, field := range strings.Fields(output) {
		field = strings.Trim(field, "[]")
		if strings.HasSuffix(field, ":"+port) || field == port {
			return true
		}
	}
	return false
}

func checkDNSHost(ctx context.Context, report *Report, executor CommandExecutor, name string, host string, why string, fix string) {
	output, err := runDiagnosticCommand(ctx, executor, Command{Name: "getent", Args: []string{"ahosts", host}})
	if err != nil {
		report.addFix(name, false, err.Error(), why, fix)
		return
	}
	if strings.TrimSpace(output) == "" {
		report.addFix(name, false, host+" did not resolve", why, fix)
		return
	}
	report.add(name, true, host)
}
