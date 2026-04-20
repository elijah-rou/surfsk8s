package views

import (
	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/ui/components"
)

type GenericResourcesView struct{}

func NewGenericResourcesView() GenericResourcesView {
	return GenericResourcesView{}
}

func (v GenericResourcesView) Columns(resource cluster.ResourceKind) []components.Column {
	columns := make([]components.Column, 0, 3+len(resource.PrinterColumns)+1)
	columns = append(columns, components.Column{Title: "CONTEXT", Width: 16})
	if resource.Namespaced {
		columns = append(columns, components.Column{Title: "NAMESPACE", Width: 18})
	}
	columns = append(columns, components.Column{Title: "NAME", Width: 30})
	if len(resource.PrinterColumns) != 0 {
		for _, column := range resource.PrinterColumns {
			columns = append(columns, components.Column{Title: column.Name, Width: printerColumnWidth(column)})
		}
	} else {
		columns = append(columns,
			components.Column{Title: "READY", Width: 10},
			components.Column{Title: "STATUS", Width: 24},
		)
	}
	columns = append(columns, components.Column{Title: "AGE", Width: 6, AlignRight: true})
	return columns
}

func (v GenericResourcesView) Rows(rows []cluster.GenericResourceRow, resource cluster.ResourceKind) [][]string {
	result := make([][]string, 0, len(rows))
	for _, row := range rows {
		values := make([]string, 0, 3+len(row.PrinterValues)+1)
		values = append(values, row.Cluster)
		if resource.Namespaced {
			values = append(values, row.Namespace)
		}
		values = append(values, row.Name)
		if len(resource.PrinterColumns) != 0 {
			values = append(values, row.PrinterValues...)
		} else {
			values = append(values, row.Ready, row.Status)
		}
		values = append(values, row.Age)
		result = append(result, values)
	}
	return result
}

func printerColumnWidth(column cluster.PrinterColumn) int {
	width := len(column.Name) + 2
	if column.Type == "boolean" {
		width = max(width, 8)
	}
	if column.Type == "date" {
		width = max(width, 10)
	}
	if width < 12 {
		width = 12
	}
	if width > 24 {
		width = 24
	}
	return width
}
