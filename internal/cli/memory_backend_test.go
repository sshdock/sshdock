package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	domaincfg "github.com/sshdock/sshdock/internal/domain"
)

type MemoryBackend struct {
	gitHost        string
	baseDomain     string
	apps           map[string]App
	releases       []Release
	deployments    []Deployment
	events         []Event
	domains        []Domain
	keys           map[string]SSHKey
	config         map[string]map[string]string
	logOutput      string
	logRequests    []LogRequest
	deploymentLogs map[string]string
}

func NewMemoryBackend(gitHost string) *MemoryBackend {
	if gitHost == "" {
		gitHost = "server"
	}

	return &MemoryBackend{
		gitHost:        gitHost,
		apps:           map[string]App{},
		keys:           map[string]SSHKey{},
		config:         map[string]map[string]string{},
		deploymentLogs: map[string]string{},
	}
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
		return App{}, appNotFoundError(name)
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

func (b *MemoryBackend) RemoveApp(name string) error {
	if _, ok := b.apps[name]; !ok {
		return fmt.Errorf("app %q not found", name)
	}
	delete(b.apps, name)
	b.releases = filterReleases(b.releases, name)
	b.deployments = filterDeployments(b.deployments, name)
	b.domains = filterDomains(b.domains, name)
	return nil
}

func (b *MemoryBackend) AppHealth(name string) (AppHealth, error) {
	model, ok := b.apps[name]
	if !ok {
		return AppHealth{}, appNotFoundError(name)
	}
	report := AppHealth{
		AppName:     model.Name,
		Status:      appmodel.AppStatus(model.Status),
		NodeID:      model.NodeID,
		DomainCount: len(memoryDomainsForApp(b.domains, name)),
	}
	report.Checks = append(report.Checks, healthCheckForAppStatus(model.Status))
	if release, ok := latestCLIRelease(memoryReleasesForApp(b.releases, name)); ok {
		report.AttemptReleaseID = release.ID
		report.AttemptReleaseStatus = appmodel.ReleaseStatus(release.Status)
		report.Checks = append(report.Checks, healthCheckForRelease(release.ID, release.Status))
		if release.Failure != "" {
			report.LastFailure = release.Failure
		}
	} else {
		report.Checks = append(report.Checks, HealthCheck{Status: "warn", Name: "release", Detail: "no releases"})
	}
	report.Checks = append(report.Checks, healthCheckForDomains(report.DomainCount))
	report.Health = overallHealth(report.Checks)
	return report, nil
}

func (b *MemoryBackend) Logs(request LogRequest, stdout io.Writer, _ io.Writer) error {
	if _, ok := b.apps[request.AppName]; !ok {
		return fmt.Errorf("app %q not found", request.AppName)
	}
	b.logRequests = append(b.logRequests, request)
	_, err := fmt.Fprint(stdout, b.logOutput)
	return err
}

func (b *MemoryBackend) DeploymentLogs(request DeploymentLogRequest, stdout io.Writer) error {
	if _, ok := b.apps[request.AppName]; !ok {
		return fmt.Errorf("app %q not found", request.AppName)
	}
	deploymentID := request.DeploymentID
	if deploymentID == "" {
		deployments, err := b.ListDeployments(request.AppName)
		if err != nil {
			return err
		}
		if len(deployments) == 0 {
			return fmt.Errorf("no deployments for app %q", request.AppName)
		}
		deploymentID = deployments[len(deployments)-1].ID
	}
	content, ok := b.deploymentLogs[deploymentID]
	if !ok {
		return fmt.Errorf("deployment %q has no stored deployment log", deploymentID)
	}
	_, err := fmt.Fprint(stdout, content)
	return err
}

func (b *MemoryBackend) ListReleases(appName string) ([]Release, error) {
	if _, ok := b.apps[appName]; !ok {
		return nil, appNotFoundError(appName)
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

func (b *MemoryBackend) ListDeployments(appName string) ([]Deployment, error) {
	if _, ok := b.apps[appName]; !ok {
		return nil, appNotFoundError(appName)
	}
	var deployments []Deployment
	for _, deployment := range b.deployments {
		if deployment.AppName == appName {
			deployments = append(deployments, deployment)
		}
	}
	sort.Slice(deployments, func(i, j int) bool {
		if deployments[i].StartedAt.Equal(deployments[j].StartedAt) {
			return deployments[i].ID < deployments[j].ID
		}
		return deployments[i].StartedAt.Before(deployments[j].StartedAt)
	})
	return deployments, nil
}

func (b *MemoryBackend) ListEvents(appName string) ([]Event, error) {
	var events []Event
	for _, event := range b.events {
		if event.AppName == appName {
			events = append(events, event)
		}
	}
	if _, ok := b.apps[appName]; !ok && len(events) == 0 {
		return nil, appNotFoundError(appName)
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
		return nil, appNotFoundError(appName)
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

func (b *MemoryBackend) CheckDomains(appName string) ([]DomainCheck, error) {
	if _, ok := b.apps[appName]; !ok {
		return nil, appNotFoundError(appName)
	}
	domains := memoryDomainsForApp(b.domains, appName)
	checks := make([]DomainCheck, 0, len(domains))
	for _, domain := range domains {
		checks = append(checks, DomainCheck{
			DomainName:  domain.DomainName,
			ServiceName: domain.ServiceName,
			Port:        domain.Port,
			HTTPS:       domain.HTTPS,
			Status:      "stored",
			Detail:      "router check unavailable",
		})
	}
	return checks, nil
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

func (b *MemoryBackend) SetConfig(appName string, name string, value []byte) error {
	if _, ok := b.apps[appName]; !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("config key name is required")
	}
	if b.config[appName] == nil {
		b.config[appName] = map[string]string{}
	}
	b.config[appName][name] = string(value)
	return nil
}

func (b *MemoryBackend) ImportConfig(appName string, input io.Reader) (int, error) {
	if _, ok := b.apps[appName]; !ok {
		return 0, fmt.Errorf("app %q not found", appName)
	}
	return appconfig.ImportLines(input, func(name string, value []byte) error {
		return b.SetConfig(appName, name, value)
	})
}

func (b *MemoryBackend) ListConfig(appName string) ([]ConfigEntry, error) {
	if _, ok := b.apps[appName]; !ok {
		return nil, fmt.Errorf("app %q not found", appName)
	}
	values := b.config[appName]
	entries := make([]ConfigEntry, 0, len(values))
	for name := range values {
		entries = append(entries, ConfigEntry{Name: name, Status: "set", RedactedValue: "<redacted>"})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func (b *MemoryBackend) GetConfig(appName string, name string) (string, error) {
	if _, ok := b.apps[appName]; !ok {
		return "", fmt.Errorf("app %q not found", appName)
	}
	value, ok := b.config[appName][name]
	if !ok {
		return "", fmt.Errorf("config %q not found for app %q", name, appName)
	}
	return value, nil
}

func (b *MemoryBackend) UnsetConfig(appName string, name string) error {
	if _, ok := b.apps[appName]; !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	if _, ok := b.config[appName][name]; !ok {
		return fmt.Errorf("config %q not found for app %q", name, appName)
	}
	delete(b.config[appName], name)
	return nil
}

func latestCLIRelease(releases []Release) (Release, bool) {
	if len(releases) == 0 {
		return Release{}, false
	}
	sort.Slice(releases, func(i, j int) bool {
		if releases[i].CreatedAt.Equal(releases[j].CreatedAt) {
			return releases[i].ID < releases[j].ID
		}
		return releases[i].CreatedAt.Before(releases[j].CreatedAt)
	})
	return releases[len(releases)-1], true
}

func memoryReleasesForApp(releases []Release, appName string) []Release {
	filtered := make([]Release, 0, len(releases))
	for _, release := range releases {
		if release.AppName == appName {
			filtered = append(filtered, release)
		}
	}
	return filtered
}

func memoryDomainsForApp(domains []Domain, appName string) []Domain {
	filtered := make([]Domain, 0, len(domains))
	for _, domain := range domains {
		if domain.AppName == appName {
			filtered = append(filtered, domain)
		}
	}
	return filtered
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

func filterDeployments(deployments []Deployment, appName string) []Deployment {
	filtered := deployments[:0]
	for _, deployment := range deployments {
		if deployment.AppName != appName {
			filtered = append(filtered, deployment)
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

func (b *MemoryBackend) CreateApp(name string) (App, string, error) {
	if err := validateAppName(name, b.gitHost); err != nil {
		return App{}, "", err
	}
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
