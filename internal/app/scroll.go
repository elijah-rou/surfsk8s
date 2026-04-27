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
	resourceDetailPaneStatusStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238")).Padding(0, 1)
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
	now := time.Now()
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		if len(a.activeDeploymentPods) == 0 && a.activeDeployment.Deployment != nil {
			a.activeDeploymentPods = a.deploymentAssociatedPods(now)
		}
		return a.activeDeploymentPods
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		if len(a.activeServicePods) == 0 && a.activeService.Service != nil {
			a.activeServicePods = a.serviceAssociatedPods(now)
		}
		return a.activeServicePods
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		if len(a.activeNodePods) == 0 && a.activeNode.Node != nil {
			a.activeNodePods = a.nodeAssociatedPods(now)
		}
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
	bodyHeight := max(12, a.height-topSections-2)
	paneOuterWidth := max(24, a.width-2)
	paneInnerWidth := max(20, paneOuterWidth-2)
	summaryMinHeight := 4
	tableHeight := resourceDetailPodTableHeight(bodyHeight, a.height, len(rows), summaryMinHeight)
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
	summaryPane := a.renderResourceDetailPane("Details pane", detailFocusContent, "j/k scroll  home/end edge  g owner  G deps  ]/tab → pods", viewportBody, paneOuterWidth)
	statusRow := a.renderResourceDetailStatusRow(len(rows), paneOuterWidth)
	podsPane := a.renderResourceDetailPane(fmt.Sprintf("Pods pane (%d)", len(rows)), detailFocusPods, "hjkl nav  enter open-pod  [/shift+tab → details", a.detailPodTable.View(), paneOuterWidth)
	return summaryPane + "\n" + statusRow + "\n" + podsPane
}

func resourceDetailPodTableHeight(bodyHeight int, shellHeight int, rowCount int, summaryMinHeight int) int {
	const minTableHeight = 4
	desired := max(minTableHeight, rowCount+2)
	maxBodyTableHeight := max(minTableHeight, bodyHeight-6-summaryMinHeight)
	maxShellTableHeight := max(minTableHeight, shellHeight/3-3)
	return min(desired, min(maxBodyTableHeight, maxShellTableHeight))
}

func (a *App) renderResourceDetailPane(title string, focus detailFocusKind, hint string, body string, width int) string {
	marker := "○"
	headerStyle := theme.Muted.Copy()
	hintStyle := theme.Muted.Copy()
	paneStyle := resourceDetailPaneInactiveStyle
	if a.detailFocus == focus {
		marker = "●"
		headerStyle = theme.HeaderStyle.Copy().Bold(true)
		hintStyle = theme.HeaderStyle.Copy()
		paneStyle = resourceDetailPaneActiveStyle
	}
	header := headerStyle.Render(marker+" "+title) + "  " + hintStyle.Render(hint)
	return paneStyle.Width(width).Render(header + "\n" + body)
}

func (a *App) renderResourceDetailStatusRow(podCount int, width int) string {
	focus := "details"
	if a.detailFocus == detailFocusPods {
		focus = "pods"
	}
	text := fmt.Sprintf("Focus: %s  Pods: %d  [ details  ] pods", focus, podCount)
	return resourceDetailPaneStatusStyle.Width(width).Render(text)
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
	case "home":
		a.textViewport.GotoTop()
		return true
	case "end":
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
