package diagnostics

import (
	"context"
	"strings"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/store"
)

func (r *Report) checkRestartPolicies(ctx context.Context, cfg config.Config, executor CommandExecutor, sqlite *store.SQLiteStore) {
	apps, err := sqlite.ListApps(ctx)
	if err != nil {
		r.addWarning("restart policies", "unable to list apps: "+err.Error())
		return
	}
	configService := appconfig.NewService(sqlite, cfg.ConfigKeyPath)
	runner := compose.NewDockerRunner(diagnosticComposeExecutor{executor: executor})
	for _, model := range apps {
		projectDir, composePath, err := appmodel.CurrentComposeEntry(model)
		if err != nil {
			continue
		}
		domains, err := sqlite.ListDomainsByApp(ctx, model.ID)
		if err != nil {
			r.addWarning("restart policy "+model.ID, "unable to list routes: "+err.Error())
			continue
		}
		candidates := make([]string, 0, len(domains))
		for _, domain := range domains {
			candidates = append(candidates, domain.ServiceName)
		}
		env, err := configService.ResolveAppConfig(ctx, model.ID)
		if err != nil {
			r.addWarning("restart policy "+model.ID, "unable to resolve app config: "+err.Error())
			continue
		}
		request := compose.StatusRequest{AppName: model.ID, ProjectDir: projectDir, ComposePath: composePath, Env: env}
		services, err := runner.Status(ctx, request)
		if err == nil {
			for _, service := range services {
				if service.State == "running" {
					candidates = append(candidates, service.Name)
				}
			}
		}
		nonRestarting, err := runner.NonRestartingServices(ctx, request, candidates)
		if err != nil {
			nonRestarting, err = compose.NonRestartingServices(composePath, env, candidates)
		}
		if err != nil {
			r.addWarning("restart policy "+model.ID, err.Error())
			continue
		}
		if len(nonRestarting) > 0 {
			r.addWarning("restart policy "+model.ID, strings.Join(nonRestarting, ", ")+" use the default non-restarting policy; set restart: unless-stopped or always for reboot recovery")
		}
	}
}

type diagnosticComposeExecutor struct {
	executor CommandExecutor
}

func (e diagnosticComposeExecutor) Run(ctx context.Context, command compose.Command) (string, error) {
	return e.executor.Run(ctx, Command{Name: command.Name, Args: command.Args, Dir: command.Dir, Env: command.Env})
}
