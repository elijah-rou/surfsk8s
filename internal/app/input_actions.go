package app

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elijahrou/surfsk8s/internal/actions"
)

type inputMode uint8

const (
	inputModeSearch inputMode = iota
	inputModeCommand
	inputModeScale
)

type actionResultMsg struct {
	description string
	err         error
}

func (m inputMode) usesFilterLabel() bool {
	switch m {
	case inputModeSearch, inputModeCommand:
		return true
	default:
		return false
	}
}

func (a *App) statusFilterValue() string {
	if !a.inputMode.usesFilterLabel() {
		return a.currentQuery()
	}
	return a.currentQuery()
}

func (a *App) updateSearchPrompt(msg tea.Msg) tea.Cmd {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.String() {
		case "esc":
			a.filter.Clear()
			a.filter.Deactivate()
			a.setCurrentQuery("")
			a.refreshCurrentScreen(time.Now())
			return nil
		case "enter":
			a.filter.Deactivate()
			a.setCurrentQuery(a.filter.Value())
			a.refreshCurrentScreen(time.Now())
			return nil
		}
	}

	cmd := a.filter.Update(msg)
	a.setCurrentQuery(a.filter.Value())
	a.refreshCurrentScreen(time.Now())
	return cmd
}

func (a *App) updateScalePrompt(msg tea.Msg) tea.Cmd {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.String() {
		case "esc":
			a.inputMode = inputModeSearch
			a.filter.Clear()
			a.filter.Deactivate()
			return nil
		case "enter":
			value := strings.TrimSpace(a.filter.Value())
			replicas, err := strconv.Atoi(value)
			if err != nil || replicas < 0 {
				a.statusMessage = "replicas must be a non-negative integer"
				return nil
			}
			a.inputMode = inputModeSearch
			a.filter.Deactivate()
			return a.runScaleDeployment(replicas)
		}
	}
	return a.filter.Update(msg)
}

func (a *App) openScalePrompt() tea.Cmd {
	if a.activeResource.Resource != "deployments" || a.activeResource.APIGroup != "apps" {
		a.statusMessage = "scale unsupported for this resource"
		return nil
	}
	if a.activeDeployment.Deployment == nil {
		a.statusMessage = "deployment disappeared"
		return nil
	}
	currentReplicas := actions.DesiredReplicas(a.activeDeployment.Deployment)
	a.inputMode = inputModeScale
	a.filter.SetPrompt("replicas> ")
	a.filter.SetPlaceholder("replica count")
	a.filter.SetValue(strconv.Itoa(currentReplicas))
	a.filter.Activate()
	return nil
}

func runProcessCommand(cmd *exec.Cmd, description string) tea.Cmd {
	if cmd == nil {
		panic("app.runProcessCommand: nil cmd")
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return actionResultMsg{description: description, err: err}
	})
}

func (a *App) runExecPod() tea.Cmd {
	cmd, description, err := a.executor.ExecPodShell(a.activePod)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	return runProcessCommand(cmd, description)
}

func (a *App) runEditPod() tea.Cmd {
	cmd, description, err := a.executor.EditPod(a.activePod)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	return runProcessCommand(cmd, description)
}

func (a *App) runPortForwardPod() tea.Cmd {
	cmd, description, err := a.executor.PortForwardPod(a.activePod)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	return runProcessCommand(cmd, description)
}

func (a *App) runEditResource() tea.Cmd {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		cmd, description, err := a.executor.EditDeployment(a.activeDeployment)
		if err != nil {
			a.statusMessage = err.Error()
			return nil
		}
		return runProcessCommand(cmd, description)
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		cmd, description, err := a.executor.EditService(a.activeService)
		if err != nil {
			a.statusMessage = err.Error()
			return nil
		}
		return runProcessCommand(cmd, description)
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		cmd, description, err := a.executor.EditNode(a.activeNode)
		if err != nil {
			a.statusMessage = err.Error()
			return nil
		}
		return runProcessCommand(cmd, description)
	default:
		a.statusMessage = "edit unsupported for this resource"
		return nil
	}
}

func (a *App) runPortForwardResource() tea.Cmd {
	if a.activeResource.Resource != "services" || a.activeResource.APIGroup != "" {
		a.statusMessage = "port-forward unsupported for this resource"
		return nil
	}
	cmd, description, err := a.executor.PortForwardService(a.activeService)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	return runProcessCommand(cmd, description)
}

func (a *App) runScaleDeployment(replicas int) tea.Cmd {
	cmd, description, err := a.executor.ScaleDeployment(a.activeDeployment, replicas)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	return runProcessCommand(cmd, description)
}

func (a *App) runRestartResource() tea.Cmd {
	if a.activeResource.Resource != "deployments" || a.activeResource.APIGroup != "apps" {
		a.statusMessage = "restart unsupported for this resource"
		return nil
	}
	cmd, description, err := a.executor.RestartDeployment(a.activeDeployment)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	return runProcessCommand(cmd, description)
}

func (a *App) describeAction() string {
	switch a.inputMode {
	case inputModeScale:
		return fmt.Sprintf("scale %s", a.activeDeployment.Row.Name)
	default:
		return ""
	}
}
