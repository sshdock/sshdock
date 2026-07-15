package cli

import (
	"context"
	"errors"
	"fmt"

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
		release, ok, err := b.latestRuntimeRelease(ctx, name)
		if err != nil {
			return AppHealth{}, fmt.Errorf("find runtime release for app %q: %w", name, err)
		}
		if ok && isRunnableReleaseStatus(release.Status) && release.ComposePath != "" {
			if err := b.addServiceStatus(ctx, name, model, release, &report); err != nil {
				return AppHealth{}, compose.RedactError(err, redactionValues)
			}
		}
	}
	for index := range report.Checks {
		report.Checks[index].Detail = compose.RedactValues(report.Checks[index].Detail, redactionValues)
	}
	report.Health = overallHealth(report.Checks)
	return report, nil
}

func (b *StoreBackend) addServiceStatus(ctx context.Context, name string, model appmodel.App, release appmodel.Release, report *AppHealth) error {
	projectDir := projectDirFromModel(model, release)
	env, err := b.configEnv(ctx, name, projectDir)
	if err != nil {
		return err
	}
	services, err := b.recoveryRunner.Status(ctx, compose.StatusRequest{AppName: name, ProjectDir: projectDir, ComposePath: release.ComposePath, Env: env})
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
	return nil
}
