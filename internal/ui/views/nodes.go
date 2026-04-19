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
			{Title: "NAME", Width: 28},
			{Title: "STATUS", Width: 10},
			{Title: "ROLES", Width: 20},
			{Title: "VERSION", Width: 14},
			{Title: "AGE", Width: 6, AlignRight: true},
			{Title: "CLUSTER", Width: 16},
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
			node.Name,
			node.Status,
			node.Roles,
			node.Version,
			node.Age,
			node.Cluster,
		})
	}
	return rows
}
