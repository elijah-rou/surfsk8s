package views

import (
	"strconv"

	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/components"
)

// DeploymentsView displays deployments with replicas, strategy, age, cluster.
type DeploymentsView struct {
	columns []components.Column
}

func NewDeploymentsView() DeploymentsView {
	return DeploymentsView{
		columns: []components.Column{
			{Title: "CONTEXT", Width: 16},
			{Title: "NAMESPACE", Width: 16},
			{Title: "NAME", Width: 32},
			{Title: "READY", Width: 7},
			{Title: "UPDATED", Width: 8, AlignRight: true},
			{Title: "AVAILABLE", Width: 10, AlignRight: true},
			{Title: "AGE", Width: 6, AlignRight: true},
		},
	}
}

func (v DeploymentsView) Columns() []components.Column {
	return v.columns
}

func (v DeploymentsView) Rows(rowsIn []state.DeploymentRow) [][]string {
	rows := make([][]string, 0, len(rowsIn))
	for _, deployment := range rowsIn {
		rows = append(rows, []string{
			deployment.Cluster,
			deployment.Namespace,
			deployment.Name,
			deployment.Ready,
			strconv.Itoa(int(deployment.UpToDate)),
			strconv.Itoa(int(deployment.Available)),
			deployment.Age,
		})
	}
	return rows
}
