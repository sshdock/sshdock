package tui

import (
	"strconv"
	"strings"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

type AppListView struct {
	Items []AppListItem
}

type AppListItem struct {
	ID                     string
	Name                   string
	Status                 string
	NodeID                 string
	LatestDeploymentStatus string
	DomainCount            int
}

type AppDetailView struct {
	App         AppSummary
	Services    []ServiceView
	Domains     []DomainView
	Releases    []ReleaseView
	Deployments []DeploymentView
	Events      []EventView
	Health      HealthSummary
}

func (v AppDetailView) LatestDeployments(limit int) []DeploymentView {
	if limit <= 0 || limit >= len(v.Deployments) {
		return v.Deployments
	}

	return v.Deployments[len(v.Deployments)-limit:]
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
	OverallStatus           string
	CurrentMainCommit       string
	RouteStatus             string
	LatestDeploymentID      string
	LatestDeploymentCommit  string
	LatestDeploymentTrigger string
	LatestDeploymentStatus  string
	ServiceStatus           string
	LastFailureDeploymentID string
	LastFailure             string
	Checks                  []app.HealthCheck
}

type LogsView struct {
	Lines []string
}

func NewAppListView(apps []app.App, latestDeployments map[string]app.DeploymentStatus, domains map[string][]app.Domain) AppListView {
	items := make([]AppListItem, 0, len(apps))
	for _, model := range apps {
		item := AppListItem{
			ID:          model.ID,
			Name:        model.Name,
			Status:      string(model.Status),
			NodeID:      model.NodeID,
			DomainCount: len(domains[model.ID]),
		}
		item.LatestDeploymentStatus = string(latestDeployments[model.ID])
		items = append(items, item)
	}

	return AppListView{Items: items}
}

func NewAppDetailView(model app.App, report app.HealthReport, domains []app.Domain, releases []app.Release, deployments []app.Deployment, events []app.Event) AppDetailView {
	return AppDetailView{
		App:         newAppSummary(model),
		Services:    newServiceViews(report.Services),
		Domains:     newDomainViews(domains),
		Releases:    newReleaseViews(releases),
		Deployments: newDeploymentViews(deployments),
		Events:      newEventViews(events),
		Health:      newHealthSummaryFromReport(report),
	}
}

func NewLogsView(output string) LogsView {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}

	return LogsView{
		Lines: lines,
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

func newServiceViews(services []app.ServiceHealth) []ServiceView {
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

func newHealthSummaryFromReport(report app.HealthReport) HealthSummary {
	serviceStatus := "-"
	if report.ServiceCount > 0 {
		serviceStatus = strconv.Itoa(report.RunningServiceCount) + " running"
		if report.AttentionServiceCount > 0 {
			serviceStatus += ", " + strconv.Itoa(report.AttentionServiceCount) + " attention"
		}
	}
	return HealthSummary{
		OverallStatus:           report.Health,
		CurrentMainCommit:       report.CurrentMainCommit,
		RouteStatus:             report.RouteStatus,
		LatestDeploymentID:      report.LatestDeploymentID,
		LatestDeploymentCommit:  report.LatestDeploymentCommit,
		LatestDeploymentTrigger: string(report.LatestDeploymentTrigger),
		LatestDeploymentStatus:  string(report.LatestDeploymentStatus),
		ServiceStatus:           serviceStatus,
		LastFailureDeploymentID: report.LastFailureDeploymentID,
		LastFailure:             report.LastFailure,
		Checks:                  append([]app.HealthCheck(nil), report.Checks...),
	}
}
