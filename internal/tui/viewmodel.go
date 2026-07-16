package tui

import (
	"strconv"
	"strings"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

type AppListView struct {
	Items []AppListItem
}

type AppListItem struct {
	ID                  string
	Name                string
	Status              string
	NodeID              string
	LatestReleaseStatus string
	DomainCount         int
}

type AppDetailView struct {
	App         AppSummary
	Services    []ServiceView
	Domains     []DomainView
	Releases    []ReleaseView
	Deployments []DeploymentView
	Events      []EventView
	Health      HealthSummary
	Actions     []string
}

type AppSummary struct {
	ID     string
	Name   string
	NodeID string
	Status string
}

type ServiceView struct {
	Name  string
	State string
}

type DomainView struct {
	DomainName  string
	ServiceName string
	Target      string
	HTTPS       bool
}

type ReleaseView struct {
	ID          string
	CommitSHA   string
	ComposePath string
	Status      string
	CreatedAt   time.Time
}

type DeploymentView struct {
	ID            string
	ReleaseID     string
	CommitSHA     string
	Trigger       string
	Status        string
	StartedAt     time.Time
	FinishedAt    time.Time
	FailureStage  string
	FailureDetail string
	RetryGuidance string
	ErrorMessage  string
}

type EventView struct {
	Type      string
	Message   string
	CreatedAt time.Time
}

type HealthSummary struct {
	RouteStatus            string
	LatestDeploymentStatus string
	ServiceStatus          string
	LastFailure            string
}

type LogsView struct {
	AppID       string
	ServiceName string
	Lines       []string
}

func NewAppListView(apps []app.App, latestReleases map[string]app.Release, domains map[string][]app.Domain) AppListView {
	items := make([]AppListItem, 0, len(apps))
	for _, model := range apps {
		item := AppListItem{
			ID:          model.ID,
			Name:        model.Name,
			Status:      string(model.Status),
			NodeID:      model.NodeID,
			DomainCount: len(domains[model.ID]),
		}
		if latest, ok := latestReleases[model.ID]; ok {
			item.LatestReleaseStatus = string(latest.Status)
		}
		items = append(items, item)
	}

	return AppListView{Items: items}
}

func NewAppDetailView(model app.App, services []compose.ServiceStatus, domains []app.Domain, releases []app.Release, deployments []app.Deployment, events []app.Event) AppDetailView {
	return AppDetailView{
		App:         newAppSummary(model),
		Services:    newServiceViews(services),
		Domains:     newDomainViews(domains),
		Releases:    newReleaseViews(releases),
		Deployments: newDeploymentViews(deployments),
		Events:      newEventViews(events),
		Health:      newHealthSummary(services, domains, deployments),
		Actions:     []string{"restart app", "redeploy current main", "attach domain"},
	}
}

func NewLogsView(appID string, serviceName string, output string) LogsView {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}

	return LogsView{
		AppID:       appID,
		ServiceName: serviceName,
		Lines:       lines,
	}
}

func newAppSummary(model app.App) AppSummary {
	return AppSummary{
		ID:     model.ID,
		Name:   model.Name,
		NodeID: model.NodeID,
		Status: string(model.Status),
	}
}

func newServiceViews(services []compose.ServiceStatus) []ServiceView {
	views := make([]ServiceView, 0, len(services))
	for _, service := range services {
		views = append(views, ServiceView{Name: service.Name, State: service.State})
	}
	return views
}

func newDomainViews(domains []app.Domain) []DomainView {
	views := make([]DomainView, 0, len(domains))
	for _, domain := range domains {
		views = append(views, DomainView{
			DomainName:  domain.DomainName,
			ServiceName: domain.ServiceName,
			Target:      domain.ServiceName + ":" + strconv.Itoa(domain.Port),
			HTTPS:       domain.HTTPS,
		})
	}
	return views
}

func newReleaseViews(releases []app.Release) []ReleaseView {
	views := make([]ReleaseView, 0, len(releases))
	for _, release := range releases {
		views = append(views, ReleaseView{
			ID:          release.ID,
			CommitSHA:   release.CommitSHA,
			ComposePath: release.ComposePath,
			Status:      string(release.Status),
			CreatedAt:   release.CreatedAt,
		})
	}
	return views
}

func newDeploymentViews(deployments []app.Deployment) []DeploymentView {
	views := make([]DeploymentView, 0, len(deployments))
	for _, deployment := range deployments {
		views = append(views, DeploymentView{
			ID:            deployment.ID,
			ReleaseID:     deployment.ReleaseID,
			CommitSHA:     deployment.CommitSHA,
			Trigger:       string(deployment.Trigger),
			Status:        string(deployment.Status),
			StartedAt:     deployment.StartedAt,
			FinishedAt:    deployment.FinishedAt,
			FailureStage:  deployment.FailureStage,
			FailureDetail: deployment.FailureDetail,
			RetryGuidance: deployment.RetryGuidance,
			ErrorMessage:  deployment.ErrorMessage,
		})
	}
	return views
}

func newEventViews(events []app.Event) []EventView {
	views := make([]EventView, 0, len(events))
	for _, event := range events {
		views = append(views, EventView{
			Type:      event.Type,
			Message:   event.Message,
			CreatedAt: event.CreatedAt,
		})
	}
	return views
}

func newHealthSummary(services []compose.ServiceStatus, domains []app.Domain, deployments []app.Deployment) HealthSummary {
	summary := HealthSummary{
		RouteStatus:            "unrouted",
		LatestDeploymentStatus: "-",
		ServiceStatus:          "-",
	}
	if len(domains) > 0 {
		summary.RouteStatus = "routed"
	}
	if len(deployments) > 0 {
		latest := deployments[len(deployments)-1]
		summary.LatestDeploymentStatus = string(latest.Status)
		for index := len(deployments) - 1; index >= 0; index-- {
			deployment := deployments[index]
			if deployment.ErrorMessage != "" {
				summary.LastFailure = deployment.ErrorMessage
				break
			}
		}
	}
	if len(services) > 0 {
		running := 0
		for _, service := range services {
			if service.State == "running" {
				running++
			}
		}
		if running == len(services) {
			summary.ServiceStatus = strconv.Itoa(running) + " running"
		} else {
			summary.ServiceStatus = strconv.Itoa(running) + " running, " + strconv.Itoa(len(services)-running) + " attention"
		}
	}
	return summary
}
