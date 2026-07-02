package cli

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

type App struct {
	Name   string
	Status string
	NodeID string
}

type Domain struct {
	AppName     string
	ServiceName string
	DomainName  string
	Port        int
}

type SSHKey struct {
	Name      string
	PublicKey string
	CreatedAt time.Time
}

type Backend interface {
	CreateApp(name string) (App, string, error)
	ListApps() ([]App, error)
	GetApp(name string) (App, error)
	RestartApp(name string) error
	RestartService(appName string, serviceName string) error
	RedeployApp(name string) error
	RollbackApp(name string, releaseID string) error
	AttachDomain(domain Domain) error
	SetServerGitHost(host string) error
	AddSSHKey(name string, publicKey string) error
}

type MemoryBackend struct {
	gitHost string
	apps    map[string]App
	domains []Domain
	keys    map[string]SSHKey
}

func NewMemoryBackend(gitHost string) *MemoryBackend {
	if gitHost == "" {
		gitHost = "server"
	}

	return &MemoryBackend{
		gitHost: gitHost,
		apps:    map[string]App{},
		keys:    map[string]SSHKey{},
	}
}

func (b *MemoryBackend) CreateApp(name string) (App, string, error) {
	if _, ok := b.apps[name]; ok {
		return App{}, "", fmt.Errorf("app %q already exists", name)
	}

	model := App{Name: name, Status: "created", NodeID: "local"}
	b.apps[name] = model

	return model, fmt.Sprintf("git@%s:%s.git", b.gitHost, name), nil
}

func (b *MemoryBackend) ListApps() ([]App, error) {
	names := make([]string, 0, len(b.apps))
	for name := range b.apps {
		names = append(names, name)
	}
	sort.Strings(names)

	apps := make([]App, 0, len(names))
	for _, name := range names {
		apps = append(apps, b.apps[name])
	}

	return apps, nil
}

func (b *MemoryBackend) GetApp(name string) (App, error) {
	model, ok := b.apps[name]
	if !ok {
		return App{}, fmt.Errorf("app %q not found", name)
	}

	return model, nil
}

func (b *MemoryBackend) AttachDomain(domain Domain) error {
	if _, ok := b.apps[domain.AppName]; !ok {
		return fmt.Errorf("app %q not found", domain.AppName)
	}

	b.domains = append(b.domains, domain)
	return nil
}

func (b *MemoryBackend) RestartApp(name string) error {
	if _, ok := b.apps[name]; !ok {
		return fmt.Errorf("app %q not found", name)
	}
	return nil
}

func (b *MemoryBackend) RestartService(appName string, serviceName string) error {
	if _, ok := b.apps[appName]; !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	if strings.TrimSpace(serviceName) == "" {
		return fmt.Errorf("service name is required")
	}
	return nil
}

func (b *MemoryBackend) RedeployApp(name string) error {
	if _, ok := b.apps[name]; !ok {
		return fmt.Errorf("app %q not found", name)
	}
	return nil
}

func (b *MemoryBackend) RollbackApp(name string, releaseID string) error {
	if _, ok := b.apps[name]; !ok {
		return fmt.Errorf("app %q not found", name)
	}
	if strings.TrimSpace(releaseID) == "" {
		return fmt.Errorf("release ID is required")
	}
	return nil
}

func (b *MemoryBackend) SetServerGitHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("server Git host is required")
	}
	b.gitHost = host
	return nil
}

func (b *MemoryBackend) AddSSHKey(name string, publicKey string) error {
	name = strings.TrimSpace(name)
	publicKey = strings.TrimSpace(publicKey)
	if name == "" {
		return fmt.Errorf("SSH key name is required")
	}
	if err := validatePublicKey(publicKey); err != nil {
		return err
	}

	b.keys[name] = SSHKey{Name: name, PublicKey: publicKey, CreatedAt: time.Now().UTC()}
	return nil
}

type Runner struct {
	backend Backend
	version string
}

func NewRunner(backend Backend, version string) *Runner {
	return &Runner{backend: backend, version: version}
}

func (r *Runner) Run(args []string, stdout io.Writer, stderr io.Writer) int {
	return r.RunWithInput(args, nil, stdout, stderr)
}

func (r *Runner) RunWithInput(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintf(stdout, "rhumbase %s\n", r.version)
		return 0
	}

	if len(args) >= 1 {
		switch args[0] {
		case "apps":
			return r.runApps(args[1:], stdout, stderr)
		case "domains":
			return r.runDomains(args[1:], stdout, stderr)
		case "server":
			return r.runServer(args[1:], stdout, stderr)
		case "ssh-keys":
			return r.runSSHKeys(args[1:], stdin, stdout, stderr)
		}
	}

	printUsage(stderr)
	return 2
}

func (r *Runner) runApps(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 2 && args[0] == "create" {
		app, remoteURL, err := r.backend.CreateApp(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		fmt.Fprintf(stdout, "created app %s\n", app.Name)
		fmt.Fprintf(stdout, "git remote add rhumbase %s\n", remoteURL)
		fmt.Fprintln(stdout, "git push rhumbase main")
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

	if len(args) == 2 && args[0] == "redeploy" {
		if err := r.backend.RedeployApp(args[1]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "redeployed %s\n", args[1])
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

	printUsage(stderr)
	return 2
}

func (r *Runner) runDomains(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 6 && args[0] == "attach" && args[4] == "--port" {
		port, err := strconv.Atoi(args[5])
		if err != nil {
			fmt.Fprintf(stderr, "invalid port %q\n", args[5])
			return 2
		}

		domain := Domain{
			AppName:     args[1],
			ServiceName: args[2],
			DomainName:  args[3],
			Port:        port,
		}
		if err := r.backend.AttachDomain(domain); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		fmt.Fprintf(stdout, "attached %s to %s/%s:%d\n", domain.DomainName, domain.AppName, domain.ServiceName, domain.Port)
		fmt.Fprintf(stdout, "point DNS for %s to this server\n", domain.DomainName)
		return 0
	}

	printUsage(stderr)
	return 2
}

func (r *Runner) runServer(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 3 && args[0] == "domain" && args[1] == "set" {
		if err := r.backend.SetServerGitHost(args[2]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "server Git host set to %s\n", strings.TrimSpace(args[2]))
		return 0
	}

	printUsage(stderr)
	return 2
}

func (r *Runner) runSSHKeys(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 2 && args[0] == "add" {
		if stdin == nil {
			stdin = strings.NewReader("")
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "read SSH public key: %v\n", err)
			return 1
		}
		if err := r.backend.AddSSHKey(args[1], string(data)); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "added SSH key %s\n", args[1])
		return 0
	}

	printUsage(stderr)
	return 2
}

func printUsage(stderr io.Writer) {
	fmt.Fprintln(stderr, "usage: rhumbase version | diagnostics | apps create <name> | apps list | apps info <name> | apps restart <name> [service] | apps redeploy <name> | apps rollback <name> <release-id> | domains attach <app> <service> <domain> --port <port> | server domain set <domain> | ssh-keys add <name>")
}

func validatePublicKey(publicKey string) error {
	if publicKey == "" {
		return fmt.Errorf("SSH public key is required on stdin")
	}
	fields := strings.Fields(publicKey)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "ssh-") {
		return fmt.Errorf("invalid SSH public key")
	}
	return nil
}
