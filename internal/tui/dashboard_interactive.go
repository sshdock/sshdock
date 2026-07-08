package tui

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type DashboardRefreshFunc func(context.Context) (DashboardSnapshot, error)

type DashboardActions interface {
	RestartApp(appName string) error
	RestartService(appName string, serviceName string) error
	RedeployApp(appName string) error
	RollbackApp(appName string, releaseID string) error
	AttachDomain(appName string, serviceName string, domainName string, port int) error
	DetachDomain(appName string, domainName string) error
	RemoveApp(appName string) error
}

type dashboardFocus int

const (
	dashboardFocusApps dashboardFocus = iota
	dashboardFocusDetail
)

const dashboardDualPaneMinWidth = 82
const dashboardFollowInterval = 2 * time.Second

func (f dashboardFocus) String() string {
	switch f {
	case dashboardFocusDetail:
		return "detail"
	default:
		return "apps"
	}
}

type dashboardLayout struct {
	width                    int
	compact                  bool
	panelInnerHeight         int
	leftWidth                int
	rightWidth               int
	compactListInnerHeight   int
	compactDetailInnerHeight int
}

type dashboardMode int

const (
	dashboardModeNormal dashboardMode = iota
	dashboardModeActionMenu
	dashboardModeActionChoice
	dashboardModeActionInput
	dashboardModeActionConfirm
)

type dashboardActionKind int

const (
	dashboardActionRestartApp dashboardActionKind = iota
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
	{label: "restart app", kind: dashboardActionRestartApp},
	{label: "restart service", kind: dashboardActionRestartService},
	{label: "redeploy latest", kind: dashboardActionRedeploy},
	{label: "rollback release", kind: dashboardActionRollback},
	{label: "attach domain", kind: dashboardActionAttachDomain},
	{label: "detach domain", kind: dashboardActionDetachDomain},
	{label: "remove app", kind: dashboardActionRemoveApp},
}

type InteractiveDashboardModel struct {
	snapshot DashboardSnapshot
	refresh  DashboardRefreshFunc
	actions  DashboardActions

	selected  int
	tab       int
	width     int
	height    int
	showHelp  bool
	err       error
	message   string
	logs      viewport.Model
	filter    textinput.Model
	filtering bool
	focus     dashboardFocus

	mode          dashboardMode
	actionIndex   int
	choiceIndex   int
	pendingAction dashboardActionKind
	actionInput   textinput.Model
	followLogs    bool
}

type dashboardRefreshMsg struct {
	snapshot DashboardSnapshot
	err      error
	status   string
	follow   bool
}

type dashboardActionMsg struct {
	snapshot DashboardSnapshot
	err      error
	status   string
}

type dashboardFollowTickMsg struct{}

var dashboardTabs = []string{"Summary", "Services", "Routes", "Releases", "Deploys", "Events", "Logs"}

var (
	dashboardTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("62")).
				Padding(0, 1)
	dashboardStatusStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("236")).
				Padding(0, 1)
	dashboardPanelStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("238"))
	dashboardFocusedPanelStyle = lipgloss.NewStyle().
					Border(lipgloss.NormalBorder()).
					BorderForeground(lipgloss.Color("62"))
)

func NewInteractiveDashboardModel(snapshot DashboardSnapshot, refresh DashboardRefreshFunc) InteractiveDashboardModel {
	filter := textinput.New()
	filter.Placeholder = "filter apps"
	filter.Prompt = ""
	filter.CharLimit = 80
	filter.Width = 24

	actionInput := textinput.New()
	actionInput.Prompt = "> "
	actionInput.CharLimit = 160
	actionInput.Width = 48

	model := InteractiveDashboardModel{
		snapshot:    snapshot,
		refresh:     refresh,
		width:       100,
		height:      28,
		showHelp:    false,
		logs:        viewport.New(80, 8),
		filter:      filter,
		focus:       dashboardFocusApps,
		actionInput: actionInput,
	}
	model.syncLogViewport()
	return model
}

func NewInteractiveDashboardModelWithActions(snapshot DashboardSnapshot, refresh DashboardRefreshFunc, actions DashboardActions) InteractiveDashboardModel {
	model := NewInteractiveDashboardModel(snapshot, refresh)
	model.actions = actions
	return model
}

func RunInteractiveDashboard(ctx context.Context, snapshot DashboardSnapshot, refresh DashboardRefreshFunc, input io.Reader, output io.Writer) error {
	return RunInteractiveDashboardWithActions(ctx, snapshot, refresh, nil, input, output)
}

func RunInteractiveDashboardWithActions(ctx context.Context, snapshot DashboardSnapshot, refresh DashboardRefreshFunc, actions DashboardActions, input io.Reader, output io.Writer) error {
	program := tea.NewProgram(
		NewInteractiveDashboardModelWithActions(snapshot, refresh, actions),
		tea.WithContext(ctx),
		tea.WithInput(input),
		tea.WithOutput(output),
		tea.WithAltScreen(),
	)
	_, err := program.Run()
	return err
}

func (m InteractiveDashboardModel) Init() tea.Cmd {
	return nil
}

func (m InteractiveDashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeFilter()
		m.resizeViewport()
		m.syncLogViewport()
		return m, nil
	case tea.KeyMsg:
		if m.mode != dashboardModeNormal {
			return m.updateAction(msg)
		}
		if m.filtering {
			return m.updateFilter(msg)
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "down", "j":
			m.moveSelection(1)
		case "up", "k":
			m.moveSelection(-1)
		case "g", "home":
			m.jumpSelection(0)
		case "G", "end":
			m.jumpSelection(len(m.filteredAppOrder()) - 1)
		case "tab":
			m.tab = (m.tab + 1) % len(dashboardTabs)
			m.disableFollowOutsideLogs()
			m.logs.GotoTop()
		case "shift+tab":
			m.tab = (m.tab + len(dashboardTabs) - 1) % len(dashboardTabs)
			m.disableFollowOutsideLogs()
			m.logs.GotoTop()
		case "enter":
			m.focus = dashboardFocusDetail
		case "esc":
			m.focus = dashboardFocusApps
		case "pgdown", "d":
			m.logs.PageDown()
		case "pgup", "u":
			m.logs.PageUp()
		case "?":
			m.showHelp = !m.showHelp
		case "/":
			m.filtering = true
			m.focus = dashboardFocusApps
			return m, m.filter.Focus()
		case "a":
			m.openActionMenu()
		case "r":
			if m.refresh == nil {
				return m, nil
			}
			return m, func() tea.Msg {
				snapshot, err := m.refresh(context.Background())
				return dashboardRefreshMsg{snapshot: snapshot, err: err, status: "refreshed"}
			}
		case "f":
			if dashboardTabs[m.tab] != "Logs" {
				return m, nil
			}
			m.followLogs = !m.followLogs
			if m.followLogs {
				m.message = "logs follow on"
				return m, dashboardFollowTickCmd()
			}
			m.message = "logs follow off"
		}
		m.syncLogViewport()
		return m, nil
	case dashboardRefreshMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.applySnapshot(msg.snapshot)
		m.err = nil
		if msg.status != "" {
			m.message = msg.status
		}
		if m.followLogs && dashboardTabs[m.tab] == "Logs" {
			return m, dashboardFollowTickCmd()
		}
		return m, nil
	case dashboardActionMsg:
		m.mode = dashboardModeNormal
		m.actionInput.Blur()
		m.actionInput.SetValue("")
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.applySnapshot(msg.snapshot)
		m.err = nil
		m.message = msg.status
		return m, nil
	case dashboardFollowTickMsg:
		if !m.followLogs || dashboardTabs[m.tab] != "Logs" || m.refresh == nil {
			return m, nil
		}
		return m, func() tea.Msg {
			snapshot, err := m.refresh(context.Background())
			return dashboardRefreshMsg{snapshot: snapshot, err: err, status: "logs refreshed", follow: true}
		}
	default:
		var cmd tea.Cmd
		m.logs, cmd = m.logs.Update(msg)
		return m, cmd
	}
}

func (m InteractiveDashboardModel) View() string {
	parts := []string{m.titleBar(), m.bodyView(), m.statusBar()}
	return strings.TrimRight(strings.Join(parts, "\n"), "\n")
}

func (m InteractiveDashboardModel) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.selected = 0
			m.logs.GotoTop()
			m.syncLogViewport()
		}
		m.filtering = false
		m.filter.Blur()
		return m, nil
	case "enter":
		m.filtering = false
		m.filter.Blur()
		m.clampSelection()
		m.syncLogViewport()
		return m, nil
	}

	before := m.filter.Value()
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() != before {
		m.selected = 0
		m.logs.GotoTop()
		m.syncLogViewport()
	}
	return m, cmd
}

func (m InteractiveDashboardModel) updateAction(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.closeAction()
		return m, nil
	}

	switch m.mode {
	case dashboardModeActionMenu:
		return m.updateActionMenu(msg)
	case dashboardModeActionChoice:
		return m.updateActionChoice(msg)
	case dashboardModeActionInput, dashboardModeActionConfirm:
		return m.updateActionInput(msg)
	default:
		return m, nil
	}
}

func (m InteractiveDashboardModel) updateActionMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "down", "j":
		m.actionIndex = minInt(len(dashboardActionItems)-1, m.actionIndex+1)
	case "up", "k":
		m.actionIndex = maxInt(0, m.actionIndex-1)
	case "enter":
		return m.selectAction()
	}
	return m, nil
}

func (m InteractiveDashboardModel) updateActionChoice(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	choices := m.actionChoices()
	switch msg.String() {
	case "down", "j":
		m.choiceIndex = minInt(len(choices)-1, m.choiceIndex+1)
	case "up", "k":
		m.choiceIndex = maxInt(0, m.choiceIndex-1)
	case "enter":
		return m.executeSelectedChoice(choices)
	}
	return m, nil
}

func (m InteractiveDashboardModel) updateActionInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		if m.mode == dashboardModeActionConfirm {
			return m.executeRemoveConfirmation()
		}
		return m.executeAttachInput()
	}

	var cmd tea.Cmd
	m.actionInput, cmd = m.actionInput.Update(msg)
	return m, cmd
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
	case dashboardActionRestartApp, dashboardActionRedeploy:
		return m, m.executeAction(item.kind, "", "", 0)
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

type dashboardActionChoice struct {
	label string
	value string
}

func (m InteractiveDashboardModel) actionChoices() []dashboardActionChoice {
	selected, ok := m.selectedApp()
	if !ok {
		return nil
	}
	switch m.pendingAction {
	case dashboardActionRestartService:
		services := selected.Detail.Services()
		choices := make([]dashboardActionChoice, 0, len(services))
		for _, service := range services {
			choices = append(choices, dashboardActionChoice{
				label: service.Name + " " + valueOrDash(service.State),
				value: service.Name,
			})
		}
		return choices
	case dashboardActionRollback:
		releases := selected.Detail.Releases()
		choices := make([]dashboardActionChoice, 0, len(releases))
		for _, release := range releases {
			choices = append(choices, dashboardActionChoice{
				label: release.ID + " " + valueOrDash(release.Status),
				value: release.ID,
			})
		}
		return choices
	case dashboardActionDetachDomain:
		domains := selected.Detail.Domains()
		choices := make([]dashboardActionChoice, 0, len(domains))
		for _, domain := range domains {
			choices = append(choices, dashboardActionChoice{
				label: domain.DomainName + " -> " + valueOrDash(domain.Target),
				value: domain.DomainName,
			})
		}
		return choices
	default:
		return nil
	}
}

func (m InteractiveDashboardModel) executeSelectedChoice(choices []dashboardActionChoice) (tea.Model, tea.Cmd) {
	if len(choices) == 0 {
		m.err = fmt.Errorf("no choices available")
		return m, nil
	}
	if m.choiceIndex < 0 {
		m.choiceIndex = 0
	}
	if m.choiceIndex >= len(choices) {
		m.choiceIndex = len(choices) - 1
	}
	choice := choices[m.choiceIndex]
	switch m.pendingAction {
	case dashboardActionRestartService:
		return m, m.executeAction(m.pendingAction, choice.value, "", 0)
	case dashboardActionRollback:
		return m, m.executeAction(m.pendingAction, choice.value, "", 0)
	case dashboardActionDetachDomain:
		return m, m.executeAction(m.pendingAction, "", choice.value, 0)
	default:
		return m, nil
	}
}

func (m InteractiveDashboardModel) executeAttachInput() (tea.Model, tea.Cmd) {
	parts := strings.Fields(m.actionInput.Value())
	if len(parts) != 3 {
		m.err = fmt.Errorf("attach domain expects service domain port")
		return m, nil
	}
	port, err := strconv.Atoi(parts[2])
	if err != nil || port <= 0 {
		m.err = fmt.Errorf("port must be a positive integer")
		return m, nil
	}
	return m, m.executeAction(dashboardActionAttachDomain, parts[0], parts[1], port)
}

func (m InteractiveDashboardModel) executeRemoveConfirmation() (tea.Model, tea.Cmd) {
	appName, ok := m.selectedAppID()
	if !ok {
		m.err = fmt.Errorf("no app selected")
		return m, nil
	}
	if strings.TrimSpace(m.actionInput.Value()) != appName {
		m.err = fmt.Errorf("confirmation did not match")
		m.actionInput.SetValue("")
		return m, nil
	}
	return m, m.executeAction(dashboardActionRemoveApp, "", "", 0)
}

func (m InteractiveDashboardModel) executeAction(kind dashboardActionKind, primary string, secondary string, port int) tea.Cmd {
	appName, ok := m.selectedAppID()
	if !ok {
		return func() tea.Msg {
			return dashboardActionMsg{err: fmt.Errorf("no app selected")}
		}
	}
	actions := m.actions
	refresh := m.refresh
	current := m.snapshot
	status := dashboardActionStatus(kind, primary, secondary)

	return func() tea.Msg {
		if actions == nil {
			return dashboardActionMsg{err: fmt.Errorf("dashboard actions are not configured")}
		}
		var err error
		switch kind {
		case dashboardActionRestartApp:
			err = actions.RestartApp(appName)
		case dashboardActionRestartService:
			err = actions.RestartService(appName, primary)
		case dashboardActionRedeploy:
			err = actions.RedeployApp(appName)
		case dashboardActionRollback:
			err = actions.RollbackApp(appName, primary)
		case dashboardActionAttachDomain:
			err = actions.AttachDomain(appName, primary, secondary, port)
		case dashboardActionDetachDomain:
			err = actions.DetachDomain(appName, secondary)
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

func dashboardActionStatus(kind dashboardActionKind, primary string, secondary string) string {
	switch kind {
	case dashboardActionRestartApp:
		return "restart app complete"
	case dashboardActionRestartService:
		return "restart service " + valueOrDash(primary) + " complete"
	case dashboardActionRedeploy:
		return "redeploy latest complete"
	case dashboardActionRollback:
		return "rollback " + valueOrDash(primary) + " complete"
	case dashboardActionAttachDomain:
		return "attach " + valueOrDash(secondary) + " complete"
	case dashboardActionDetachDomain:
		return "detach " + valueOrDash(secondary) + " complete"
	case dashboardActionRemoveApp:
		return "remove app complete"
	default:
		return "action complete"
	}
}

func dashboardFollowTickCmd() tea.Cmd {
	return tea.Tick(dashboardFollowInterval, func(time.Time) tea.Msg {
		return dashboardFollowTickMsg{}
	})
}

func (m InteractiveDashboardModel) LogOffset() int {
	return m.logs.YOffset
}

func (m *InteractiveDashboardModel) moveSelection(delta int) {
	order := m.filteredAppOrder()
	if len(order) == 0 {
		m.selected = 0
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(order) {
		m.selected = len(order) - 1
	}
	m.logs.GotoTop()
}

func (m *InteractiveDashboardModel) jumpSelection(index int) {
	order := m.filteredAppOrder()
	if len(order) == 0 {
		m.selected = 0
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(order) {
		index = len(order) - 1
	}
	m.selected = index
	m.logs.GotoTop()
}

func (m *InteractiveDashboardModel) clampSelection() {
	order := m.filteredAppOrder()
	if len(order) == 0 {
		m.selected = 0
		return
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(order) {
		m.selected = len(order) - 1
	}
}

func (m InteractiveDashboardModel) titleBar() string {
	width := m.terminalWidth()
	selected := "No apps"
	if detail, ok := m.selectedApp(); ok {
		metadata := detail.Detail.Metadata()
		selected = fmt.Sprintf("%s %s on %s", metadata.Name, metadata.Status, metadata.NodeID)
	}
	line := fmt.Sprintf("SSHDock Dashboard | %s | %s", selected, dashboardTabs[m.tab])
	if m.followLogs {
		line = fmt.Sprintf("%s | follow on", line)
	}
	if m.message != "" {
		line = fmt.Sprintf("%s | %s", line, m.message)
	}
	if m.err != nil {
		line = fmt.Sprintf("%s | %s", line, m.err.Error())
	}
	return dashboardTitleStyle.Render(fitLine(line, maxInt(1, width-2)))
}

func (m InteractiveDashboardModel) bodyView() string {
	layout := m.dashboardLayout()
	if layout.compact {
		return m.compactView()
	}

	apps := m.renderPanel("Apps", m.appListView(panelContentWidth(layout.leftWidth), false), layout.leftWidth, layout.panelInnerHeight, m.focus == dashboardFocusApps)
	detail := m.renderPanel(m.detailTitle(), m.detailContent(panelContentWidth(layout.rightWidth)), layout.rightWidth, layout.panelInnerHeight, m.focus == dashboardFocusDetail)
	return lipgloss.JoinHorizontal(lipgloss.Top, apps, detail)
}

func (m InteractiveDashboardModel) statusBar() string {
	width := m.terminalWidth()
	if m.filtering {
		line := strings.Join([]string{m.helpView(), fmt.Sprintf("apps %d", len(m.filteredAppOrder()))}, " | ")
		return dashboardStatusStyle.Render(fitLine(line, maxInt(1, width-2)))
	}
	if m.mode != dashboardModeNormal {
		parts := []string{m.helpView(), fmt.Sprintf("apps %d", len(m.filteredAppOrder()))}
		if m.err != nil {
			parts = append(parts, "error: "+m.err.Error())
		}
		line := strings.Join(parts, " | ")
		return dashboardStatusStyle.Render(fitLine(line, maxInt(1, width-2)))
	}
	if m.focus == dashboardFocusDetail || dashboardTabs[m.tab] == "Logs" {
		parts := []string{m.helpView(), fmt.Sprintf("apps %d", len(m.filteredAppOrder()))}
		if m.followLogs {
			parts = append(parts, "follow on")
		}
		line := strings.Join(parts, " | ")
		return dashboardStatusStyle.Render(fitLine(line, maxInt(1, width-2)))
	}
	parts := []string{
		fmt.Sprintf("apps %d", len(m.filteredAppOrder())),
		fmt.Sprintf("focus %s", m.focus.String()),
	}
	if m.filter.Value() != "" {
		parts = append(parts, "filter: "+valueOrDash(m.filter.Value()))
	}
	if m.err != nil {
		parts = append(parts, "error: "+m.err.Error())
	}
	if m.message != "" {
		parts = append(parts, m.message)
	}
	parts = append(parts, m.helpView())

	line := strings.Join(parts, " | ")
	return dashboardStatusStyle.Render(fitLine(line, maxInt(1, width-2)))
}

func (m InteractiveDashboardModel) appListView(width int, compact bool) string {
	var builder strings.Builder
	if m.filtering || m.filter.Value() != "" {
		fmt.Fprintf(&builder, "filter: %s\n", m.filter.View())
	}

	order := m.filteredAppOrder()
	if len(order) == 0 {
		if m.filter.Value() != "" {
			builder.WriteString("No matching apps\n")
		} else {
			builder.WriteString("No apps\n")
		}
		return builder.String()
	}
	items := m.appItemsByID()
	rows := make([][]string, 0, len(order))
	for i, id := range order {
		item, ok := items[id]
		if !ok {
			continue
		}
		cursor := " "
		if i == m.selected {
			cursor = ">"
		}
		rows = append(rows, []string{
			strings.TrimSpace(cursor + " " + item.Name),
			item.Status,
			valueOrDash(item.LatestReleaseStatus),
			fmt.Sprintf("%d", item.DomainCount),
		})
	}
	columns := []dashboardTableColumn{
		{Header: "App", MinWidth: 10, Flex: true, Priority: 0},
		{Header: "State", MinWidth: 7, Priority: 1},
		{Header: "Release", MinWidth: 9, Priority: 2},
		{Header: "Doms", MinWidth: 4, Priority: 3},
	}
	if compact {
		columns = columns[:2]
	}
	builder.WriteString(renderDashboardTable(width, columns, rows))
	return builder.String()
}

func (m InteractiveDashboardModel) detailTitle() string {
	selected, ok := m.selectedApp()
	if !ok {
		return "Detail"
	}
	if m.mode != dashboardModeNormal {
		return "Actions " + selected.Detail.Metadata().Name
	}
	return "App " + selected.Detail.Metadata().Name
}

func (m InteractiveDashboardModel) detailContent(width int) string {
	selected, ok := m.selectedApp()
	if !ok {
		return "No apps\n"
	}

	var builder strings.Builder
	metadata := selected.Detail.Metadata()
	fmt.Fprintf(&builder, "Status: %s | Node: %s\n", metadata.Status, metadata.NodeID)
	if m.mode != dashboardModeNormal {
		builder.WriteString(m.actionContent(width))
		return builder.String()
	}
	builder.WriteString(m.tabsView())
	builder.WriteString("\n")

	switch dashboardTabs[m.tab] {
	case "Summary":
		builder.WriteString(renderSummaryTable(width, selected.Detail))
	case "Services":
		builder.WriteString(renderServiceTable(width, selected.Detail.Services()))
	case "Routes":
		builder.WriteString(renderRouteTable(width, selected.Detail.Domains()))
	case "Releases":
		builder.WriteString(renderReleaseTable(width, selected.Detail.Releases()))
	case "Deploys":
		builder.WriteString(renderDeployTable(width, selected.Detail.LatestDeployments(5)))
	case "Events":
		builder.WriteString(renderEventTable(width, selected.Detail.Events()))
	case "Logs":
		builder.WriteString(m.logsBlock())
	}
	return builder.String()
}

func (m InteractiveDashboardModel) detailView() string {
	selected, ok := m.selectedApp()
	if !ok {
		return "No apps\n"
	}

	var builder strings.Builder
	builder.WriteString("App ")
	builder.WriteString(selected.Detail.Metadata().Name)
	builder.WriteString("\n")
	builder.WriteString(m.detailContent(panelContentWidth(m.dashboardLayout().rightWidth)))
	return builder.String()
}

func (m InteractiveDashboardModel) compactView() string {
	layout := m.dashboardLayout()
	apps := m.renderPanel("Apps", m.appListView(panelContentWidth(layout.width), true), layout.width, layout.compactListInnerHeight, m.focus == dashboardFocusApps)
	detail := m.renderPanel(m.detailTitle(), m.detailContent(panelContentWidth(layout.width)), layout.width, layout.compactDetailInnerHeight, m.focus == dashboardFocusDetail)
	return strings.TrimRight(strings.Join([]string{apps, detail}, "\n"), "\n")
}

func (m InteractiveDashboardModel) dashboardLayout() dashboardLayout {
	width := m.terminalWidth()
	bodyHeight := maxInt(6, m.terminalHeight()-2)
	if width < dashboardDualPaneMinWidth {
		listTotalHeight := maxInt(4, bodyHeight/3)
		if bodyHeight >= 16 {
			listTotalHeight = minInt(8, maxInt(5, bodyHeight/3))
		}
		detailTotalHeight := bodyHeight - listTotalHeight
		if detailTotalHeight < 4 {
			detailTotalHeight = maxInt(3, bodyHeight/2)
			listTotalHeight = maxInt(3, bodyHeight-detailTotalHeight)
		}
		return dashboardLayout{
			width:                    width,
			compact:                  true,
			compactListInnerHeight:   maxInt(1, listTotalHeight-2),
			compactDetailInnerHeight: maxInt(1, detailTotalHeight-2),
		}
	}

	leftWeight, rightWeight := 1, 3
	if m.focus == dashboardFocusApps {
		leftWeight, rightWeight = 2, 3
	}
	totalWeight := leftWeight + rightWeight
	leftWidth := (width * leftWeight) / totalWeight
	if leftWidth < 28 {
		leftWidth = 28
	}
	if width-leftWidth < 38 {
		leftWidth = width - 38
	}
	if leftWidth < 24 {
		leftWidth = 24
	}

	return dashboardLayout{
		width:            width,
		panelInnerHeight: maxInt(3, bodyHeight-2),
		leftWidth:        leftWidth,
		rightWidth:       width - leftWidth,
	}
}

func (m InteractiveDashboardModel) renderPanel(title string, content string, width int, innerHeight int, focused bool) string {
	style := dashboardPanelStyle
	if focused {
		style = dashboardFocusedPanelStyle
	}
	frameWidth, _ := style.GetFrameSize()
	innerWidth := maxInt(1, width-frameWidth)

	lines := make([]string, 0, innerHeight)
	lines = append(lines, fitLine(title, innerWidth))
	contentLines := splitPanelLines(content)
	for _, line := range contentLines {
		if len(lines) >= innerHeight {
			break
		}
		lines = append(lines, fitLine(line, innerWidth))
	}
	if len(lines) > innerHeight {
		lines = lines[:innerHeight]
	}
	if len(lines) == innerHeight && innerHeight > 1 && len(contentLines)+1 > innerHeight {
		lines[innerHeight-1] = fitLine("...", innerWidth)
	}
	for len(lines) < innerHeight {
		lines = append(lines, "")
	}

	return style.Width(innerWidth).Render(strings.Join(lines, "\n"))
}

func splitPanelLines(content string) []string {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func (m InteractiveDashboardModel) terminalWidth() int {
	if m.width < 20 {
		return 100
	}
	return maxInt(40, m.width)
}

func (m InteractiveDashboardModel) terminalHeight() int {
	if m.height < 8 {
		return 28
	}
	return maxInt(12, m.height)
}

func (m InteractiveDashboardModel) tabsView() string {
	parts := make([]string, 0, len(dashboardTabs))
	for i, tab := range dashboardTabs {
		if i == m.tab {
			parts = append(parts, "["+tab+"]")
		} else {
			parts = append(parts, tab)
		}
	}
	return strings.Join(parts, " ")
}

func (m InteractiveDashboardModel) helpView() string {
	if m.showHelp {
		return "[?] hide  [/] filter  [j/k] select  [g/G] top/bottom  [enter] detail  [tab] tabs  [a] actions  [u/d] logs  [r] refresh  [q] quit"
	}
	if m.filtering {
		return "[type] filter  [esc] clear  [enter] apply"
	}
	if m.mode == dashboardModeActionInput {
		return "[service domain port] attach  [enter] run  [esc] cancel"
	}
	if m.mode == dashboardModeActionConfirm {
		return "[type app name exactly] confirm remove; Docker volumes stay  [enter] remove  [esc] cancel"
	}
	if m.mode != dashboardModeNormal {
		return "[j/k] choose  [enter] run  [esc] cancel"
	}
	if dashboardTabs[m.tab] == "Logs" {
		return "[u/d] scroll  [f] follow  [pgup/pgdn] page  [tab] tabs  [esc] apps  [?] help"
	}
	if m.focus == dashboardFocusDetail {
		return "[tab] next  [shift+tab] prev  [a] actions  [esc] apps  [?] help"
	}
	return "[?] help  [/] filter  [j/k] select  [g/G] top/bottom  [enter] detail  [a] actions  [r] refresh  [q] quit"
}

func (m InteractiveDashboardModel) selectedApp() (DashboardAppSnapshot, bool) {
	order := m.filteredAppOrder()
	if len(order) == 0 || m.selected < 0 || m.selected >= len(order) {
		return DashboardAppSnapshot{}, false
	}
	snapshot, ok := m.snapshot.AppsByID[order[m.selected]]
	return snapshot, ok
}

func (m InteractiveDashboardModel) selectedAppID() (string, bool) {
	order := m.filteredAppOrder()
	if len(order) == 0 || m.selected < 0 || m.selected >= len(order) {
		return "", false
	}
	return order[m.selected], true
}

func (m InteractiveDashboardModel) actionContent(width int) string {
	switch m.mode {
	case dashboardModeActionMenu:
		return m.actionMenuContent(width)
	case dashboardModeActionChoice:
		return m.actionChoiceContent(width)
	case dashboardModeActionInput:
		return strings.Join([]string{
			"Attach domain",
			"Enter: service domain port",
			m.actionInput.View(),
		}, "\n") + "\n"
	case dashboardModeActionConfirm:
		appName, _ := m.selectedAppID()
		return strings.Join([]string{
			"Remove app",
			"Type app name exactly: " + appName,
			m.actionInput.View(),
		}, "\n") + "\n"
	default:
		return ""
	}
}

func (m InteractiveDashboardModel) actionMenuContent(width int) string {
	rows := make([][]string, 0, len(dashboardActionItems))
	for i, item := range dashboardActionItems {
		cursor := " "
		if i == m.actionIndex {
			cursor = ">"
		}
		rows = append(rows, []string{cursor, item.label})
	}
	return "Actions\n" + renderDashboardTable(width, []dashboardTableColumn{
		{Header: "", MinWidth: 1, Priority: 0},
		{Header: "Action", MinWidth: 18, Flex: true, Priority: 0},
	}, rows) + "\n"
}

func (m InteractiveDashboardModel) actionChoiceContent(width int) string {
	choices := m.actionChoices()
	if len(choices) == 0 {
		return "No choices available\n"
	}
	rows := make([][]string, 0, len(choices))
	for i, choice := range choices {
		cursor := " "
		if i == m.choiceIndex {
			cursor = ">"
		}
		rows = append(rows, []string{cursor, choice.label})
	}
	return actionChoiceTitle(m.pendingAction) + "\n" + renderDashboardTable(width, []dashboardTableColumn{
		{Header: "", MinWidth: 1, Priority: 0},
		{Header: "Choice", MinWidth: 18, Flex: true, Priority: 0},
	}, rows) + "\n"
}

func actionChoiceTitle(kind dashboardActionKind) string {
	switch kind {
	case dashboardActionRestartService:
		return "Choose service"
	case dashboardActionRollback:
		return "Choose release"
	case dashboardActionDetachDomain:
		return "Choose domain"
	default:
		return "Choose"
	}
}

func (m *InteractiveDashboardModel) applySnapshot(snapshot DashboardSnapshot) {
	selectedID, _ := m.selectedAppID()
	m.snapshot = snapshot
	m.selected = m.indexForAppID(selectedID)
	m.clampSelection()
	m.syncLogViewport()
}

func (m *InteractiveDashboardModel) disableFollowOutsideLogs() {
	if dashboardTabs[m.tab] != "Logs" {
		m.followLogs = false
	}
}

func (m InteractiveDashboardModel) appOrder() []string {
	if len(m.snapshot.AppOrder) > 0 {
		return append([]string(nil), m.snapshot.AppOrder...)
	}
	items := m.snapshot.Apps.view.Items
	order := make([]string, 0, len(items))
	for _, item := range items {
		id := item.ID
		if id == "" {
			id = item.Name
		}
		order = append(order, id)
	}
	return order
}

func (m InteractiveDashboardModel) filteredAppOrder() []string {
	order := m.appOrder()
	filter := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if filter == "" {
		return order
	}

	items := m.appItemsByID()
	filtered := make([]string, 0, len(order))
	for _, id := range order {
		item, ok := items[id]
		if !ok {
			continue
		}
		if appMatchesFilter(item, filter) {
			filtered = append(filtered, id)
		}
	}
	return filtered
}

func (m InteractiveDashboardModel) appItemsByID() map[string]AppListItem {
	items := make(map[string]AppListItem, len(m.snapshot.Apps.view.Items))
	for _, item := range m.snapshot.Apps.view.Items {
		id := item.ID
		if id == "" {
			id = item.Name
		}
		items[id] = item
	}
	return items
}

func appMatchesFilter(item AppListItem, filter string) bool {
	haystack := strings.ToLower(strings.Join([]string{
		item.Name,
		item.Status,
		item.NodeID,
		item.LatestReleaseStatus,
		fmt.Sprintf("domains=%d", item.DomainCount),
	}, " "))
	for _, term := range strings.Fields(filter) {
		if !strings.Contains(haystack, term) {
			return false
		}
	}
	return true
}

func (m InteractiveDashboardModel) indexForAppID(appID string) int {
	if appID == "" {
		return 0
	}
	for i, id := range m.filteredAppOrder() {
		if id == appID {
			return i
		}
	}
	return 0
}

func (m *InteractiveDashboardModel) resizeViewport() {
	layout := m.dashboardLayout()
	width := layout.rightWidth
	height := layout.panelInnerHeight
	if layout.compact {
		width = layout.width
		height = layout.compactDetailInnerHeight
	}
	frameWidth, _ := dashboardPanelStyle.GetFrameSize()
	m.logs.Width = maxInt(12, width-frameWidth)
	m.logs.Height = maxInt(1, height-7)
}

func (m *InteractiveDashboardModel) resizeFilter() {
	m.filter.Width = maxInt(8, minInt(32, maxInt(8, m.terminalWidth()-18)))
}

func (m *InteractiveDashboardModel) syncLogViewport() {
	m.resizeViewport()
	m.logs.SetContent(logsTableText(m.selectedLogs(), m.logs.Width))
}

func (m InteractiveDashboardModel) logsBlock() string {
	return m.logs.View()
}

func (m InteractiveDashboardModel) selectedLogs() map[string]LogsView {
	selected, ok := m.selectedApp()
	if !ok {
		return nil
	}
	return selected.Logs
}

func logsText(logsByService map[string]LogsView) string {
	return logsTableText(logsByService, 80)
}

func logsTableText(logsByService map[string]LogsView, width int) string {
	if len(logsByService) == 0 {
		return "- none"
	}
	names := make([]string, 0, len(logsByService))
	for name := range logsByService {
		names = append(names, name)
	}
	sort.Strings(names)

	var builder strings.Builder
	rows := make([][]string, 0)
	for _, name := range names {
		screen := NewLogsScreen(logsByService[name])
		if len(screen.Lines()) == 0 {
			rows = append(rows, []string{name, "- none"})
			continue
		}
		for _, line := range screen.Lines() {
			rows = append(rows, []string{name, line})
		}
	}
	builder.WriteString(renderDashboardTable(width, []dashboardTableColumn{
		{Header: "Service", MinWidth: 8, Priority: 0},
		{Header: "Line", MinWidth: 12, Flex: true, Priority: 0},
	}, rows))
	return strings.TrimRight(builder.String(), "\n")
}

type dashboardTableColumn struct {
	Header   string
	MinWidth int
	Flex     bool
	Priority int
}

func renderSummaryTable(width int, detail AppDetailScreen) string {
	metadata := detail.Metadata()
	health := detail.Health()
	rows := [][]string{
		{"App", metadata.Name},
		{"State", metadata.Status},
		{"Node", metadata.NodeID},
		{"Route", valueOrDash(health.RouteStatus)},
		{"Latest deploy", valueOrDash(health.LatestDeploymentStatus)},
		{"Service status", valueOrDash(health.ServiceStatus)},
		{"Services", fmt.Sprintf("%d", len(detail.Services()))},
		{"Routes", fmt.Sprintf("%d", len(detail.Domains()))},
		{"Releases", fmt.Sprintf("%d", len(detail.Releases()))},
		{"Deploys", fmt.Sprintf("%d", len(detail.LatestDeployments(5)))},
		{"Events", fmt.Sprintf("%d", len(detail.Events()))},
	}
	if health.LastFailure != "" {
		rows = append(rows, []string{"Last failure", health.LastFailure})
	}
	return renderDashboardTable(width, []dashboardTableColumn{
		{Header: "Field", MinWidth: 14, Priority: 0},
		{Header: "Value", MinWidth: 12, Flex: true, Priority: 0},
	}, rows)
}

func renderServiceTable(width int, services []ServiceView) string {
	if len(services) == 0 {
		return "- none"
	}
	rows := make([][]string, 0, len(services))
	for _, service := range services {
		rows = append(rows, []string{service.Name, service.State})
	}
	return renderDashboardTable(width, []dashboardTableColumn{
		{Header: "Service", MinWidth: 12, Flex: true, Priority: 0},
		{Header: "State", MinWidth: 8, Priority: 0},
	}, rows)
}

func renderRouteTable(width int, domains []DomainView) string {
	if len(domains) == 0 {
		return "- none"
	}
	rows := make([][]string, 0, len(domains))
	for _, domain := range domains {
		rows = append(rows, []string{
			domain.DomainName,
			valueOrDash(domain.ServiceName),
			domain.Target,
			fmt.Sprintf("%t", domain.HTTPS),
		})
	}
	return renderDashboardTable(width, []dashboardTableColumn{
		{Header: "Domain", MinWidth: 14, Flex: true, Priority: 0},
		{Header: "Service", MinWidth: 8, Priority: 2},
		{Header: "Target", MinWidth: 10, Priority: 1},
		{Header: "HTTPS", MinWidth: 5, Priority: 3},
	}, rows)
}

func renderReleaseTable(width int, releases []ReleaseView) string {
	if len(releases) == 0 {
		return "- none"
	}
	rows := make([][]string, 0, len(releases))
	for _, release := range releases {
		rows = append(rows, []string{
			release.ID,
			release.Status,
			shortValue(release.CommitSHA, 12),
			formatDashboardTime(release.CreatedAt),
		})
	}
	return renderDashboardTable(width, []dashboardTableColumn{
		{Header: "Release", MinWidth: 12, Flex: true, Priority: 0},
		{Header: "Status", MinWidth: 9, Priority: 0},
		{Header: "Commit", MinWidth: 8, Priority: 1},
		{Header: "Created", MinWidth: 12, Priority: 2},
	}, rows)
}

func renderDeployTable(width int, deployments []DeploymentView) string {
	if len(deployments) == 0 {
		return "- none"
	}
	rows := make([][]string, 0, len(deployments))
	for _, deployment := range deployments {
		rows = append(rows, []string{
			deployment.ID,
			deployment.Status,
			deployment.ReleaseID,
			formatDashboardTime(deployment.StartedAt),
		})
	}
	return renderDashboardTable(width, []dashboardTableColumn{
		{Header: "Deploy", MinWidth: 12, Flex: true, Priority: 0},
		{Header: "Status", MinWidth: 9, Priority: 0},
		{Header: "Release", MinWidth: 12, Priority: 1},
		{Header: "Started", MinWidth: 12, Priority: 2},
	}, rows)
}

func renderEventTable(width int, events []EventView) string {
	if len(events) == 0 {
		return "- none"
	}
	rows := make([][]string, 0, len(events))
	for _, event := range events {
		rows = append(rows, []string{
			event.Type,
			event.Message,
			formatDashboardTime(event.CreatedAt),
		})
	}
	return renderDashboardTable(width, []dashboardTableColumn{
		{Header: "Type", MinWidth: 16, Priority: 0},
		{Header: "Message", MinWidth: 16, Flex: true, Priority: 0},
		{Header: "Created", MinWidth: 12, Priority: 2},
	}, rows)
}

func renderDashboardTable(width int, columns []dashboardTableColumn, rows [][]string) string {
	width = maxInt(1, width)
	visible := visibleDashboardColumns(width, columns)
	if len(visible) == 0 {
		return ""
	}
	widths := dashboardColumnWidths(width, visible)

	var builder strings.Builder
	builder.WriteString(renderDashboardTableRow(visible, widths, func(index int) string {
		return visible[index].Header
	}))
	builder.WriteString("\n")
	builder.WriteString(renderDashboardTableDivider(widths))
	for _, row := range rows {
		builder.WriteString("\n")
		builder.WriteString(renderDashboardTableRow(visible, widths, func(index int) string {
			sourceIndex := columnSourceIndex(columns, visible[index])
			if sourceIndex < 0 || sourceIndex >= len(row) {
				return ""
			}
			return row[sourceIndex]
		}))
	}
	return builder.String()
}

func visibleDashboardColumns(width int, columns []dashboardTableColumn) []dashboardTableColumn {
	visible := append([]dashboardTableColumn(nil), columns...)
	for len(visible) > 1 && dashboardMinTableWidth(visible) > width {
		removeIndex := -1
		removePriority := -1
		for i, column := range visible {
			if column.Priority > removePriority {
				removePriority = column.Priority
				removeIndex = i
			}
		}
		if removeIndex <= 0 && len(visible) > 1 {
			removeIndex = len(visible) - 1
		}
		visible = append(visible[:removeIndex], visible[removeIndex+1:]...)
	}
	return visible
}

func dashboardColumnWidths(width int, columns []dashboardTableColumn) []int {
	widths := make([]int, len(columns))
	spacing := dashboardTableSpacing(len(columns))
	flexIndex := -1
	fixedWidth := 0
	for i, column := range columns {
		widths[i] = maxInt(1, column.MinWidth)
		if column.Flex && flexIndex == -1 {
			flexIndex = i
			continue
		}
		fixedWidth += widths[i]
	}
	if flexIndex == -1 {
		flexIndex = 0
		fixedWidth -= widths[0]
	}
	widths[flexIndex] = maxInt(1, width-fixedWidth-spacing)
	return widths
}

func dashboardMinTableWidth(columns []dashboardTableColumn) int {
	total := dashboardTableSpacing(len(columns))
	for _, column := range columns {
		total += maxInt(1, column.MinWidth)
	}
	return total
}

func dashboardTableSpacing(columns int) int {
	if columns <= 1 {
		return 0
	}
	return (columns - 1) * 2
}

func renderDashboardTableRow(columns []dashboardTableColumn, widths []int, value func(int) string) string {
	parts := make([]string, 0, len(columns))
	for i := range columns {
		parts = append(parts, padRight(fitLine(value(i), widths[i]), widths[i]))
	}
	return strings.Join(parts, "  ")
}

func renderDashboardTableDivider(widths []int) string {
	parts := make([]string, 0, len(widths))
	for _, width := range widths {
		parts = append(parts, strings.Repeat("-", maxInt(1, width)))
	}
	return strings.Join(parts, "  ")
}

func columnSourceIndex(all []dashboardTableColumn, visible dashboardTableColumn) int {
	for i, column := range all {
		if column.Header == visible.Header {
			return i
		}
	}
	return -1
}

func padRight(s string, width int) string {
	padding := width - lipgloss.Width(s)
	if padding <= 0 {
		return s
	}
	return s + strings.Repeat(" ", padding)
}

func panelContentWidth(width int) int {
	frameWidth, _ := dashboardPanelStyle.GetFrameSize()
	return maxInt(1, width-frameWidth)
}

func shortValue(value string, maxWidth int) string {
	if value == "" {
		return "-"
	}
	return fitLine(value, maxWidth)
}

func formatDashboardTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Format("2006-01-02 15:04")
}

func renderServiceBlock(services []ServiceView) string {
	var builder strings.Builder
	builder.WriteString("Services\n")
	if len(services) == 0 {
		builder.WriteString("- none\n")
		return builder.String()
	}
	for _, service := range services {
		fmt.Fprintf(&builder, "- %s %s\n", service.Name, service.State)
	}
	return builder.String()
}

func renderDomainBlock(domains []DomainView) string {
	var builder strings.Builder
	builder.WriteString("Domains\n")
	if len(domains) == 0 {
		builder.WriteString("- none\n")
		return builder.String()
	}
	for _, domain := range domains {
		fmt.Fprintf(&builder, "- %s -> %s https=%t\n", domain.DomainName, domain.Target, domain.HTTPS)
	}
	return builder.String()
}

func renderReleaseBlock(releases []ReleaseView) string {
	var builder strings.Builder
	builder.WriteString("Releases\n")
	if len(releases) == 0 {
		builder.WriteString("- none\n")
		return builder.String()
	}
	for _, release := range releases {
		fmt.Fprintf(&builder, "- %s %s %s\n", release.ID, release.Status, release.CommitSHA)
	}
	return builder.String()
}

func renderDeploymentBlock(deployments []DeploymentView) string {
	var builder strings.Builder
	builder.WriteString("Deployments\n")
	if len(deployments) == 0 {
		builder.WriteString("- none\n")
		return builder.String()
	}
	for _, deployment := range deployments {
		fmt.Fprintf(&builder, "- %s %s %s\n", deployment.ID, deployment.Status, deployment.ReleaseID)
	}
	return builder.String()
}

func fitLine(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\t", "    ")
	s = strings.TrimRight(s, "\r")
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		return strings.Repeat(".", maxWidth)
	}

	suffix := "..."
	var builder strings.Builder
	for _, r := range s {
		next := builder.String() + string(r) + suffix
		if lipgloss.Width(next) > maxWidth {
			break
		}
		builder.WriteRune(r)
	}
	if builder.Len() == 0 {
		return suffix
	}
	return builder.String() + suffix
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
