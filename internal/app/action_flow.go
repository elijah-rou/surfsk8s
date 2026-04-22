package app

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elijahrou/surfsk8s/internal/actions"
)

type pendingAction uint8

const (
	pendingActionNone pendingAction = iota
	pendingActionExecPod
	pendingActionPortForwardPod
	pendingActionPortForwardService
	pendingActionPodLogsSources
	pendingActionDeploymentLogsSources
	pendingActionNodeLogsPath
	pendingActionLogRange
)

type actionOption struct {
	Label     string
	Container string
	Port      int
	Path      string
	Directory bool
	Selected  bool
	LogRange  logRange
}

func (a *App) openActionPicker(title string, footer string, action pendingAction, options []actionOption) tea.Cmd {
	if len(options) == 0 {
		panic("app.openActionPicker: empty options")
	}
	returnScreen := a.screen
	if a.screen == screenActionPicker {
		returnScreen = a.actionReturnScreen
	}
	a.actionReturnScreen = returnScreen
	a.pendingAction = action
	a.actionPickerTitle = title
	a.actionPickerFooter = footer
	a.actionPickerOptions = append(a.actionPickerOptions[:0], options...)
	a.screen = screenActionPicker
	a.refreshActionPicker()
	return nil
}

func (a *App) actionPickerIsToggleMode() bool {
	switch a.pendingAction {
	case pendingActionPodLogsSources, pendingActionDeploymentLogsSources:
		return true
	default:
		return false
	}
}

func (a *App) refreshActionPicker() {
	rows := make([][]string, 0, len(a.actionPickerOptions))
	for _, option := range a.actionPickerOptions {
		label := option.Label
		if a.actionPickerIsToggleMode() {
			state := "[ ] "
			if option.Selected {
				state = "[x] "
			}
			label = state + label
		}
		rows = append(rows, []string{label})
	}
	a.visibleRows = len(rows)
	a.totalRows = len(rows)
	a.setNavTable(strings.ToUpper(a.actionPickerTitle), rows)
	a.navTable.MoveTop()
}

func (a *App) updateActionPickerKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		a.navTable.MoveDown(1)
	case "k", "up":
		a.navTable.MoveUp(1)
	case "g", "home":
		a.navTable.MoveTop()
	case "G", "end":
		a.navTable.MoveBottom()
	case " ":
		if a.actionPickerIsToggleMode() {
			index := a.navTable.SelectedIndex()
			if index >= 0 && index < len(a.actionPickerOptions) {
				a.actionPickerOptions[index].Selected = !a.actionPickerOptions[index].Selected
				a.refreshActionPicker()
				a.navTable.MoveDown(index)
			}
		}
	case "esc", "backspace":
		a.cancelActionFlow()
	case "enter":
		if a.actionPickerIsToggleMode() {
			return a.applyToggleActionPicker()
		}
		index := a.navTable.SelectedIndex()
		if index < 0 || index >= len(a.actionPickerOptions) {
			a.statusMessage = "selection disappeared"
			return nil
		}
		return a.handleActionOption(a.actionPickerOptions[index])
	}
	return nil
}

func (a *App) applyToggleActionPicker() tea.Cmd {
	switch a.pendingAction {
	case pendingActionPodLogsSources:
		selected := make(map[string]bool, len(a.actionPickerOptions))
		for _, option := range a.actionPickerOptions {
			if option.Selected {
				selected[option.Container] = true
			}
		}
		if len(selected) == 0 {
			a.statusMessage = "select at least one container"
			return nil
		}
		a.logSelectedContainers = selected
		a.screen = a.actionReturnScreen
		a.resetActionFlowState()
		return a.startPodLogs()
	case pendingActionDeploymentLogsSources:
		selected := make(map[string]bool, len(a.actionPickerOptions))
		for _, option := range a.actionPickerOptions {
			if option.Selected {
				selected[option.Container] = true
			}
		}
		if len(selected) == 0 {
			a.statusMessage = "select at least one container"
			return nil
		}
		a.logSelectedContainers = selected
		a.screen = a.actionReturnScreen
		a.resetActionFlowState()
		return a.startDeploymentLogs()
	default:
		a.statusMessage = "unsupported toggle action"
		return nil
	}
}

func (a *App) handleActionOption(option actionOption) tea.Cmd {
	switch a.pendingAction {
	case pendingActionExecPod:
		return a.runExecPodWithContainer(option.Container)
	case pendingActionPortForwardPod, pendingActionPortForwardService:
		return a.openLocalPortPrompt(a.pendingAction, option.Port)
	case pendingActionNodeLogsPath:
		a.screen = a.actionReturnScreen
		a.resetActionFlowState()
		if option.Directory {
			return a.openNodeLogPicker(option.Path)
		}
		return a.startNodeLogs(option.Path)
	case pendingActionLogRange:
		a.screen = a.actionReturnScreen
		a.resetActionFlowState()
		a.logRange = option.LogRange
		return a.openLogsScreen(a.logTitle, true)
	default:
		a.statusMessage = "unsupported action selection"
		a.cancelActionFlow()
		return nil
	}
}

func (a *App) openConfirmAction(description string, run func() tea.Cmd) tea.Cmd {
	if run == nil {
		panic("app.openConfirmAction: nil run")
	}
	a.actionReturnScreen = a.screen
	a.confirmDescription = description
	a.confirmRun = run
	a.screen = screenConfirmAction
	a.resetTextViewport()
	a.visibleRows = 0
	a.totalRows = 0
	return nil
}

func (a *App) updateConfirmActionKeys(msg tea.KeyMsg) tea.Cmd {
	if a.updateTextViewportKeys(msg) {
		return nil
	}
	switch msg.String() {
	case "esc", "backspace":
		a.cancelActionFlow()
		return nil
	case "enter":
		if a.confirmRun == nil {
			a.statusMessage = "confirmation action disappeared"
			a.cancelActionFlow()
			return nil
		}
		run := a.confirmRun
		a.screen = a.actionReturnScreen
		a.resetActionFlowState()
		return run()
	}
	return nil
}

func (a *App) renderConfirmAction() string {
	if a.confirmDescription == "" {
		return "confirm action"
	}
	return strings.Join([]string{
		"Confirm cluster change:",
		"",
		a.confirmDescription,
		"",
		"Enter to proceed. Esc to cancel.",
	}, "\n")
}

func (a *App) cancelActionFlow() {
	a.resetActionFlowState()
	a.inputMode = inputModeSearch
	a.filter.Clear()
	a.filter.Deactivate()
	a.screen = a.actionReturnScreen
	a.refreshCurrentScreen(time.Now())
}

func (a *App) resetActionFlowState() {
	a.pendingAction = pendingActionNone
	a.pendingRemotePort = 0
	a.actionPickerOptions = a.actionPickerOptions[:0]
	a.actionPickerTitle = ""
	a.actionPickerFooter = ""
	a.confirmRun = nil
	a.confirmDescription = ""
}

func (a *App) openLocalPortPrompt(action pendingAction, remotePort int) tea.Cmd {
	if remotePort <= 0 {
		panic("app.openLocalPortPrompt: invalid remotePort")
	}
	a.pendingAction = action
	a.pendingRemotePort = remotePort
	a.screen = a.actionReturnScreen
	a.inputMode = inputModeLocalPort
	a.filter.SetPrompt("local-port> ")
	a.filter.SetPlaceholder("local port")
	a.filter.SetValue(strconv.Itoa(remotePort))
	a.filter.Activate()
	return nil
}

func (a *App) updateLocalPortPrompt(msg tea.Msg) tea.Cmd {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.String() {
		case "esc":
			a.cancelActionFlow()
			return nil
		case "enter":
			value := strings.TrimSpace(a.filter.Value())
			localPort, err := strconv.Atoi(value)
			if err != nil || localPort <= 0 || localPort > 65535 {
				a.statusMessage = "local port must be between 1 and 65535"
				return nil
			}
			a.inputMode = inputModeSearch
			a.filter.Deactivate()
			switch a.pendingAction {
			case pendingActionPortForwardPod:
				return a.runPortForwardPodWithMapping(localPort, a.pendingRemotePort)
			case pendingActionPortForwardService:
				return a.runPortForwardServiceWithMapping(localPort, a.pendingRemotePort)
			default:
				a.statusMessage = "unsupported local port action"
				return nil
			}
		}
	}
	return a.filter.Update(msg)
}

func (a *App) runExecPodWithContainer(container string) tea.Cmd {
	a.screen = a.actionReturnScreen
	cmd, description, err := a.executor.ExecPodShellInContainer(a.activePod, container)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	a.resetActionFlowState()
	return runProcessCommand(cmd, description)
}

func (a *App) runPortForwardPodWithMapping(localPort int, remotePort int) tea.Cmd {
	cmd, description, err := a.executor.PortForwardPodWithPorts(a.activePod, localPort, remotePort)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	a.resetActionFlowState()
	return runProcessCommand(cmd, description)
}

func (a *App) runPortForwardServiceWithMapping(localPort int, remotePort int) tea.Cmd {
	cmd, description, err := a.executor.PortForwardServiceWithPorts(a.activeService, localPort, remotePort)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	a.resetActionFlowState()
	return runProcessCommand(cmd, description)
}

func buildContainerOptions(names []string) []actionOption {
	options := make([]actionOption, 0, len(names))
	for _, name := range names {
		options = append(options, actionOption{Label: name, Container: name})
	}
	return options
}

func buildPortOptions(choices []actions.PortChoice) []actionOption {
	options := make([]actionOption, 0, len(choices))
	for _, choice := range choices {
		options = append(options, actionOption{Label: choice.Label, Port: choice.Port})
	}
	return options
}

func (a *App) openPodContainerPicker(names []string) tea.Cmd {
	return a.openActionPicker("select container", "enter select  esc cancel", pendingActionExecPod, buildContainerOptions(names))
}

func (a *App) openPodPortPicker(choices []actions.PortChoice) tea.Cmd {
	return a.openActionPicker("select pod port", "enter select  esc cancel", pendingActionPortForwardPod, buildPortOptions(choices))
}

func (a *App) openServicePortPicker(choices []actions.PortChoice) tea.Cmd {
	return a.openActionPicker("select service port", "enter select  esc cancel", pendingActionPortForwardService, buildPortOptions(choices))
}
