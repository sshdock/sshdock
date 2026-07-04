package tui

type AppDetailScreen struct {
	view AppDetailView
}

func NewAppDetailScreen(view AppDetailView) AppDetailScreen {
	return AppDetailScreen{view: view}
}

func (s AppDetailScreen) Metadata() AppSummary {
	return s.view.App
}

func (s AppDetailScreen) Services() []ServiceView {
	return s.view.Services
}

func (s AppDetailScreen) Domains() []DomainView {
	return s.view.Domains
}

func (s AppDetailScreen) Releases() []ReleaseView {
	return s.view.Releases
}

func (s AppDetailScreen) LatestDeployments(limit int) []DeploymentView {
	if limit <= 0 || limit >= len(s.view.Deployments) {
		return s.view.Deployments
	}

	return s.view.Deployments[:limit]
}

func (s AppDetailScreen) Events() []EventView {
	return s.view.Events
}

func (s AppDetailScreen) Actions() []string {
	return s.view.Actions
}
