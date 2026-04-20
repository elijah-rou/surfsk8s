package app

import (
	"context"
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
	inputModeResourceFinder
	inputModeScale
	inputModeLocalPort
	inputModeScopePicker
	inputModeTableFilterValue
)

type actionResultMsg struct {
	description string
	err         error
}

func (a *App) statusInputState() (string, string, bool) {
	if a.filter.Active() {
		switch a.inputMode {
		case inputModeCommand:
			return "cmd", a.filter.Value(), true
		case inputModeScale:
			return "replicas", a.filter.Value(), true
		case inputModeResourceFinder:
			return "resource", a.filter.Value(), true
		case inputModeLocalPort:
			return "local-port", a.filter.Value(), true
		case inputModeScopePicker:
			if a.scopePickerKind == scopePickerContext {
				return "context", a.filter.Value(), true
			}
			return "namespace", a.filter.Value(), true
		case inputModeTableFilterValue:
			return "column-filter", a.filter.Value(), true
		default:
			return "filter", a.filter.Value(), true
		}
	}
	if a.screen == screenCommands {
		return "cmd", a.commandQuery, false
	}
	if a.screen == screenResourceFinder {
		return "resource", a.resourceFinderQuery, false
	}
	if a.screen == screenScopePicker {
		if a.scopePickerKind == scopePickerContext {
			return "context", a.scopeQuery, false
		}
		return "namespace", a.scopeQuery, false
	}
	return "filter", a.currentQuery(), false
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

func (a *App) updateCommandPrompt(msg tea.Msg) tea.Cmd {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.String() {
		case "esc":
			if strings.TrimSpace(a.commandQuery) != "" {
				a.commandQuery = ""
				a.filter.Clear()
				a.refreshCommands()
				return nil
			}
			a.inputMode = inputModeSearch
			a.filter.Deactivate()
			a.screen = a.prevScreen
			a.refreshCurrentScreen(time.Now())
			return nil
		case "j", "down":
			a.navTable.MoveDown(1)
			return nil
		case "k", "up":
			a.navTable.MoveUp(1)
			return nil
		case "g", "home":
			a.navTable.MoveTop()
			return nil
		case "G", "end":
			a.navTable.MoveBottom()
			return nil
		case "enter":
			index := a.navTable.SelectedIndex()
			if index < 0 || index >= len(a.visibleCommands) {
				a.statusMessage = "no command selected"
				return nil
			}
			command := a.visibleCommands[index]
			a.inputMode = inputModeSearch
			a.filter.Deactivate()
			command.Run(a)
			return nil
		}
	}

	cmd := a.filter.Update(msg)
	a.commandQuery = a.filter.Value()
	a.refreshCommands()
	return cmd
}

func (a *App) updateScalePrompt(msg tea.Msg) tea.Cmd {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.String() {
		case "esc":
			a.cancelActionFlow()
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
			return a.confirmScaleDeployment(replicas)
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
	a.actionReturnScreen = a.screen
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
	containers, err := actions.PodContainerNames(a.activePod.Pod)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	if len(containers) == 1 {
		return a.runExecPodWithContainer(containers[0])
	}
	return a.openPodContainerPicker(containers)
}

func (a *App) runEditPod() tea.Cmd {
	_, description, err := a.executor.EditPod(a.activePod)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	return a.openConfirmAction(description, func() tea.Cmd {
		details, ok := a.store.PodDetailsByKey(a.activePod.Row.Key, time.Now())
		if !ok {
			a.statusMessage = "pod vanished during refresh"
			a.refreshCurrentScreen(time.Now())
			return nil
		}
		a.activePod = details
		cmd, description, err := a.executor.EditPod(details)
		if err != nil {
			a.statusMessage = err.Error()
			return nil
		}
		return runProcessCommand(cmd, description)
	})
}

func (a *App) runPortForwardPod() tea.Cmd {
	choices, err := actions.PodPortChoices(a.activePod.Pod)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	if len(choices) == 1 {
		return a.openLocalPortPrompt(pendingActionPortForwardPod, choices[0].Port)
	}
	return a.openPodPortPicker(choices)
}

func (a *App) runEditResource() tea.Cmd {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		_, description, err := a.executor.EditDeployment(a.activeDeployment)
		if err != nil {
			a.statusMessage = err.Error()
			return nil
		}
		return a.openConfirmAction(description, func() tea.Cmd {
			details, ok := a.store.DeploymentDetailsByKey(a.activeDeployment.Row.Key, time.Now())
			if !ok {
				a.statusMessage = "resource vanished during refresh"
				a.refreshCurrentScreen(time.Now())
				return nil
			}
			a.activeDeployment = details
			cmd, description, err := a.executor.EditDeployment(details)
			if err != nil {
				a.statusMessage = err.Error()
				return nil
			}
			return runProcessCommand(cmd, description)
		})
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		_, description, err := a.executor.EditService(a.activeService)
		if err != nil {
			a.statusMessage = err.Error()
			return nil
		}
		return a.openConfirmAction(description, func() tea.Cmd {
			details, ok := a.store.ServiceDetailsByKey(a.activeService.Row.Key, time.Now())
			if !ok {
				a.statusMessage = "resource vanished during refresh"
				a.refreshCurrentScreen(time.Now())
				return nil
			}
			a.activeService = details
			cmd, description, err := a.executor.EditService(details)
			if err != nil {
				a.statusMessage = err.Error()
				return nil
			}
			return runProcessCommand(cmd, description)
		})
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		_, description, err := a.executor.EditNode(a.activeNode)
		if err != nil {
			a.statusMessage = err.Error()
			return nil
		}
		return a.openConfirmAction(description, func() tea.Cmd {
			details, ok := a.store.NodeDetailsByKey(a.activeNode.Row.Key, time.Now())
			if !ok {
				a.statusMessage = "resource vanished during refresh"
				a.refreshCurrentScreen(time.Now())
				return nil
			}
			a.activeNode = details
			cmd, description, err := a.executor.EditNode(details)
			if err != nil {
				a.statusMessage = err.Error()
				return nil
			}
			return runProcessCommand(cmd, description)
		})
	default:
		_, description, err := a.executor.EditGenericResource(a.activeResource, a.activeGenericDetails)
		if err != nil {
			a.statusMessage = err.Error()
			return nil
		}
		return a.openConfirmAction(description, func() tea.Cmd {
			details, err := a.manager.GenericResourceDetails(context.Background(), a.activeResource, a.activeGenericDetails.Row.Key, time.Now())
			if err != nil {
				a.statusMessage = err.Error()
				a.refreshCurrentScreen(time.Now())
				return nil
			}
			a.activeGenericDetails = details
			cmd, description, err := a.executor.EditGenericResource(a.activeResource, details)
			if err != nil {
				a.statusMessage = err.Error()
				return nil
			}
			return runProcessCommand(cmd, description)
		})
	}
}

func (a *App) runPortForwardResource() tea.Cmd {
	if a.activeResource.Resource != "services" || a.activeResource.APIGroup != "" {
		a.statusMessage = "port-forward unsupported for this resource"
		return nil
	}
	choices, err := actions.ServicePortChoices(a.activeService.Service)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	if len(choices) == 1 {
		return a.openLocalPortPrompt(pendingActionPortForwardService, choices[0].Port)
	}
	return a.openServicePortPicker(choices)
}

func (a *App) confirmScaleDeployment(replicas int) tea.Cmd {
	_, description, err := a.executor.ScaleDeployment(a.activeDeployment, replicas)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	return a.openConfirmAction(description, func() tea.Cmd {
		details, ok := a.store.DeploymentDetailsByKey(a.activeDeployment.Row.Key, time.Now())
		if !ok {
			a.statusMessage = "resource vanished during refresh"
			a.refreshCurrentScreen(time.Now())
			return nil
		}
		a.activeDeployment = details
		cmd, description, err := a.executor.ScaleDeployment(details, replicas)
		if err != nil {
			a.statusMessage = err.Error()
			return nil
		}
		return runProcessCommand(cmd, description)
	})
}

func (a *App) runRestartResource() tea.Cmd {
	if a.activeResource.Resource != "deployments" || a.activeResource.APIGroup != "apps" {
		a.statusMessage = "restart unsupported for this resource"
		return nil
	}
	_, description, err := a.executor.RestartDeployment(a.activeDeployment)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	return a.openConfirmAction(description, func() tea.Cmd {
		details, ok := a.store.DeploymentDetailsByKey(a.activeDeployment.Row.Key, time.Now())
		if !ok {
			a.statusMessage = "resource vanished during refresh"
			a.refreshCurrentScreen(time.Now())
			return nil
		}
		a.activeDeployment = details
		cmd, description, err := a.executor.RestartDeployment(details)
		if err != nil {
			a.statusMessage = err.Error()
			return nil
		}
		return runProcessCommand(cmd, description)
	})
}
