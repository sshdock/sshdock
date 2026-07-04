package cli

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	domaincfg "github.com/iketiunn/rumbase/internal/domain"
)

type App struct {
	Name       string
	Status     string
	NodeID     string
	DefaultURL string
}

type Domain struct {
	AppName     string
	ServiceName string
	DomainName  string
	Port        int
	HTTPS       bool
}

type SSHKey struct {
	Name      string
	PublicKey string
	CreatedAt time.Time
}

type Release struct {
	ID          string
	AppName     string
	CommitSHA   string
	ComposePath string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Event struct {
	AppName   string
	Type      string
	Message   string
	CreatedAt time.Time
}

type LogRequest struct {
	AppName     string
	ServiceName string
	Follow      bool
}

type Backend interface {
	CreateApp(name string) (App, string, error)
	ListApps() ([]App, error)
	GetApp(name string) (App, error)
	RestartApp(name string) error
	RestartService(appName string, serviceName string) error
	RedeployApp(name string) error
	RollbackApp(name string, releaseID string) error
	RemoveApp(name string) error
	Logs(request LogRequest, stdout io.Writer, stderr io.Writer) error
	ListReleases(appName string) ([]Release, error)
	ListEvents(appName string) ([]Event, error)
	ListDomains(appName string) ([]Domain, error)
	AttachDomain(domain Domain) error
	DetachDomain(appName string, domainName string) error
	SetServerGitHost(host string) error
	AddSSHKey(name string, publicKey string) error
	ListSSHKeys() ([]SSHKey, error)
	RemoveSSHKey(name string) error
}

type MemoryBackend struct {
	gitHost     string
	baseDomain  string
	apps        map[string]App
	releases    []Release
	events      []Event
	domains     []Domain
	keys        map[string]SSHKey
	logOutput   string
	logRequests []LogRequest
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
	if b.baseDomain != "" {
		if appHost, err := domaincfg.AppHost(name, b.baseDomain); err == nil {
			model.DefaultURL = "https://" + appHost
		}
	}
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
	domain.HTTPS = true

	b.domains = append(b.domains, domain)
	return nil
}

func (b *MemoryBackend) DetachDomain(appName string, domainName string) error {
	if _, ok := b.apps[appName]; !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	for i, domain := range b.domains {
		if domain.AppName == appName && domain.DomainName == domainName {
			b.domains = append(b.domains[:i], b.domains[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("domain %q not found for app %q", domainName, appName)
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

func (b *MemoryBackend) RemoveApp(name string) error {
	if _, ok := b.apps[name]; !ok {
		return fmt.Errorf("app %q not found", name)
	}
	delete(b.apps, name)
	b.releases = filterReleases(b.releases, name)
	b.events = filterEvents(b.events, name)
	b.domains = filterDomains(b.domains, name)
	return nil
}

func (b *MemoryBackend) Logs(request LogRequest, stdout io.Writer, _ io.Writer) error {
	if _, ok := b.apps[request.AppName]; !ok {
		return fmt.Errorf("app %q not found", request.AppName)
	}
	b.logRequests = append(b.logRequests, request)
	_, err := fmt.Fprint(stdout, b.logOutput)
	return err
}

func (b *MemoryBackend) ListReleases(appName string) ([]Release, error) {
	if _, ok := b.apps[appName]; !ok {
		return nil, fmt.Errorf("app %q not found", appName)
	}
	var releases []Release
	for _, release := range b.releases {
		if release.AppName == appName {
			releases = append(releases, release)
		}
	}
	sort.Slice(releases, func(i, j int) bool {
		if releases[i].CreatedAt.Equal(releases[j].CreatedAt) {
			return releases[i].ID < releases[j].ID
		}
		return releases[i].CreatedAt.Before(releases[j].CreatedAt)
	})
	return releases, nil
}

func (b *MemoryBackend) ListEvents(appName string) ([]Event, error) {
	if _, ok := b.apps[appName]; !ok {
		return nil, fmt.Errorf("app %q not found", appName)
	}
	var events []Event
	for _, event := range b.events {
		if event.AppName == appName {
			events = append(events, event)
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].Type < events[j].Type
		}
		return events[i].CreatedAt.Before(events[j].CreatedAt)
	})
	return events, nil
}

func (b *MemoryBackend) ListDomains(appName string) ([]Domain, error) {
	if _, ok := b.apps[appName]; !ok {
		return nil, fmt.Errorf("app %q not found", appName)
	}
	var domains []Domain
	for _, domain := range b.domains {
		if domain.AppName == appName {
			domains = append(domains, domain)
		}
	}
	sort.Slice(domains, func(i, j int) bool {
		return domains[i].DomainName < domains[j].DomainName
	})
	return domains, nil
}

func (b *MemoryBackend) SetServerGitHost(host string) error {
	baseDomain, err := domaincfg.NormalizeBaseDomain(host)
	if err != nil {
		return err
	}
	b.baseDomain = baseDomain
	b.gitHost = domaincfg.ControlHost(baseDomain)
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

func (b *MemoryBackend) ListSSHKeys() ([]SSHKey, error) {
	names := make([]string, 0, len(b.keys))
	for name := range b.keys {
		names = append(names, name)
	}
	sort.Strings(names)

	keys := make([]SSHKey, 0, len(names))
	for _, name := range names {
		keys = append(keys, b.keys[name])
	}
	return keys, nil
}

func (b *MemoryBackend) RemoveSSHKey(name string) error {
	if _, ok := b.keys[name]; !ok {
		return fmt.Errorf("SSH key %q not found", name)
	}
	delete(b.keys, name)
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
			return r.runApps(args[1:], stdin, stdout, stderr)
		case "domains":
			return r.runDomains(args[1:], stdout, stderr)
		case "events":
			return r.runEvents(args[1:], stdout, stderr)
		case "logs":
			return r.runLogs(args[1:], stdout, stderr)
		case "releases":
			return r.runReleases(args[1:], stdout, stderr)
		case "server":
			return r.runServer(args[1:], stdout, stderr)
		case "ssh-keys":
			return r.runSSHKeys(args[1:], stdin, stdout, stderr)
		}
	}

	printUsage(stderr)
	return 2
}

func (r *Runner) runApps(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 2 && args[0] == "create" {
		app, remoteURL, err := r.backend.CreateApp(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		fmt.Fprintf(stdout, "created app %s\n", app.Name)
		fmt.Fprintf(stdout, "git remote add rhumbase %s\n", remoteURL)
		fmt.Fprintln(stdout, "git push rhumbase main")
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
			printUsage(stderr)
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
	if len(args) == 2 && args[0] == "list" {
		domains, err := r.backend.ListDomains(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(domains) == 0 {
			fmt.Fprintln(stdout, "no domains")
			return 0
		}
		for _, domain := range domains {
			fmt.Fprintf(stdout, "%s\t%s\t%d\t%t\n", domain.DomainName, domain.ServiceName, domain.Port, domain.HTTPS)
		}
		return 0
	}

	if len(args) == 3 && args[0] == "detach" {
		if err := r.backend.DetachDomain(args[1], args[2]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "detached %s from %s\n", args[2], args[1])
		return 0
	}

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

func (r *Runner) runEvents(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 2 && args[0] == "list" {
		events, err := r.backend.ListEvents(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(events) == 0 {
			fmt.Fprintln(stdout, "no events")
			return 0
		}
		for _, event := range events {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", formatCLITime(event.CreatedAt), event.Type, event.Message)
		}
		return 0
	}

	printUsage(stderr)
	return 2
}

func (r *Runner) runLogs(args []string, stdout io.Writer, stderr io.Writer) int {
	request, ok := parseLogsArgs(args)
	if !ok {
		printUsage(stderr)
		return 2
	}
	if err := r.backend.Logs(request, stdout, stderr); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func (r *Runner) runReleases(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 2 && args[0] == "list" {
		releases, err := r.backend.ListReleases(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(releases) == 0 {
			fmt.Fprintln(stdout, "no releases")
			return 0
		}
		for _, release := range releases {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", release.ID, release.Status, release.CommitSHA, formatCLITime(release.CreatedAt), release.ComposePath)
		}
		return 0
	}

	printUsage(stderr)
	return 2
}

func (r *Runner) runServer(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 3 && args[0] == "domain" && args[1] == "set" {
		baseDomain, err := domaincfg.NormalizeBaseDomain(args[2])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := r.backend.SetServerGitHost(baseDomain); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "server base domain set to %s\n", baseDomain)
		fmt.Fprintf(stdout, "control host: %s\n", domaincfg.ControlHost(baseDomain))
		fmt.Fprintf(stdout, "app host pattern: <app>.%s\n", baseDomain)
		return 0
	}

	printUsage(stderr)
	return 2
}

func (r *Runner) runSSHKeys(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "list" {
		keys, err := r.backend.ListSSHKeys()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(keys) == 0 {
			fmt.Fprintln(stdout, "no SSH keys")
			return 0
		}
		for _, key := range keys {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", key.Name, sshKeyFingerprint(key.PublicKey), formatCLITime(key.CreatedAt))
		}
		return 0
	}

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

	if len(args) == 2 && args[0] == "remove" {
		if err := r.backend.RemoveSSHKey(args[1]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "removed SSH key %s\n", args[1])
		return 0
	}

	printUsage(stderr)
	return 2
}

func printUsage(stderr io.Writer) {
	fmt.Fprintln(stderr, "usage: rhumbase version | diagnostics | logs <app> [service] [-f] | releases list <app> | events list <app> | apps create <name> | apps list | apps info <name> | apps restart <name> [service] | apps redeploy <name> | apps rollback <name> <release-id> | apps remove <name> [--force] | domains attach <app> <service> <domain> --port <port> | domains list <app> | domains detach <app> <domain> | server domain set <domain> | ssh-keys add <name> | ssh-keys list | ssh-keys remove <name>")
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

func parseLogsArgs(args []string) (LogRequest, bool) {
	var request LogRequest
	for _, arg := range args {
		if arg == "-f" || arg == "--follow" {
			request.Follow = true
			continue
		}
		if request.AppName == "" {
			request.AppName = arg
			continue
		}
		if request.ServiceName == "" {
			request.ServiceName = arg
			continue
		}
		return LogRequest{}, false
	}
	return request, request.AppName != ""
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

func formatCLITime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
}

func sshKeyFingerprint(publicKey string) string {
	fields := strings.Fields(publicKey)
	if len(fields) < 2 {
		return "unknown"
	}
	decoded, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return "unknown"
	}
	sum := sha256.Sum256(decoded)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func filterReleases(releases []Release, appName string) []Release {
	filtered := releases[:0]
	for _, release := range releases {
		if release.AppName != appName {
			filtered = append(filtered, release)
		}
	}
	return filtered
}

func filterEvents(events []Event, appName string) []Event {
	filtered := events[:0]
	for _, event := range events {
		if event.AppName != appName {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func filterDomains(domains []Domain, appName string) []Domain {
	filtered := domains[:0]
	for _, domain := range domains {
		if domain.AppName != appName {
			filtered = append(filtered, domain)
		}
	}
	return filtered
}
