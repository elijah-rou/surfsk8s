package views

import (
	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/components"
)

// ServicesView displays services with type, cluster IP, ports, cluster.
type ServicesView struct {
	columns []components.Column
}

func NewServicesView() ServicesView {
	return ServicesView{
		columns: []components.Column{
			{Title: "CONTEXT", Width: 16},
			{Title: "NAMESPACE", Width: 16},
			{Title: "NAME", Width: 26},
			{Title: "TYPE", Width: 12},
			{Title: "CLUSTER-IP", Width: 16},
			{Title: "PORTS", Width: 20},
			{Title: "AGE", Width: 6, AlignRight: true},
		},
	}
}

func (v ServicesView) Columns() []components.Column {
	return v.columns
}

func (v ServicesView) Rows(rowsIn []state.ServiceRow) [][]string {
	rows := make([][]string, 0, len(rowsIn))
	for _, service := range rowsIn {
		rows = append(rows, []string{
			service.Cluster,
			service.Namespace,
			service.Name,
			service.Type,
			service.ClusterIP,
			service.Ports,
			service.Age,
		})
	}
	return rows
}
