package tui

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type DashboardRefreshFunc func(context.Context) (DashboardSnapshot, error)

type InteractiveDashboardModel struct {
	snapshot DashboardSnapshot
	refresh  DashboardRefreshFunc

	selected int
	tab      int
	width    int
	height   int
	showHelp bool
	err      error
	logs     viewport.Model
}

type dashboardRefreshMsg struct {
	snapshot DashboardSnapshot
	err      error
}

var dashboardTabs = []string{"Overview", "Domains", "Releases", "Deployments", "Logs"}

func NewInteractiveDashboardModel(snapshot DashboardSnapshot, refresh DashboardRefreshFunc) InteractiveDashboardModel {
	model := InteractiveDashboardModel{
		snapshot: snapshot,
		refresh:  refresh,
		width:    100,
		height:   28,
		showHelp: true,
		logs:     viewport.New(80, 8),
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
		m.resizeViewport()
		m.syncLogViewport()
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "down", "j":
			m.moveSelection(1)
		case "up", "k":
			m.moveSelection(-1)
		case "tab":
			m.tab = (m.tab + 1) % len(dashboardTabs)
			m.logs.GotoTop()
		case "shift+tab":
			m.tab = (m.tab + len(dashboardTabs) - 1) % len(dashboardTabs)
			m.logs.GotoTop()
		case "pgdown", "d":
			m.logs.PageDown()
		case "pgup", "u":
			m.logs.PageUp()
		case "?":
			m.showHelp = !m.showHelp
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
		m.snapshot = msg.snapshot
		m.selected = 0
		m.tab = 0
		m.err = nil
		m.syncLogViewport()
		return m, nil
	default:
		var cmd tea.Cmd
		m.logs, cmd = m.logs.Update(msg)
		return m, cmd
	}
}

func (m InteractiveDashboardModel) View() string {
	if m.width < 80 {
		return m.compactView()
	}

	left := lipgloss.NewStyle().Width(30).PaddingRight(2).Render(m.appListView())
	right := lipgloss.NewStyle().Width(maxInt(40, m.width-34)).Render(m.detailView())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return strings.TrimRight(strings.Join([]string{m.headerView(), body, m.helpView()}, "\n"), "\n")
}

func (m InteractiveDashboardModel) LogOffset() int {
	return m.logs.YOffset
}

func (m *InteractiveDashboardModel) moveSelection(delta int) {
	order := m.appOrder()
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

func (m InteractiveDashboardModel) headerView() string {
	title := lipgloss.NewStyle().Bold(true).Render("Rhumbase")
	selected := "No apps"
	if detail, ok := m.selectedApp(); ok {
		metadata := detail.Detail.Metadata()
		selected = fmt.Sprintf("%s %s", metadata.Name, metadata.Status)
	}
	if m.err != nil {
		return fmt.Sprintf("%s  %s\n%s", title, selected, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(m.err.Error()))
	}
	return fmt.Sprintf("%s  %s", title, selected)
}

func (m InteractiveDashboardModel) appListView() string {
	var builder strings.Builder
	builder.WriteString("Apps\n")
	rows := m.snapshot.Apps.Rows()
	if len(rows) == 0 {
		builder.WriteString("  No apps\n")
		return builder.String()
	}
	for i, row := range rows {
		cursor := " "
		if i == m.selected {
			cursor = ">"
		}
		fmt.Fprintf(&builder, "%s %s %s latest=%s domains=%d\n", cursor, row.Name, row.Status, valueOrDash(row.LatestReleaseStatus), row.DomainCount)
	}
	return builder.String()
}

func (m InteractiveDashboardModel) detailView() string {
	selected, ok := m.selectedApp()
	if !ok {
		return "No apps\n"
	}

	var builder strings.Builder
	metadata := selected.Detail.Metadata()
	fmt.Fprintf(&builder, "App %s\nStatus: %s\nNode: %s\n", metadata.Name, metadata.Status, metadata.NodeID)
	builder.WriteString(m.tabsView())
	builder.WriteString("\n")

	switch dashboardTabs[m.tab] {
	case "Overview":
		builder.WriteString(renderServiceBlock(selected.Detail.Services()))
		builder.WriteString("\n")
		builder.WriteString(m.logsBlock())
	case "Domains":
		builder.WriteString(renderDomainBlock(selected.Detail.Domains()))
	case "Releases":
		builder.WriteString(renderReleaseBlock(selected.Detail.Releases()))
	case "Deployments":
		builder.WriteString(renderDeploymentBlock(selected.Detail.LatestDeployments(5)))
	case "Logs":
		builder.WriteString(m.logsBlock())
	}
	return builder.String()
}

func (m InteractiveDashboardModel) compactView() string {
	return strings.TrimRight(strings.Join([]string{
		m.headerView(),
		m.appListView(),
		m.detailView(),
		m.helpView(),
	}, "\n"), "\n")
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
	if !m.showHelp {
		return "? help"
	}
	return "j/k or arrows select  tab switch  u/d scroll logs  r refresh  q quit  ? hide help"
}

func (m InteractiveDashboardModel) selectedApp() (DashboardAppSnapshot, bool) {
	order := m.appOrder()
	if len(order) == 0 || m.selected < 0 || m.selected >= len(order) {
		return DashboardAppSnapshot{}, false
	}
	snapshot, ok := m.snapshot.AppsByID[order[m.selected]]
	return snapshot, ok
}

func (m InteractiveDashboardModel) appOrder() []string {
	if len(m.snapshot.AppOrder) > 0 {
		return append([]string(nil), m.snapshot.AppOrder...)
	}
	rows := m.snapshot.Apps.Rows()
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		order = append(order, row.Name)
	}
	return order
}

func (m *InteractiveDashboardModel) resizeViewport() {
	width := maxInt(20, m.width-36)
	height := maxInt(3, m.height-14)
	if m.width < 80 {
		width = maxInt(20, m.width-4)
		height = maxInt(3, m.height-18)
	}
	m.logs.Width = width
	m.logs.Height = height
}

func (m *InteractiveDashboardModel) syncLogViewport() {
	m.resizeViewport()
	m.logs.SetContent(logsText(m.selectedLogs()))
}

func (m InteractiveDashboardModel) logsBlock() string {
	return "Logs\n" + m.logs.View()
}

func (m InteractiveDashboardModel) selectedLogs() map[string]LogsView {
	selected, ok := m.selectedApp()
	if !ok {
		return nil
	}
	return selected.Logs
}

func logsText(logsByService map[string]LogsView) string {
	if len(logsByService) == 0 {
		return "- none"
	}
	names := make([]string, 0, len(logsByService))
	for name := range logsByService {
		names = append(names, name)
	}
	sort.Strings(names)

	var builder strings.Builder
	for _, name := range names {
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		fmt.Fprintf(&builder, "Logs %s\n", name)
		screen := NewLogsScreen(logsByService[name])
		if len(screen.Lines()) == 0 {
			builder.WriteString("- none\n")
			continue
		}
		for _, line := range screen.Lines() {
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}
	return strings.TrimRight(builder.String(), "\n")
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

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
