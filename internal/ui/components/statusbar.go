package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/elijahrou/surfsk8s/internal/ui/theme"
)

type ClusterStatus struct {
	Name    string
	Healthy bool
	Synced  bool
	Message string
	Warning string
}

type StatusBarState struct {
	Clusters    []ClusterStatus
	Context     string
	Namespace   string
	InputLabel  string
	InputValue  string
	InputActive bool
	VisibleRows int
	TotalRows   int
	Footer      string
	Activity    string
}

// StatusBar shows cluster connection health, active context, namespace, and input state.
type StatusBar struct{}

func NewStatusBar() StatusBar {
	return StatusBar{}
}

func (s StatusBar) View(state StatusBarState) string {
	parts := make([]string, 0, 7)
	parts = append(parts, renderClusters(state.Clusters))

	if state.Activity != "" {
		parts = append(parts, theme.StatusWarn.Render("activity:"+state.Activity))
	}

	contextLabel := "ctx:all"
	if state.Context != "" {
		contextLabel = "ctx:" + state.Context
	}
	parts = append(parts, theme.ClusterLabel.Render(contextLabel))

	namespace := "ns:all"
	if state.Namespace != "" {
		namespace = "ns:" + state.Namespace
	}
	parts = append(parts, theme.ClusterLabel.Render(namespace))

	inputLabel := state.InputLabel
	if inputLabel == "" {
		inputLabel = "filter"
	}
	input := inputLabel + ":off"
	if strings.TrimSpace(state.InputValue) != "" {
		input = inputLabel + ":" + state.InputValue
	}
	if state.InputActive {
		input = lipgloss.NewStyle().Underline(true).Render(input)
	}
	parts = append(parts, input)

	parts = append(parts, fmt.Sprintf("rows:%d/%d", state.VisibleRows, state.TotalRows))
	if state.Footer != "" {
		parts = append(parts, theme.Muted.Render(state.Footer))
	}
	return strings.Join(parts, "  ")
}

func renderClusters(clusters []ClusterStatus) string {
	if len(clusters) == 0 {
		return theme.StatusWarn.Render("cluster:disconnected")
	}

	parts := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		label := cluster.Name
		if label == "" {
			label = "unknown"
		}

		suffix := "connecting"
		style := theme.StatusWarn
		switch {
		case cluster.Message != "":
			suffix = cluster.Message
			if cluster.Message == "connecting" {
				style = theme.StatusWarn
			} else {
				style = theme.StatusError
			}
		case cluster.Warning != "":
			suffix = cluster.Warning
			style = theme.StatusWarn
		case cluster.Synced && cluster.Healthy:
			suffix = "ok"
			style = theme.StatusOK
		}

		parts = append(parts, style.Render(label+":"+suffix))
	}
	return strings.Join(parts, " ")
}
