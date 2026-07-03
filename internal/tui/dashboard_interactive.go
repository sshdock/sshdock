package tui

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type DashboardRefreshFunc func(context.Context) (DashboardSnapshot, error)

type dashboardFocus int

const (
	dashboardFocusApps dashboardFocus = iota
	dashboardFocusDetail
)

const dashboardDualPaneMinWidth = 82

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

type InteractiveDashboardModel struct {
	snapshot DashboardSnapshot
	refresh  DashboardRefreshFunc

	selected  int
	tab       int
	width     int
	height    int
	showHelp  bool
	err       error
	logs      viewport.Model
	filter    textinput.Model
	filtering bool
	focus     dashboardFocus
}

type dashboardRefreshMsg struct {
	snapshot DashboardSnapshot
	err      error
}

var dashboardTabs = []string{"Summary", "Services", "Routes", "Releases", "Deploys", "Logs"}

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

	model := InteractiveDashboardModel{
		snapshot: snapshot,
		refresh:  refresh,
		width:    100,
		height:   28,
		showHelp: false,
		logs:     viewport.New(80, 8),
		filter:   filter,
		focus:    dashboardFocusApps,
	}
	model.syncLogViewport()
	return model
}

func RunInteractiveDashboard(ctx context.Context, snapshot DashboardSnapshot, refresh DashboardRefreshFunc, input io.Reader, output io.Writer) error {
	program := tea.NewProgram(
		NewInteractiveDashboardModel(snapshot, refresh),
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
			m.logs.GotoTop()
		case "shift+tab":
			m.tab = (m.tab + len(dashboardTabs) - 1) % len(dashboardTabs)
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
		case "r":
			if m.refresh == nil {
				return m, nil
			}
			return m, func() tea.Msg {
				snapshot, err := m.refresh(context.Background())
				return dashboardRefreshMsg{snapshot: snapshot, err: err}
			}
		}
		m.syncLogViewport()
		return m, nil
	case dashboardRefreshMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		selectedID, _ := m.selectedAppID()
		m.snapshot = msg.snapshot
		m.selected = m.indexForAppID(selectedID)
		m.err = nil
		m.clampSelection()
		m.syncLogViewport()
		return m, nil
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
	line := fmt.Sprintf("Rhumbase Dashboard | %s | %s", selected, dashboardTabs[m.tab])
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
	if m.focus == dashboardFocusDetail || dashboardTabs[m.tab] == "Logs" {
		line := strings.Join([]string{m.helpView(), fmt.Sprintf("apps %d", len(m.filteredAppOrder()))}, " | ")
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
		return "[?] hide  [/] filter  [j/k] select  [g/G] top/bottom  [enter] detail  [tab] tabs  [u/d] logs  [r] refresh  [q] quit"
	}
	if m.filtering {
		return "[type] filter  [esc] clear  [enter] apply"
	}
	if dashboardTabs[m.tab] == "Logs" {
		return "[u/d] scroll  [pgup/pgdn] page  [tab] tabs  [esc] apps  [?] help"
	}
	if m.focus == dashboardFocusDetail {
		return "[tab] next  [shift+tab] prev  [esc] apps  [?] help"
	}
	return "[?] help  [/] filter  [j/k] select  [g/G] top/bottom  [enter] detail  [r] refresh  [q] quit"
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
	rows := [][]string{
		{"App", metadata.Name},
		{"State", metadata.Status},
		{"Node", metadata.NodeID},
		{"Services", fmt.Sprintf("%d", len(detail.Services()))},
		{"Routes", fmt.Sprintf("%d", len(detail.Domains()))},
		{"Releases", fmt.Sprintf("%d", len(detail.Releases()))},
		{"Deploys", fmt.Sprintf("%d", len(detail.LatestDeployments(5)))},
	}
	return renderDashboardTable(width, []dashboardTableColumn{
		{Header: "Field", MinWidth: 8, Priority: 0},
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
