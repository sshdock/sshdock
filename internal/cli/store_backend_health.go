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

	report := AppHealth{AppName: model.Name, Status: model.Status, NodeID: model.NodeID, DomainCount: len(domains)}
	report.Checks = append(report.Checks, healthCheckForAppStatus(string(model.Status)))
	if b.currentMainResolver != nil {
		currentMain, resolveErr := b.currentMainResolver.ResolveCurrentMain(ctx, model.RepoPath)
		if resolveErr != nil {
			report.Checks = append(report.Checks, HealthCheck{Status: "warn", Name: "current main", Detail: resolveErr.Error() + "; push a commit to remote main or inspect the bare repository"})
		} else {
			report.CurrentMainCommit = currentMain
			report.Checks = append(report.Checks, HealthCheck{Status: "ok", Name: "current main", Detail: currentMain})
		}
	}
	latestDeployment, hasLatestDeployment := latestAppDeployment(deployments)
	release, hasRelease := latestAppRelease(releases)
	if hasLatestDeployment {
		for _, candidate := range releases {
			if candidate.ID == latestDeployment.ReleaseID {
				release = candidate
				hasRelease = true
				break
			}
		}
	}
	if hasRelease {
		report.AttemptReleaseID = release.ID
		report.AttemptReleaseStatus = release.Status
		report.Checks = append(report.Checks, healthCheckForRelease(release.ID, string(release.Status)))
	} else {
		report.Checks = append(report.Checks, HealthCheck{Status: "warn", Name: "release", Detail: "no releases"})
	}
	if hasLatestDeployment {
		report.LatestDeploymentID = latestDeployment.ID
		report.LatestDeploymentCommit = latestDeployment.CommitSHA
		report.LatestDeploymentTrigger = latestDeployment.Trigger
		report.LatestDeploymentStatus = latestDeployment.Status
		report.Checks = append(report.Checks, healthCheckForDeployment(latestDeployment.ID, string(latestDeployment.Status)))
	} else {
		report.Checks = append(report.Checks, healthCheckForDeployment("", ""))
	}
	var recentFailure appmodel.Deployment
	for _, deployment := range deployments {
		if deployment.Status != appmodel.DeploymentStatusFailed {
			continue
		}
		if recentFailure.ID == "" || deployment.StartedAt.After(recentFailure.StartedAt) || deployment.StartedAt.Equal(recentFailure.StartedAt) && deployment.ID > recentFailure.ID {
			recentFailure = deployment
		}
	}
	if recentFailure.ID != "" {
		report.LastFailureDeploymentID = recentFailure.ID
		summary := strings.TrimSpace(recentFailure.FailureDetail)
		if !strings.HasPrefix(summary, "stage=") {
			summary = strings.TrimSpace(recentFailure.ErrorMessage)
		}
		if !strings.HasPrefix(summary, "stage=") {
			parts := make([]string, 0, 3)
			if recentFailure.FailureStage != "" {
				parts = append(parts, "stage="+recentFailure.FailureStage)
			}
			if recentFailure.FailureDetail != "" {
				parts = append(parts, "detail="+recentFailure.FailureDetail)
			}
			if recentFailure.RetryGuidance != "" {
				parts = append(parts, "retry="+recentFailure.RetryGuidance)
			}
			if len(parts) > 0 {
				summary = strings.Join(parts, "; ")
			}
		}
		report.LastFailure = compose.RedactValues(summary, redactionValues)
	}
	report.Checks = append(report.Checks, healthCheckForDomains(report.DomainCount))
	routeChecks, err := b.CheckDomains(name)
	if err != nil {
		return AppHealth{}, err
	}
	addRouteHealth(routeChecks, &report)

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

func addRouteHealth(routes []DomainCheck, report *AppHealth) {
	if len(routes) == 0 {
		report.RouteStatus = "unrouted"
		report.Checks = append(report.Checks, HealthCheck{Status: "warn", Name: "routes", Detail: "unrouted"})
		return
	}

	checkStatus := "ok"
	attentionByStatus := make(map[string]int)
	for _, route := range routes {
		if route.Status == "ok" {
			report.ActiveRouteCount++
			continue
		}
		report.RouteAttentionCount++
		attentionByStatus[route.Status]++
		if route.Status == "failed" || route.Status == "missing" || route.Status == "mismatch" {
			checkStatus = "fail"
		} else if checkStatus == "ok" {
			checkStatus = "warn"
		}
	}
	report.RouteStatus = fmt.Sprintf("%d active, %d attention", report.ActiveRouteCount, report.RouteAttentionCount)
	if report.RouteAttentionCount > 0 {
		states := make([]string, 0, len(attentionByStatus))
		for _, status := range []string{"failed", "missing", "mismatch", "unavailable", "stored"} {
			if count := attentionByStatus[status]; count > 0 {
				states = append(states, fmt.Sprintf("%s=%d", status, count))
			}
		}
		report.RouteStatus += " (" + strings.Join(states, ", ") + ")"
	}
	report.Checks = append(report.Checks, HealthCheck{Status: checkStatus, Name: "routes", Detail: report.RouteStatus})
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
		report.Services = append(report.Services, appmodel.ServiceHealth{Name: service.Name, State: service.State})
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
		report.Checks = append(report.Checks, HealthCheck{Status: "warn", Name: "restart policy", Detail: "inspection unavailable: " + err.Error()})
		return nil
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
