package cli

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	appmodel "github.com/sshdock/sshdock/internal/app"
	domaincfg "github.com/sshdock/sshdock/internal/domain"
)

type App struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	NodeID     string `json:"node_id"`
	DefaultURL string `json:"default_url"`
}

type Domain struct {
	AppName     string `json:"app_name"`
	ServiceName string `json:"service_name"`
	DomainName  string `json:"domain_name"`
	Port        int    `json:"port"`
	HTTPS       bool   `json:"https"`
}

type SSHKey struct {
	Name      string
	PublicKey string
	CreatedAt time.Time
}

type Release struct {
	ID          string    `json:"id"`
	AppName     string    `json:"app_name"`
	CommitSHA   string    `json:"commit_sha"`
	ComposePath string    `json:"compose_path"`
	Status      string    `json:"status"`
	Failure     string    `json:"failure"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Deployment struct {
	ID            string    `json:"id"`
	AppName       string    `json:"app_name"`
	ReleaseID     string    `json:"release_id"`
	CommitSHA     string    `json:"commit_sha"`
	Trigger       string    `json:"trigger"`
	Status        string    `json:"status"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at,omitzero"`
	FailureStage  string    `json:"failure_stage"`
	FailureDetail string    `json:"failure_detail"`
	RetryGuidance string    `json:"retry_guidance"`
}

type Event struct {
	ID        string    `json:"id"`
	AppName   string    `json:"app_name"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type AppHealth = appmodel.HealthReport

type HealthCheck = appmodel.HealthCheck

type DomainCheck struct {
	DomainName  string `json:"domain_name"`
	ServiceName string `json:"service_name"`
	Port        int    `json:"port"`
	HTTPS       bool   `json:"https"`
	Status      string `json:"status"`
	Detail      string `json:"detail"`
}

type ConfigEntry struct {
	Name          string
	Status        string
	RedactedValue string
	UpdatedAt     time.Time
	MutatedBy     string
}

type LogRequest struct {
	AppName     string
	ServiceName string
	Follow      bool
	Lines       int
}

type Runner struct {
	newBackend          func() (Backend, error)
	backend             Backend
	version             string
	interactiveTerminal bool
}

func (r *Runner) WithInteractiveTerminal(interactive bool) *Runner {
	r.interactiveTerminal = interactive
	return r
}

func NewRunner(backend Backend, version string) *Runner {
	return &Runner{backend: backend, version: version}
}

func NewRunnerWithBackendFactory(newBackend func() (Backend, error), version string) *Runner {
	return &Runner{newBackend: newBackend, version: version}
}

func (r *Runner) ensureBackend() error {
	if r.backend == nil && r.newBackend != nil {
		backend, err := r.newBackend()
		if err != nil {
			return err
		}
		r.backend = backend
	}
	if r.backend == nil {
		return fmt.Errorf("command backend is not configured")
	}
	return nil
}

func (r *Runner) requireBackend(stderr io.Writer) bool {
	if err := r.ensureBackend(); err != nil {
		fmt.Fprintln(stderr, err)
		return false
	}
	return true
}

func (r *Runner) Run(args []string, stdout io.Writer, stderr io.Writer) int {
	return r.RunWithInput(args, nil, stdout, stderr)
}

func (r *Runner) RunWithInput(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	normalized, jsonOutput, err := ParseOutputArgs(args)
	if err != nil {
		if writeErr := WriteJSON(stdout, JSONCommandName(normalized), nil, err); writeErr != nil {
			fmt.Fprintln(stderr, writeErr)
			return 1
		}
		return 2
	}
	if jsonOutput {
		return r.runJSON(normalized, stdout, stderr)
	}
	args = normalized

	if len(args) == 0 || (len(args) == 1 && isHelpArg(args[0])) {
		printRootHelp(stdout)
		return 0
	}

	if args[0] == "help" {
		if len(args) == 1 {
			printRootHelp(stdout)
			return 0
		}
		if len(args) == 2 {
			return printHelpTopic(args[1], stdout, stderr)
		}
		printInvalidUsage(stderr, "help")
		return 2
	}

	if len(args) == 2 && isHelpArg(args[1]) {
		return printHelpTopic(args[0], stdout, stderr)
	}

	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintf(stdout, "sshdock %s\n", r.version)
		return 0
	}

	if len(args) >= 1 {
		switch args[0] {
		case "apps":
			return r.runApps(args[1:], stdin, stdout, stderr)
		case "config":
			return r.runConfig(args[1:], stdin, stdout, stderr)
		case "domains":
			return r.runDomains(args[1:], stdout, stderr)
		case "deployments":
			return r.runDeployments(args[1:], stdout, stderr)
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

	printUnknownCommand(stderr, args[0])
	return 2
}

func (r *Runner) runConfig(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 3 && args[0] == "set" {
		if !r.requireBackend(stderr) {
			return 1
		}
		if stdin == nil {
			stdin = strings.NewReader("")
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "read config value: %v\n", err)
			return 1
		}
		value := strings.TrimRight(string(data), "\r\n")
		if err := r.backend.SetConfig(args[1], args[2], []byte(value)); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "set config %s for %s\n", args[2], args[1])
		printConfigRedeployHint(stdout, args[1])
		return 0
	}

	if len(args) == 2 && args[0] == "import" {
		if !r.requireBackend(stderr) {
			return 1
		}
		count, err := r.backend.ImportConfig(args[1], stdin)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "imported %d config value(s) for %s\n", count, args[1])
		if count > 0 {
			printConfigRedeployHint(stdout, args[1])
		}
		return 0
	}

	if len(args) == 2 && args[0] == "list" {
		if !r.requireBackend(stderr) {
			return 1
		}
		entries, err := r.backend.ListConfig(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(entries) == 0 {
			fmt.Fprintln(stdout, "no config")
			return 0
		}
		for _, entry := range entries {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", entry.Name, entry.Status, entry.RedactedValue, formatCLITime(entry.UpdatedAt), entry.MutatedBy)
		}
		return 0
	}

	if len(args) == 2 && args[0] == "keys" {
		if !r.requireBackend(stderr) {
			return 1
		}
		entries, err := r.backend.ListConfig(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		for _, entry := range entries {
			fmt.Fprintln(stdout, entry.Name)
		}
		return 0
	}

	if len(args) == 3 && args[0] == "get" {
		if !r.requireBackend(stderr) {
			return 1
		}
		value, err := r.backend.GetConfig(args[1], args[2])
		if err != nil {
			if isConfigKeyPermissionDenied(err) {
				printConfigGetAccessGuidance(stderr, args[1], args[2])
				return 1
			}
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, value)
		return 0
	}

	if len(args) == 3 && args[0] == "unset" {
		if !r.requireBackend(stderr) {
			return 1
		}
		if err := r.backend.UnsetConfig(args[1], args[2]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "unset config %s for %s\n", args[2], args[1])
		printConfigRedeployHint(stdout, args[1])
		return 0
	}

	printInvalidUsage(stderr, "config")
	return 2
}

func printConfigRedeployHint(stdout io.Writer, appName string) {
	fmt.Fprintln(stdout, "redeploy required for running containers:")
	fmt.Fprintf(stdout, "  sudo sshdock apps redeploy %s\n", appName)
}

func isConfigKeyPermissionDenied(err error) bool {
	return errors.Is(err, os.ErrPermission) && strings.Contains(err.Error(), "config encryption key")
}

func printConfigGetAccessGuidance(stderr io.Writer, appName string, keyName string) {
	args := []string{"config", "get", appName, keyName}
	command := strings.Join(args, " ")
	fmt.Fprintln(stderr, "config get requires access to SSHDock's config encryption key.")
	fmt.Fprintln(stderr, "Run one of:")
	fmt.Fprintf(stderr, "  sudo sshdock %s\n", command)
	fmt.Fprintf(stderr, "  ssh sshdock@<host> %s\n", command)
}

func (r *Runner) runDomains(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 2 && args[0] == "list" {
		if !r.requireBackend(stderr) {
			return 1
		}
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

	if len(args) == 2 && args[0] == "check" {
		if !r.requireBackend(stderr) {
			return 1
		}
		checks, err := r.backend.CheckDomains(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(checks) == 0 {
			fmt.Fprintln(stdout, "no domains")
			return 0
		}
		for _, check := range checks {
			fmt.Fprintf(stdout, "%s\t%s\t%d\t%t\t%s\t%s\n", check.DomainName, check.ServiceName, check.Port, check.HTTPS, check.Status, check.Detail)
		}
		return 0
	}

	if len(args) == 3 && args[0] == "detach" {
		if !r.requireBackend(stderr) {
			return 1
		}
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

		if !r.requireBackend(stderr) {
			return 1
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

	printInvalidUsage(stderr, "domains")
	return 2
}

func printAppHealth(stdout io.Writer, report AppHealth) {
	fmt.Fprintf(stdout, "app: %s\n", report.AppName)
	fmt.Fprintf(stdout, "health: %s\n", report.Health)
	fmt.Fprintf(stdout, "status: %s\n", cliValueOrDash(string(report.Status)))
	fmt.Fprintf(stdout, "node: %s\n", cliValueOrDash(report.NodeID))
	fmt.Fprintf(stdout, "current main: %s\n", cliValueOrDash(report.CurrentMainCommit))
	if report.AttemptReleaseID != "" || report.AttemptReleaseStatus != "" {
		fmt.Fprintf(stdout, "attempt release: %s %s\n", cliValueOrDash(report.AttemptReleaseID), cliValueOrDash(string(report.AttemptReleaseStatus)))
	} else {
		fmt.Fprintln(stdout, "attempt release: -")
	}
	if report.LatestDeploymentID != "" || report.LatestDeploymentStatus != "" {
		fmt.Fprintf(stdout, "latest deploy: %s %s", cliValueOrDash(report.LatestDeploymentID), cliValueOrDash(string(report.LatestDeploymentStatus)))
		if report.LatestDeploymentCommit != "" {
			fmt.Fprintf(stdout, " commit=%s", report.LatestDeploymentCommit)
		}
		if report.LatestDeploymentTrigger != "" {
			fmt.Fprintf(stdout, " trigger=%s", report.LatestDeploymentTrigger)
		}
		fmt.Fprintln(stdout)
	} else {
		fmt.Fprintln(stdout, "latest deploy: -")
	}
	fmt.Fprintf(stdout, "domains: %d\n", report.DomainCount)
	fmt.Fprintf(stdout, "routes: %s\n", cliValueOrDash(report.RouteStatus))
	if report.ServiceCount > 0 {
		fmt.Fprintf(stdout, "services: %d running, %d attention\n", report.RunningServiceCount, report.AttentionServiceCount)
		for _, service := range report.Services {
			fmt.Fprintf(stdout, "service\t%s\t%s\n", cliField(service.Name), cliField(service.State))
		}
	} else {
		fmt.Fprintln(stdout, "services: -")
	}
	if report.LastFailure != "" {
		fmt.Fprint(stdout, "last failure: ")
		if report.LastFailureDeploymentID != "" {
			fmt.Fprintf(stdout, "%s ", report.LastFailureDeploymentID)
		}
		fmt.Fprintf(stdout, "%s\n", cliField(report.LastFailure))
	}
	fmt.Fprintln(stdout, "checks:")
	for _, check := range report.Checks {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", cliField(check.Status), cliField(check.Name), cliField(check.Detail))
	}
}

func (r *Runner) runEvents(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 2 && args[0] == "list" {
		if !r.requireBackend(stderr) {
			return 1
		}
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
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", formatCLITime(event.CreatedAt), cliField(event.Type), cliField(event.Message))
		}
		return 0
	}

	printInvalidUsage(stderr, "events")
	return 2
}

func (r *Runner) runDeployments(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 3 && args[0] == "info" {
		if !r.requireBackend(stderr) {
			return 1
		}
		deployment, err := r.deploymentInfo(args[1], args[2])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "id: %s\napp: %s\nrelease: %s\ncommit: %s\ntrigger: %s\nstatus: %s\nstarted: %s\nfinished: %s\nfailure stage: %s\nfailure detail: %s\nretry guidance: %s\n",
			cliField(deployment.ID), cliField(deployment.AppName), cliField(deployment.ReleaseID), cliField(deployment.CommitSHA),
			cliField(deployment.Trigger), cliField(deployment.Status), formatCLITime(deployment.StartedAt), formatCLITime(deployment.FinishedAt),
			cliField(deployment.FailureStage), cliField(deployment.FailureDetail), cliField(deployment.RetryGuidance))
		return 0
	}

	if len(args) >= 2 && args[0] == "logs" {
		request, ok := parseDeploymentLogArgs(args[1:])
		if !ok {
			printInvalidUsage(stderr, "deployments")
			return 2
		}
		if !r.requireBackend(stderr) {
			return 1
		}
		if err := r.backend.DeploymentLogs(request, stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if len(args) == 2 && args[0] == "list" {
		if !r.requireBackend(stderr) {
			return 1
		}
		deployments, err := r.backend.ListDeployments(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(deployments) == 0 {
			fmt.Fprintln(stdout, "no deployments")
			return 0
		}
		for _, deployment := range deployments {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				cliField(deployment.ID),
				cliField(deployment.Status),
				cliField(deployment.Trigger),
				cliField(deployment.CommitSHA),
				cliField(deployment.ReleaseID),
				formatCLITime(deployment.StartedAt),
				formatCLITime(deployment.FinishedAt),
				cliField(deployment.FailureStage),
				cliField(deployment.FailureDetail),
				cliField(deployment.RetryGuidance),
			)
		}
		return 0
	}

	printInvalidUsage(stderr, "deployments")
	return 2
}

func parseDeploymentLogArgs(args []string) (DeploymentLogRequest, bool) {
	request := DeploymentLogRequest{}
	for _, arg := range args {
		if arg == "-f" || arg == "--follow" {
			request.Follow = true
			continue
		}
		if request.AppName == "" {
			request.AppName = arg
			continue
		}
		if request.DeploymentID == "" {
			request.DeploymentID = arg
			continue
		}
		return DeploymentLogRequest{}, false
	}
	return request, request.AppName != ""
}

func (r *Runner) runLogs(args []string, stdout io.Writer, stderr io.Writer) int {
	request, ok := parseLogsArgs(args)
	if !ok {
		printInvalidUsage(stderr, "logs")
		return 2
	}
	if !r.requireBackend(stderr) {
		return 1
	}
	if err := r.backend.Logs(request, stdout, stderr); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func (r *Runner) runReleases(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 2 && args[0] == "list" {
		if !r.requireBackend(stderr) {
			return 1
		}
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
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s", release.ID, release.Status, release.CommitSHA, formatCLITime(release.CreatedAt), release.ComposePath)
			if release.Failure != "" {
				fmt.Fprintf(stdout, "\t%s", release.Failure)
			}
			fmt.Fprintln(stdout)
		}
		return 0
	}

	printInvalidUsage(stderr, "releases")
	return 2
}

func (r *Runner) runServer(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 3 && args[0] == "domain" && args[1] == "set" {
		baseDomain, err := domaincfg.NormalizeBaseDomain(args[2])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if !r.requireBackend(stderr) {
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

	printInvalidUsage(stderr, "server")
	return 2
}

func (r *Runner) runSSHKeys(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "list" {
		if !r.requireBackend(stderr) {
			return 1
		}
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
		if !r.requireBackend(stderr) {
			return 1
		}
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
		if !r.requireBackend(stderr) {
			return 1
		}
		if err := r.backend.RemoveSSHKey(args[1]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "removed SSH key %s\n", args[1])
		return 0
	}

	printInvalidUsage(stderr, "ssh-keys")
	return 2
}

func isHelpArg(arg string) bool {
	return arg == "-h" || arg == "--help"
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
	request := LogRequest{Lines: 100}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-f" || arg == "--follow" {
			request.Follow = true
			continue
		}
		if arg == "--tail" {
			if i+1 >= len(args) {
				return LogRequest{}, false
			}
			lines, err := strconv.Atoi(args[i+1])
			if err != nil || lines <= 0 {
				return LogRequest{}, false
			}
			request.Lines = lines
			i++
			continue
		}
		if strings.HasPrefix(arg, "--tail=") {
			lines, err := strconv.Atoi(strings.TrimPrefix(arg, "--tail="))
			if err != nil || lines <= 0 {
				return LogRequest{}, false
			}
			request.Lines = lines
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

func healthCheckForAppStatus(status string) HealthCheck {
	switch status {
	case "healthy":
		return HealthCheck{Status: "ok", Name: "app", Detail: "status healthy"}
	case "failed":
		return HealthCheck{Status: "fail", Name: "app", Detail: "status failed"}
	default:
		return HealthCheck{Status: "warn", Name: "app", Detail: "status " + cliValueOrDash(status)}
	}
}

func healthCheckForRelease(id string, status string) HealthCheck {
	detail := strings.TrimSpace(id + " " + status)
	switch status {
	case "succeeded":
		return HealthCheck{Status: "ok", Name: "release", Detail: detail}
	case "failed":
		return HealthCheck{Status: "fail", Name: "release", Detail: detail}
	default:
		return HealthCheck{Status: "warn", Name: "release", Detail: detail}
	}
}

func healthCheckForDeployment(id string, status string) HealthCheck {
	if id == "" && status == "" {
		return HealthCheck{Status: "warn", Name: "deployment", Detail: "no deployments"}
	}
	detail := strings.TrimSpace(id + " " + status)
	switch status {
	case "succeeded":
		return HealthCheck{Status: "ok", Name: "deployment", Detail: detail}
	case "failed":
		return HealthCheck{Status: "fail", Name: "deployment", Detail: detail}
	default:
		return HealthCheck{Status: "warn", Name: "deployment", Detail: detail}
	}
}

func healthCheckForDomains(count int) HealthCheck {
	if count == 0 {
		return HealthCheck{Status: "warn", Name: "domains", Detail: "none configured"}
	}
	return HealthCheck{Status: "ok", Name: "domains", Detail: fmt.Sprintf("%d configured", count)}
}

func healthCheckForServices(total int, running int, attention int) HealthCheck {
	if total == 0 {
		return HealthCheck{Status: "warn", Name: "services", Detail: "status unavailable"}
	}
	if attention > 0 {
		return HealthCheck{Status: "fail", Name: "services", Detail: fmt.Sprintf("%d running, %d attention", running, attention)}
	}
	return HealthCheck{Status: "ok", Name: "services", Detail: fmt.Sprintf("%d running", running)}
}

func overallHealth(checks []HealthCheck) string {
	health := "ok"
	for _, check := range checks {
		if check.Status == "fail" {
			return "fail"
		}
		if check.Status == "warn" || check.Status == "unknown" {
			health = "warn"
		}
	}
	return health
}

func cliValueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func cliField(value string) string {
	value = cliValueOrDash(value)
	return strings.Map(func(char rune) rune {
		if char < 32 || (char >= 127 && char <= 159) {
			return ' '
		}
		return char
	}, value)
}
