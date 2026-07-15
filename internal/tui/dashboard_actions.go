package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type DashboardActions interface {
	StartApp(appName string) error
	StopApp(appName string) error
	RestartApp(appName string) error
	RestartService(appName string, serviceName string) error
	RedeployApp(appName string) error
	RollbackApp(appName string, releaseID string) error
	AttachDomain(appName string, serviceName string, domainName string, port int) error
	DetachDomain(appName string, domainName string) error
	RemoveApp(appName string) error
}

type dashboardActionKind int

const (
	dashboardActionStartApp dashboardActionKind = iota
	dashboardActionStopApp
	dashboardActionRestartApp
	dashboardActionRestartService
	dashboardActionRedeploy
	dashboardActionRollback
	dashboardActionAttachDomain
	dashboardActionDetachDomain
	dashboardActionRemoveApp
)

type dashboardActionItem struct {
	label string
	kind  dashboardActionKind
}

var dashboardActionItems = []dashboardActionItem{
	{label: "start app", kind: dashboardActionStartApp},
	{label: "stop app", kind: dashboardActionStopApp},
	{label: "restart app", kind: dashboardActionRestartApp},
	{label: "restart service", kind: dashboardActionRestartService},
	{label: "redeploy current main", kind: dashboardActionRedeploy},
	{label: "rollback release", kind: dashboardActionRollback},
	{label: "attach domain", kind: dashboardActionAttachDomain},
	{label: "detach domain", kind: dashboardActionDetachDomain},
	{label: "remove app", kind: dashboardActionRemoveApp},
}

type dashboardActionChoice struct {
	label string
	value string
}

type dashboardActionRequest struct {
	kind      dashboardActionKind
	primary   string
	secondary string
	port      int
}

func (m *InteractiveDashboardModel) openActionMenu() {
	if m.actions == nil {
		m.err = fmt.Errorf("dashboard actions are not configured")
		return
	}
	if _, ok := m.selectedAppID(); !ok {
		m.err = fmt.Errorf("no app selected")
		return
	}
	m.err = nil
	m.message = ""
	m.mode = dashboardModeActionMenu
	m.actionIndex = 0
	m.choiceIndex = 0
	m.focus = dashboardFocusDetail
	m.actionInput.Blur()
	m.actionInput.SetValue("")
}

func (m *InteractiveDashboardModel) closeAction() {
	m.mode = dashboardModeNormal
	m.err = nil
	m.actionInput.Blur()
	m.actionInput.SetValue("")
}

func (m InteractiveDashboardModel) selectAction() (tea.Model, tea.Cmd) {
	if m.actionIndex < 0 || m.actionIndex >= len(dashboardActionItems) {
		return m, nil
	}
	item := dashboardActionItems[m.actionIndex]
	m.pendingAction = item.kind
	m.choiceIndex = 0
	m.err = nil
	m.message = ""

	switch item.kind {
	case dashboardActionStartApp, dashboardActionStopApp, dashboardActionRestartApp, dashboardActionRedeploy:
		return m, m.executeAction(dashboardActionRequest{kind: item.kind})
	case dashboardActionRestartService, dashboardActionRollback, dashboardActionDetachDomain:
		if len(m.actionChoices()) == 0 {
			m.err = fmt.Errorf("no choices available for %s", item.label)
			return m, nil
		}
		m.mode = dashboardModeActionChoice
		return m, nil
	case dashboardActionAttachDomain:
		m.mode = dashboardModeActionInput
		m.actionInput.Placeholder = "web app.example.com 3000"
		m.actionInput.SetValue("")
		return m, m.actionInput.Focus()
	case dashboardActionRemoveApp:
		m.mode = dashboardModeActionConfirm
		m.actionInput.Placeholder = "type app name exactly"
		m.actionInput.SetValue("")
		return m, m.actionInput.Focus()
	default:
		return m, nil
	}
}

func (m InteractiveDashboardModel) actionVisibleRows() int {
	layout := m.dashboardLayout()
	innerHeight := layout.panelInnerHeight
	if layout.compact {
		innerHeight = layout.compactDetailInnerHeight
	}
	return maxInt(1, innerHeight-5)
}

func actionWindow(length int, selected int, limit int) (int, int) {
	if limit >= length {
		return 0, length
	}
	start := maxInt(0, selected-limit+1)
	return start, minInt(length, start+limit)
}
