package tui

type LogsScreen struct {
	view LogsView
}

func NewLogsScreen(view LogsView) LogsScreen {
	return LogsScreen{view: view}
}

func (s LogsScreen) Title() string {
	return s.view.AppID + "/" + s.view.ServiceName + " logs"
}

func (s LogsScreen) Lines() []string {
	return s.view.Lines
}
