package views

import (
	"strconv"

	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/components"
)

// PodsView displays pod resources with status, restarts, age, node, cluster.
type PodsView struct {
	columns []components.Column
}

func NewPodsView() PodsView {
	return PodsView{
		columns: []components.Column{
			{Title: "CONTEXT", Width: 16},
			{Title: "NAMESPACE", Width: 14},
			{Title: "NAME", Width: 34},
			{Title: "READY", Width: 7},
			{Title: "STATUS", Width: 14},
			{Title: "RESTARTS", Width: 8, AlignRight: true},
			{Title: "AGE", Width: 6, AlignRight: true},
			{Title: "NODE", Width: 18},
		},
	}
}

func (v PodsView) Columns() []components.Column {
	return v.columns
}

func (v PodsView) Rows(rowsIn []state.PodRow) [][]string {
	rows := make([][]string, 0, len(rowsIn))
	for _, pod := range rowsIn {
		rows = append(rows, []string{
			pod.Cluster,
			pod.Namespace,
			pod.Name,
			pod.Ready,
			pod.Status,
			strconv.Itoa(pod.Restarts),
			pod.Age,
			pod.Node,
		})
	}
	return rows
}
