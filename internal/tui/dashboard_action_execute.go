package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

func (m InteractiveDashboardModel) executeAction(request dashboardActionRequest) tea.Cmd {
	appName, ok := m.selectedAppID()
	if !ok {
		return func() tea.Msg { return dashboardActionMsg{err: fmt.Errorf("no app selected")} }
	}
	actions := m.actions
	refresh := m.refresh
	current := m.snapshot
	status := dashboardActionStatus(request)

	return func() tea.Msg {
		if actions == nil {
			return dashboardActionMsg{err: fmt.Errorf("dashboard actions are not configured")}
		}
		var err error
		switch request.kind {
		case dashboardActionStartApp:
			err = actions.StartApp(appName)
		case dashboardActionStopApp:
			err = actions.StopApp(appName)
		case dashboardActionRestartApp:
			err = actions.RestartApp(appName)
		case dashboardActionRestartService:
			err = actions.RestartService(appName, request.primary)
		case dashboardActionRedeploy:
			err = actions.RedeployApp(appName)
		case dashboardActionRollback:
			err = actions.RollbackApp(appName, request.primary)
		case dashboardActionAttachDomain:
			err = actions.AttachDomain(appName, request.primary, request.secondary, request.port)
		case dashboardActionDetachDomain:
			err = actions.DetachDomain(appName, request.secondary)
		case dashboardActionRemoveApp:
			err = actions.RemoveApp(appName)
		}
		if err != nil {
			return dashboardActionMsg{err: err}
		}
		if refresh == nil {
			return dashboardActionMsg{snapshot: current, status: status}
		}
		snapshot, err := refresh(context.Background())
		if err != nil {
			return dashboardActionMsg{err: err}
		}
		return dashboardActionMsg{snapshot: snapshot, status: status}
	}
}

func dashboardActionStatus(request dashboardActionRequest) string {
	switch request.kind {
	case dashboardActionStartApp:
		return "start app complete"
	case dashboardActionStopApp:
		return "stop app complete"
	case dashboardActionRestartApp:
		return "restart app complete"
	case dashboardActionRestartService:
		return "restart service " + valueOrDash(request.primary) + " complete"
	case dashboardActionRedeploy:
		return "redeploy current main complete"
	case dashboardActionRollback:
		return "rollback " + valueOrDash(request.primary) + " complete"
	case dashboardActionAttachDomain:
		return "attach " + valueOrDash(request.secondary) + " complete"
	case dashboardActionDetachDomain:
		return "detach " + valueOrDash(request.secondary) + " complete"
	case dashboardActionRemoveApp:
		return "remove app complete"
	default:
		return "action complete"
	}
}
