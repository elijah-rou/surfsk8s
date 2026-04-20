package app

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/elijahrou/surfsk8s/internal/ui/components"
)

var writeClipboard = copyToClipboard

type clipboardProgram struct {
	name string
	args []string
}

func copyToClipboard(value string) error {
	program, err := clipboardProgramForPlatform()
	if err != nil {
		return err
	}
	cmd := exec.Command(program.name, program.args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open clipboard stdin: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("start clipboard command: %w", err)
	}
	if _, err := io.WriteString(stdin, value); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return fmt.Errorf("write clipboard data: %w", err)
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Wait()
		return fmt.Errorf("close clipboard stdin: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("clipboard command failed: %w", err)
	}
	return nil
}

func clipboardProgramForPlatform() (clipboardProgram, error) {
	programs := []clipboardProgram{
		{name: "pbcopy"},
		{name: "wl-copy"},
		{name: "xclip", args: []string{"-selection", "clipboard"}},
		{name: "xsel", args: []string{"--clipboard", "--input"}},
	}
	for _, program := range programs {
		if _, err := exec.LookPath(program.name); err == nil {
			return program, nil
		}
	}
	return clipboardProgram{}, fmt.Errorf("no clipboard command found (pbcopy, wl-copy, xclip, xsel)")
}

func (a *App) yankSelectedTable() tea.Cmd {
	value, err := a.selectedRowPipe(time.Now())
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	if err := writeClipboard(value); err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	a.statusMessage = "row copied"
	return nil
}

func (a *App) yankFullTableRow() tea.Cmd {
	value, err := a.filteredTablePipe(time.Now())
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	if err := writeClipboard(value); err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	a.statusMessage = "table copied"
	return nil
}

func (a *App) selectedRowPipe(now time.Time) (string, error) {
	switch a.screen {
	case screenPods:
		index := a.podTable.SelectedIndex()
		if index < 0 {
			return "", fmt.Errorf("no pod selected")
		}
		return pipeTableFromCells(a.podTableRows(a.podWindow(index, 1, now), now))
	case screenResourceList:
		index := a.resourceTable.SelectedIndex()
		if index < 0 {
			return "", fmt.Errorf("no resource selected")
		}
		return pipeTableFromCells(a.selectedResourceCells(index, 1, now))
	default:
		return "", fmt.Errorf("clipboard yank unavailable on this screen")
	}
}

func (a *App) filteredTablePipe(now time.Time) (string, error) {
	switch a.screen {
	case screenPods:
		rows := a.podTableRows(a.podWindow(0, a.visibleRows, now), now)
		return csvTableWithHeader(a.podsView.Columns(), rows)
	case screenResourceList:
		rows := a.selectedResourceCells(0, a.visibleRows, now)
		return csvTableWithHeader(a.currentResourceColumns(), rows)
	default:
		return "", fmt.Errorf("clipboard yank unavailable on this screen")
	}
}

func csvTableWithHeader(columns []components.Column, rows [][]string) (string, error) {
	if len(columns) == 0 {
		return "", fmt.Errorf("selection disappeared")
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	header := make([]string, len(columns))
	for i, column := range columns {
		header[i] = strings.TrimSpace(column.Title)
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("write csv header: %w", err)
	}
	for _, row := range rows {
		record := make([]string, len(columns))
		for i := range columns {
			if i < len(row) {
				record[i] = strings.TrimSpace(ansi.Strip(row[i]))
			}
		}
		if err := w.Write(record); err != nil {
			return "", fmt.Errorf("write csv row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func pipeTableFromCells(rows [][]string) (string, error) {
	if len(rows) == 0 || len(rows[0]) == 0 {
		return "", fmt.Errorf("selection disappeared")
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, formatPipeTableRow(row))
	}
	return strings.Join(lines, "\n"), nil
}

func formatPipeTableRow(cells []string) string {
	parts := make([]string, len(cells))
	for idx, cell := range cells {
		parts[idx] = strings.TrimSpace(ansi.Strip(cell))
	}
	return "| " + strings.Join(parts, " | ") + " |"
}

func (a *App) currentResourceColumns() []components.Column {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		return a.deploymentsView.Columns()
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		return a.servicesView.Columns()
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		return a.nodesView.Columns()
	default:
		return a.genericView.Columns(a.activeResource)
	}
}

func (a *App) selectedResourceCells(index int, count int, now time.Time) [][]string {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		return a.deploymentTableRows(a.deploymentWindow(index, count, now), now)
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		return a.serviceTableRows(a.serviceWindow(index, count, now), now)
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		return a.nodeTableRows(a.nodeWindow(index, count, now), now)
	default:
		return a.genericTableRows(index, count, now)
	}
}
