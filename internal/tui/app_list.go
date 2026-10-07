package tui

type AppListScreen struct {
	view AppListView
}

type AppListRow struct {
	Name                   string
	Status                 string
	NodeID                 string
	LatestDeploymentStatus string
	DomainCount            int
}

func NewAppListScreen(view AppListView) AppListScreen {
	return AppListScreen{view: view}
}

func (s AppListScreen) Rows() []AppListRow {
	rows := make([]AppListRow, 0, len(s.view.Items))
	for _, item := range s.view.Items {
		rows = append(rows, AppListRow{
			Name:                   item.Name,
			Status:                 item.Status,
			NodeID:                 item.NodeID,
			LatestDeploymentStatus: item.LatestDeploymentStatus,
			DomainCount:            item.DomainCount,
		})
	}

	return rows
}

func (s AppListScreen) Empty() bool {
	return len(s.view.Items) == 0
}

func (s AppListScreen) EmptyMessage() string {
	return "No apps"
}
