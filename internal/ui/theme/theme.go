package theme

import "github.com/charmbracelet/lipgloss"

var (
	HeaderStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	SelectedRow  = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	StatusOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	StatusError  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	StatusWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	ClusterLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	Muted        = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)
