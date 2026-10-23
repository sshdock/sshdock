package cli

import (
	"context"
	"errors"
	"fmt"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
)

type composePreflightRunner interface {
	Preflight(context.Context, compose.PreflightRequest) (compose.PreflightReport, error)
}

func (b *StoreBackend) Preflight(name string) (compose.PreflightReport, error) {
	ctx := context.Background()
	model, err := b.store.GetApp(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return compose.PreflightReport{}, appNotFoundError(name)
	}
	if err != nil {
		return compose.PreflightReport{}, fmt.Errorf("get app %q: %w", name, err)
	}
	projectDir, _, err := appmodel.CurrentComposeEntry(model)
	if err != nil {
		return compose.PreflightReport{Scope: "server", Checks: []compose.PreflightCheck{{
			Name: "compose file", Status: "fail", Detail: err.Error(), Blocking: true,
			Fix: "restore a worktree with exactly one supported Compose file, or explicitly redeploy current remote main",
		}}}, nil
	}
	checker, ok := b.recoveryRunner.(composePreflightRunner)
	if !ok {
		return compose.PreflightReport{}, fmt.Errorf("Compose preflight runner is not configured")
	}
	if b.configManager == nil {
		return compose.PreflightReport{}, fmt.Errorf("config manager is not configured")
	}
	redactionValues, err := b.configRedactionValues(ctx, name)
	if err != nil {
		return compose.PreflightReport{}, err
	}
	env, err := b.configEnv(ctx, name, projectDir)
	if err != nil {
		return compose.PreflightReport{}, compose.RedactError(err, redactionValues)
	}
	report, err := checker.Preflight(ctx, compose.PreflightRequest{
		AppName: name, ProjectDir: projectDir, Env: env, Scope: "server", RedactionValues: redactionValues,
	})
	if err != nil {
		return compose.PreflightReport{}, compose.RedactError(err, redactionValues)
	}
	caddy := compose.PreflightCheck{
		Name: "Caddy", Status: "unknown", Detail: "active Caddy config inspection is unavailable; reload and public reachability are not verified",
		Fix: "run sudo sshdock diagnostics and sshdock domains check " + name,
	}
	if reader, ok := b.router.(routeReader); ok {
		if _, err := reader.Routes(ctx); err != nil {
			caddy.Status = "warn"
			caddy.Detail = "active Caddy config could not be read: " + err.Error() + "; reload and public reachability are not verified"
		} else {
			caddy.Status = "ok"
			caddy.Detail = "active Caddy config is readable; reload and public reachability are not verified"
		}
	}
	report.Checks = append(report.Checks, caddy)
	for index := range report.Checks {
		report.Checks[index].Detail = compose.RedactValues(report.Checks[index].Detail, redactionValues)
		report.Checks[index].Fix = compose.RedactValues(report.Checks[index].Fix, redactionValues)
	}
	return report, nil
}
