package app

import tea "github.com/charmbracelet/bubbletea"

func (a *App) updateMouse(msg tea.MouseMsg) tea.Cmd {
	if a.filter.Active() {
		return nil
	}
	if msg.Action != tea.MouseActionPress {
		return nil
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return a.scrollWheel(-1)
	case tea.MouseButtonWheelDown:
		return a.scrollWheel(1)
	default:
		return nil
	}
}

func (a *App) scrollWheel(direction int) tea.Cmd {
	if direction != -1 && direction != 1 {
		panic("app.scrollWheel: invalid direction")
	}

	if a.usesTextViewport() {
		var cmd tea.Cmd
		a.textViewport, cmd = a.textViewport.Update(wheelMouseMsg(direction))
		return cmd
	}

	steps := 3
	if direction < 0 {
		steps = 3
	}

	switch a.screen {
	case screenContexts, screenCatalog, screenGroupResources, screenCommands, screenActionPicker:
		if direction < 0 {
			a.navTable.MoveUp(steps)
		} else {
			a.navTable.MoveDown(steps)
		}
	case screenPods:
		if direction < 0 {
			a.podTable.MoveUp(steps)
		} else {
			a.podTable.MoveDown(steps)
		}
	case screenResourceList:
		if direction < 0 {
			a.resourceTable.MoveUp(steps)
		} else {
			a.resourceTable.MoveDown(steps)
		}
	}
	return nil
}

func wheelMouseMsg(direction int) tea.MouseMsg {
	if direction != -1 && direction != 1 {
		panic("app.wheelMouseMsg: invalid direction")
	}
	button := tea.MouseButtonWheelDown
	if direction < 0 {
		button = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: button}
}
