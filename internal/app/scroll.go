package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) usesTextViewport() bool {
	switch a.screen {
	case screenPodDetails, screenResourceDetails, screenConfirmAction:
		return true
	default:
		return false
	}
}

func (a *App) renderTextViewport(content string, topSections int) string {
	width := max(20, a.width-2)
	height := max(4, a.height-topSections-1)
	a.textViewport.Width = width
	a.textViewport.Height = height
	a.textViewport.SetContent(content)
	if a.textViewport.PastBottom() {
		a.textViewport.GotoBottom()
	}
	lines := 1
	if content != "" {
		lines = len(strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n"))
	}
	visible := min(lines, a.textViewport.Height)
	if lines > a.textViewport.YOffset {
		visible = min(lines-a.textViewport.YOffset, a.textViewport.Height)
	}
	a.visibleRows = visible
	a.totalRows = lines
	return a.textViewport.View()
}

func (a *App) resetTextViewport() {
	a.textViewport.SetYOffset(0)
}

func (a *App) updateTextViewportKeys(msg tea.KeyMsg) bool {
	if !a.usesTextViewport() {
		return false
	}
	switch msg.String() {
	case "j", "down":
		a.textViewport.ScrollDown(1)
		return true
	case "k", "up":
		a.textViewport.ScrollUp(1)
		return true
	case "g", "home":
		a.textViewport.GotoTop()
		return true
	case "G", "end":
		a.textViewport.GotoBottom()
		return true
	case "pgdown", "f":
		a.textViewport.PageDown()
		return true
	case "pgup", "b":
		a.textViewport.PageUp()
		return true
	default:
		return false
	}
}
