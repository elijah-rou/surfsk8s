package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/theme"
)

var (
	resourceDetailPaneActiveStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("14"))
	resourceDetailPaneInactiveStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))
)

func (a *App) usesTextViewport() bool {
	switch a.screen {
	case screenPodDetails, screenConfirmAction, screenLogs:
		return true
	case screenResourceDetails:
		return !a.resourceDetailHasPodTable()
	default:
		return false
	}
}

func (a *App) renderTextViewport(content string, topSections int) string {
	return a.renderTextViewportWithReserved(content, topSections, 0)
}

func (a *App) renderTextViewportWithReserved(content string, topSections int, reservedBottom int) string {
	width := max(20, a.width-2)
	height := max(4, a.height-topSections-1-reservedBottom)
	return a.renderTextViewportSized(content, width, height)
}

func (a *App) renderTextViewportSized(content string, width int, height int) string {
	a.textViewport.Width = width
	a.textViewport.Height = height
	if content != a.textViewportContent {
		a.textViewport.SetContent(content)
		a.textViewportContent = content
		a.textViewportLineCount = 1
		if content != "" {
			a.textViewportLineCount = len(strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n"))
		}
		if a.textViewport.PastBottom() {
			a.textViewport.GotoBottom()
		}
	}
	lines := max(1, a.textViewportLineCount)
	visible := min(lines, a.textViewport.Height)
	if lines > a.textViewport.YOffset {
		visible = min(lines-a.textViewport.YOffset, a.textViewport.Height)
	}
	a.visibleRows = visible
	a.totalRows = lines
	return a.textViewport.View()
}

func (a *App) resourceDetailHasPodTable() bool {
	if a.screen != screenResourceDetails {
		return false
	}
	return len(a.currentAssociatedPodRows()) != 0
}

func (a *App) currentAssociatedPodRows() []state.PodRow {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		return a.activeDeploymentPods
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		return a.activeServicePods
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		return a.activeNodePods
	default:
		return nil
	}
}

func (a *App) renderResourceDetailBody(content string, topSections int) string {
	rows := a.currentAssociatedPodRows()
	if len(rows) == 0 {
		return a.renderTextViewport(content, topSections)
	}
	bodyHeight := max(12, a.height-topSections-1)
	paneOuterWidth := max(24, a.width-2)
	paneInnerWidth := max(20, paneOuterWidth-2)
	summaryMinHeight := 4
	maxTableHeight := max(6, bodyHeight-6-summaryMinHeight)
	tableHeight := min(max(6, len(rows)+2), maxTableHeight)
	summaryHeight := max(summaryMinHeight, bodyHeight-6-tableHeight)
	viewportBody := a.renderTextViewportSized(content, paneInnerWidth, summaryHeight)
	a.detailPodTable.SetColumns(a.currentPodColumns())
	a.detailPodTable.SetSize(paneInnerWidth, tableHeight)
	a.detailPodTable.SetWindowProvider(len(rows), func(start int, end int) [][]string {
		if start < 0 {
			start = 0
		}
		if end > len(rows) {
			end = len(rows)
		}
		if start > end {
			start = end
		}
		return a.podTableRows(rows[start:end], time.Now())
	})
	summaryPane := a.renderResourceDetailPane("Details pane", detailFocusContent, "]/tab → pods", viewportBody, paneOuterWidth)
	podsPane := a.renderResourceDetailPane(fmt.Sprintf("Pods pane (%d)", len(rows)), detailFocusPods, "[/shift+tab → details", a.detailPodTable.View(), paneOuterWidth)
	return summaryPane + "\n" + podsPane
}

func (a *App) renderResourceDetailPane(title string, focus detailFocusKind, hint string, body string, width int) string {
	marker := "○"
	headerStyle := theme.Muted.Copy()
	paneStyle := resourceDetailPaneInactiveStyle
	if a.detailFocus == focus {
		marker = "●"
		headerStyle = theme.HeaderStyle.Copy().Bold(true)
		paneStyle = resourceDetailPaneActiveStyle
	}
	header := headerStyle.Render(marker+" "+title) + "  " + theme.Muted.Render(hint)
	return paneStyle.Width(width).Render(header + "\n" + body)
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
