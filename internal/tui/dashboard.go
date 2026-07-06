package tui

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

type DashboardStore interface {
	ListApps(ctx context.Context) ([]app.App, error)
	ListReleasesByApp(ctx context.Context, appID string) ([]app.Release, error)
	ListDomainsByApp(ctx context.Context, appID string) ([]app.Domain, error)
	ListDeploymentsByApp(ctx context.Context, appID string) ([]app.Deployment, error)
	ListEventsByApp(ctx context.Context, appID string) ([]app.Event, error)
}

type DashboardConfigResolver interface {
	ResolveAppConfig(ctx context.Context, appID string, projectDir string) (map[string]string, error)
}

type DashboardHandler struct {
	store          DashboardStore
	runner         compose.Runner
	configResolver DashboardConfigResolver
}

type DashboardSnapshot struct {
	Apps     AppListScreen
	AppOrder []string
	AppsByID map[string]DashboardAppSnapshot
}

type DashboardAppSnapshot struct {
	Detail AppDetailScreen
	Logs   map[string]LogsView
}

func NewDashboardHandler(store DashboardStore, runner compose.Runner) *DashboardHandler {
	return &DashboardHandler{store: store, runner: runner}
}

func NewDashboardHandlerWithConfig(store DashboardStore, runner compose.Runner, resolver DashboardConfigResolver) *DashboardHandler {
	return &DashboardHandler{store: store, runner: runner, configResolver: resolver}
}

func (h *DashboardHandler) HandleSession(ctx context.Context, session Session) error {
	return h.Render(ctx, session)
}

func (h *DashboardHandler) Render(ctx context.Context, writer io.Writer) error {
	snapshot, err := h.Snapshot(ctx)
	if err != nil {
		return err
	}
	return RenderDashboardSnapshot(writer, snapshot)
}

func (h *DashboardHandler) Snapshot(ctx context.Context) (DashboardSnapshot, error) {
	apps, err := h.store.ListApps(ctx)
	if err != nil {
		return DashboardSnapshot{}, fmt.Errorf("list apps: %w", err)
	}

	latestReleases := map[string]app.Release{}
	domainsByApp := map[string][]app.Domain{}
	appOrder := make([]string, 0, len(apps))
	appsByID := make(map[string]DashboardAppSnapshot, len(apps))
	for _, model := range apps {
		appOrder = append(appOrder, model.ID)
		releases, err := h.store.ListReleasesByApp(ctx, model.ID)
		if err != nil {
			return DashboardSnapshot{}, fmt.Errorf("list releases for %s: %w", model.ID, err)
		}
		if latest, ok := latestRelease(releases); ok {
			latestReleases[model.ID] = latest
		}

		domains, err := h.store.ListDomainsByApp(ctx, model.ID)
		if err != nil {
			return DashboardSnapshot{}, fmt.Errorf("list domains for %s: %w", model.ID, err)
		}
		domainsByApp[model.ID] = domains

		deployments, err := h.store.ListDeploymentsByApp(ctx, model.ID)
		if err != nil {
			return DashboardSnapshot{}, fmt.Errorf("list deployments for %s: %w", model.ID, err)
		}

		events, err := h.store.ListEventsByApp(ctx, model.ID)
		if err != nil {
			return DashboardSnapshot{}, fmt.Errorf("list events for %s: %w", model.ID, err)
		}

		services, logsByService, err := h.serviceStatusAndLogs(ctx, model, releases)
		if err != nil {
			return DashboardSnapshot{}, err
		}
		view := NewAppDetailView(model, services, domains, releases, deployments, events)
		appsByID[model.ID] = DashboardAppSnapshot{
			Detail: NewAppDetailScreen(view),
			Logs:   logsByService,
		}
	}

	return DashboardSnapshot{
		Apps:     NewAppListScreen(NewAppListView(apps, latestReleases, domainsByApp)),
		AppOrder: appOrder,
		AppsByID: appsByID,
	}, nil
}

func RenderDashboardSnapshot(writer io.Writer, snapshot DashboardSnapshot) error {
	if _, err := fmt.Fprintln(writer, "SSHDock Dashboard"); err != nil {
		return err
	}
	if err := renderAppList(writer, snapshot.Apps); err != nil {
		return err
	}

	for _, appID := range snapshot.AppOrder {
		appSnapshot := snapshot.AppsByID[appID]
		if err := renderAppDetail(writer, appSnapshot.Detail, appSnapshot.Logs); err != nil {
			return err
		}
	}

	return nil
}

func (h *DashboardHandler) serviceStatusAndLogs(ctx context.Context, model app.App, releases []app.Release) ([]compose.ServiceStatus, map[string]LogsView, error) {
	logsByService := map[string]LogsView{}
	if h.runner == nil {
		return nil, logsByService, nil
	}
	latest, ok := latestRelease(releases)
	if !ok || latest.ComposePath == "" {
		return nil, logsByService, nil
	}

	projectDir := filepath.Dir(latest.ComposePath)
	env, err := h.resolveConfigEnv(ctx, model.ID, projectDir)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve config for %s: %w", model.ID, err)
	}
	services, err := h.runner.Status(ctx, compose.StatusRequest{
		AppName:     model.ID,
		ProjectDir:  projectDir,
		ComposePath: latest.ComposePath,
		Env:         env,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("load service status for %s: %w", model.ID, err)
	}

	for _, service := range services {
		output, err := h.runner.Logs(ctx, compose.LogsRequest{
			AppName:     model.ID,
			ProjectDir:  projectDir,
			ComposePath: latest.ComposePath,
			ServiceName: service.Name,
			Lines:       50,
			Env:         env,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("load logs for %s/%s: %w", model.ID, service.Name, err)
		}
		logsByService[service.Name] = NewLogsView(model.ID, service.Name, compose.RedactValues(output, env))
	}

	return services, logsByService, nil
}

func (h *DashboardHandler) resolveConfigEnv(ctx context.Context, appID string, projectDir string) (map[string]string, error) {
	if h.configResolver == nil {
		return nil, nil
	}
	return h.configResolver.ResolveAppConfig(ctx, appID, projectDir)
}

func renderAppList(writer io.Writer, screen AppListScreen) error {
	if _, err := fmt.Fprintln(writer); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "Apps"); err != nil {
		return err
	}
	if screen.Empty() {
		_, err := fmt.Fprintln(writer, screen.EmptyMessage())
		return err
	}

	for _, row := range screen.Rows() {
		if _, err := fmt.Fprintf(writer, "- %s status=%s node=%s latest=%s domains=%d\n", row.Name, row.Status, row.NodeID, valueOrDash(row.LatestReleaseStatus), row.DomainCount); err != nil {
			return err
		}
	}
	return nil
}

func renderAppDetail(writer io.Writer, screen AppDetailScreen, logsByService map[string]LogsView) error {
	metadata := screen.Metadata()
	if _, err := fmt.Fprintf(writer, "\nApp %s\n", metadata.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Status: %s\nNode: %s\n", metadata.Status, metadata.NodeID); err != nil {
		return err
	}

	if err := renderServices(writer, screen.Services()); err != nil {
		return err
	}
	if err := renderDomains(writer, screen.Domains()); err != nil {
		return err
	}
	if err := renderReleases(writer, screen.Releases()); err != nil {
		return err
	}
	if err := renderDeployments(writer, screen.LatestDeployments(5)); err != nil {
		return err
	}
	if err := renderEvents(writer, screen.Events()); err != nil {
		return err
	}
	return renderLogs(writer, logsByService)
}

func renderServices(writer io.Writer, services []ServiceView) error {
	if _, err := fmt.Fprintln(writer, "Services"); err != nil {
		return err
	}
	if len(services) == 0 {
		_, err := fmt.Fprintln(writer, "- none")
		return err
	}
	for _, service := range services {
		if _, err := fmt.Fprintf(writer, "- %s %s\n", service.Name, service.State); err != nil {
			return err
		}
	}
	return nil
}

func renderDomains(writer io.Writer, domains []DomainView) error {
	if _, err := fmt.Fprintln(writer, "Domains"); err != nil {
		return err
	}
	if len(domains) == 0 {
		_, err := fmt.Fprintln(writer, "- none")
		return err
	}
	for _, domain := range domains {
		if _, err := fmt.Fprintf(writer, "- %s -> %s https=%t\n", domain.DomainName, domain.Target, domain.HTTPS); err != nil {
			return err
		}
	}
	return nil
}

func renderReleases(writer io.Writer, releases []ReleaseView) error {
	if _, err := fmt.Fprintln(writer, "Releases"); err != nil {
		return err
	}
	if len(releases) == 0 {
		_, err := fmt.Fprintln(writer, "- none")
		return err
	}
	for _, release := range releases {
		if _, err := fmt.Fprintf(writer, "- %s %s %s\n", release.ID, release.Status, release.CommitSHA); err != nil {
			return err
		}
	}
	return nil
}

func renderDeployments(writer io.Writer, deployments []DeploymentView) error {
	if _, err := fmt.Fprintln(writer, "Deployments"); err != nil {
		return err
	}
	if len(deployments) == 0 {
		_, err := fmt.Fprintln(writer, "- none")
		return err
	}
	for _, deployment := range deployments {
		if _, err := fmt.Fprintf(writer, "- %s %s %s\n", deployment.ID, deployment.Status, deployment.ReleaseID); err != nil {
			return err
		}
	}
	return nil
}

func renderEvents(writer io.Writer, events []EventView) error {
	if _, err := fmt.Fprintln(writer, "Events"); err != nil {
		return err
	}
	if len(events) == 0 {
		_, err := fmt.Fprintln(writer, "- none")
		return err
	}
	for _, event := range events {
		if _, err := fmt.Fprintf(writer, "- %s %s\n", event.Type, event.Message); err != nil {
			return err
		}
	}
	return nil
}

func renderLogs(writer io.Writer, logsByService map[string]LogsView) error {
	if len(logsByService) == 0 {
		return nil
	}

	names := make([]string, 0, len(logsByService))
	for name := range logsByService {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		screen := NewLogsScreen(logsByService[name])
		if _, err := fmt.Fprintf(writer, "Logs %s\n", name); err != nil {
			return err
		}
		for _, line := range screen.Lines() {
			if _, err := fmt.Fprintln(writer, line); err != nil {
				return err
			}
		}
	}
	return nil
}

func latestRelease(releases []app.Release) (app.Release, bool) {
	if len(releases) == 0 {
		return app.Release{}, false
	}
	return releases[len(releases)-1], true
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
