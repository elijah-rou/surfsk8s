package views

import (
	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/components"
)

// NodesView displays nodes with readiness, roles, version, age, cluster.
type NodesView struct {
	columns []components.Column
}

func NewNodesView() NodesView {
	return NodesView{
		columns: []components.Column{
			{Title: "CONTEXT", Width: 16},
			{Title: "NAME", Width: 24},
			{Title: "STATUS", Width: 10},
			{Title: "CPU", Width: 22},
			{Title: "MEMORY", Width: 20},
			{Title: "EPHEMERAL", Width: 20},
			{Title: "GPU", Width: 12},
			{Title: "ROLES", Width: 18},
			{Title: "VERSION", Width: 14},
			{Title: "AGE", Width: 6, AlignRight: true},
		},
	}
}

func (v NodesView) Columns() []components.Column {
	return v.columns
}

func (v NodesView) Rows(rowsIn []state.NodeRow) [][]string {
	rows := make([][]string, 0, len(rowsIn))
	for _, node := range rowsIn {
		rows = append(rows, []string{
			node.Cluster,
			node.Name,
			node.Status,
			"",
			"",
			"",
			"",
			node.Roles,
			node.Version,
			node.Age,
		})
	}
	return rows
}
