package app

import (
	"strconv"
	"time"

	"github.com/elijahrou/surfsk8s/internal/state"
)

func fillDeploymentCells(dst []string, row state.DeploymentRow) {
	if len(dst) < 7 {
		panic("app.fillDeploymentCells: short dst")
	}
	dst[0] = row.Cluster
	dst[1] = row.Namespace
	dst[2] = row.Name
	dst[3] = row.Ready
	dst[4] = strconv.Itoa(int(row.UpToDate))
	dst[5] = strconv.Itoa(int(row.Available))
	dst[6] = row.Age
}

func (a *App) deploymentTableRows(rows []state.DeploymentRow, now time.Time) [][]string {
	result := ensureRowCellBuffer(&a.deploymentTableRowBuf, len(rows), 7)
	for i, row := range rows {
		fillDeploymentCells(result[i], row.WithAge(now))
	}
	return result
}

func fillServiceCells(dst []string, row state.ServiceRow) {
	if len(dst) < 7 {
		panic("app.fillServiceCells: short dst")
	}
	dst[0] = row.Cluster
	dst[1] = row.Namespace
	dst[2] = row.Name
	dst[3] = row.Type
	dst[4] = row.ClusterIP
	dst[5] = row.Ports
	dst[6] = row.Age
}

func (a *App) serviceTableRows(rows []state.ServiceRow, now time.Time) [][]string {
	result := ensureRowCellBuffer(&a.serviceTableRowBuf, len(rows), 7)
	for i, row := range rows {
		fillServiceCells(result[i], row.WithAge(now))
	}
	return result
}
