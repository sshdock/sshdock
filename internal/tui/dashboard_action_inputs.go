package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

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
			choices = append(choices, dashboardActionChoice{label: service.Name + " " + valueOrDash(service.State), value: service.Name})
		}
		return choices
	case dashboardActionRollback:
		releases := selected.Detail.Releases()
		choices := make([]dashboardActionChoice, 0, len(releases))
		for _, release := range releases {
			choices = append(choices, dashboardActionChoice{label: release.ID + " " + valueOrDash(release.Status), value: release.ID})
		}
		return choices
	case dashboardActionDetachDomain:
		domains := selected.Detail.Domains()
		choices := make([]dashboardActionChoice, 0, len(domains))
		for _, domain := range domains {
			choices = append(choices, dashboardActionChoice{label: domain.DomainName + " -> " + valueOrDash(domain.Target), value: domain.DomainName})
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
	case dashboardActionRestartService, dashboardActionRollback:
		return m, m.executeAction(dashboardActionRequest{kind: m.pendingAction, primary: choice.value})
	case dashboardActionDetachDomain:
		return m, m.executeAction(dashboardActionRequest{kind: m.pendingAction, secondary: choice.value})
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
	return m, m.executeAction(dashboardActionRequest{kind: dashboardActionAttachDomain, primary: parts[0], secondary: parts[1], port: port})
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
	return m, m.executeAction(dashboardActionRequest{kind: dashboardActionRemoveApp})
}
