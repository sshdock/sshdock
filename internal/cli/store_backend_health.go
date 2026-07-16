package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
)

func (b *StoreBackend) AppHealth(name string) (AppHealth, error) {
	ctx := context.Background()
	model, err := b.store.GetApp(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return AppHealth{}, fmt.Errorf("app %q not found", name)
	}
	if err != nil {
		return AppHealth{}, fmt.Errorf("get app %q: %w", name, err)
	}
	releases, err := b.store.ListReleasesByApp(ctx, name)
	if err != nil {
		return AppHealth{}, fmt.Errorf("list releases for app %q: %w", name, err)
	}
	deployments, err := b.store.ListDeploymentsByApp(ctx, name)
	if err != nil {
		return AppHealth{}, fmt.Errorf("list deployments for app %q: %w", name, err)
	}
	domains, err := b.store.ListDomainsByApp(ctx, name)
	if err != nil {
		return AppHealth{}, fmt.Errorf("list domains for app %q: %w", name, err)
	}
	redactionValues, err := b.configRedactionValues(ctx, name)
	if err != nil {
		return AppHealth{}, err
	}

	report := AppHealth{AppName: model.Name, Status: string(model.Status), NodeID: model.NodeID, DomainCount: len(domains)}
	report.Checks = append(report.Checks, healthCheckForAppStatus(string(model.Status)))
	if release, ok := latestAppRelease(releases); ok {
		report.LatestReleaseID = release.ID
		report.LatestReleaseStatus = string(release.Status)
		report.Checks = append(report.Checks, healthCheckForRelease(release.ID, string(release.Status)))
	} else {
		report.Checks = append(report.Checks, HealthCheck{Status: "warn", Name: "release", Detail: "no releases"})
	}
	if deployment, ok := latestAppDeployment(deployments); ok {
		report.LatestDeploymentID = deployment.ID
		report.LatestDeploymentStatus = string(deployment.Status)
		report.Checks = append(report.Checks, healthCheckForDeployment(deployment.ID, string(deployment.Status)))
		if deployment.ErrorMessage != "" {
			report.LastFailure = compose.RedactValues(deployment.ErrorMessage, redactionValues)
		}
	} else {
		report.Checks = append(report.Checks, healthCheckForDeployment("", ""))
	}
	report.Checks = append(report.Checks, healthCheckForDomains(report.DomainCount))

	if b.recoveryRunner != nil {
		projectDir, composePath, err := appmodel.CurrentComposeEntry(model)
		if err != nil {
			report.Checks = append(report.Checks, HealthCheck{Status: "warn", Name: "services", Detail: err.Error()})
		} else if err := b.addServiceStatus(ctx, name, projectDir, composePath, domains, &report); err != nil {
			return AppHealth{}, compose.RedactError(err, redactionValues)
		}
	}
	for index := range report.Checks {
		report.Checks[index].Detail = compose.RedactValues(report.Checks[index].Detail, redactionValues)
	}
	report.Health = overallHealth(report.Checks)
	return report, nil
}

func (b *StoreBackend) addServiceStatus(ctx context.Context, name string, projectDir string, composePath string, domains []appmodel.Domain, report *AppHealth) error {
	env, err := b.configEnv(ctx, name, projectDir)
	if err != nil {
		return err
	}
	services, err := b.recoveryRunner.Status(ctx, compose.StatusRequest{AppName: name, ProjectDir: projectDir, ComposePath: composePath, Env: env})
	if err != nil {
		return fmt.Errorf("load service status for app %q: %w", name, err)
	}
	report.ServiceCount = len(services)
	for _, service := range services {
		if service.State == "running" {
			report.RunningServiceCount++
		} else {
			report.AttentionServiceCount++
		}
	}
	report.Checks = append(report.Checks, healthCheckForServices(report.ServiceCount, report.RunningServiceCount, report.AttentionServiceCount))
	candidates := make([]string, 0, len(services)+len(domains))
	for _, domain := range domains {
		candidates = append(candidates, domain.ServiceName)
	}
	for _, service := range services {
		if service.State == "running" {
			candidates = append(candidates, service.Name)
		}
	}
	statusRequest := compose.StatusRequest{AppName: name, ProjectDir: projectDir, ComposePath: composePath, Env: env}
	nonRestarting, err := compose.NonRestartingServices(composePath, env, candidates)
	if inspector, ok := b.recoveryRunner.(interface {
		NonRestartingServices(context.Context, compose.StatusRequest, []string) ([]string, error)
	}); ok {
		if effective, effectiveErr := inspector.NonRestartingServices(ctx, statusRequest, candidates); effectiveErr == nil {
			nonRestarting = effective
			err = nil
		}
	}
	if err != nil {
		return err
	}
	report.Checks = append(report.Checks, healthCheckForRestartPolicy(nonRestarting))
	return nil
}

func healthCheckForRestartPolicy(services []string) HealthCheck {
	if len(services) == 0 {
		return HealthCheck{Status: "ok", Name: "restart policy", Detail: "configured for routed and running services"}
	}
	return HealthCheck{Status: "warn", Name: "restart policy", Detail: "default non-restarting policy: " + strings.Join(services, ", ") + "; set restart: unless-stopped or always for reboot recovery"}
}
