package app

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

type sortKey uint8

const (
	sortKeyDefault sortKey = iota
	sortKeyName
	sortKeyNamespace
	sortKeyCluster
	sortKeyStatus
	sortKeyAge
	sortKeyType
	sortKeyAvailable
)

type listSortState struct {
	key     sortKey
	reverse bool
}

func (s listSortState) Label() string {
	label := "default"
	switch s.key {
	case sortKeyName:
		label = "name"
	case sortKeyNamespace:
		label = "namespace"
	case sortKeyCluster:
		label = "cluster"
	case sortKeyStatus:
		label = "status"
	case sortKeyAge:
		label = "age"
	case sortKeyType:
		label = "type"
	case sortKeyAvailable:
		label = "available"
	}
	if s.reverse {
		return label + "↓"
	}
	return label + "↑"
}

func (a *App) podNeedsMaterializedSort() bool {
	return a.screen == screenPods && a.tableSortNeedsMaterializedSort()
}

func (a *App) deploymentNeedsMaterializedSort() bool {
	return a.screen == screenResourceList && a.tableSortNeedsMaterializedSort()
}

func (a *App) serviceNeedsMaterializedSort() bool {
	return a.screen == screenResourceList && a.tableSortNeedsMaterializedSort()
}

func (a *App) nodeNeedsMaterializedSort() bool {
	return a.screen == screenResourceList && a.tableSortNeedsMaterializedSort()
}

func (a *App) genericNeedsMaterializedSort() bool {
	return a.screen == screenResourceList && a.tableSortNeedsMaterializedSort()
}

func (a *App) buildSortedPods() (int, int) {
	a.sortedPods = a.sortedPods[:0]
	total := 0
	now := time.Now()
	a.store.ForEachPod(func(row state.PodRow) bool {
		if !a.contextMatches(row.Cluster) {
			return true
		}
		total++
		if !a.matchPodRowAt(row, now) {
			return true
		}
		a.sortedPods = append(a.sortedPods, row)
		return true
	})
	sorts := a.enabledTableSorts()
	sort.SliceStable(a.sortedPods, func(i int, j int) bool {
		return comparePodRows(a.sortedPods[i], a.sortedPods[j], sorts)
	})
	return total, len(a.sortedPods)
}

func (a *App) buildSortedDeployments() (int, int) {
	a.sortedDeployments = a.sortedDeployments[:0]
	total := 0
	now := time.Now()
	a.store.ForEachDeployment(func(row state.DeploymentRow) bool {
		if !a.contextMatches(row.Cluster) {
			return true
		}
		total++
		if a.namespace != "" && row.Namespace != a.namespace {
			return true
		}
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		if !a.matchesDeploymentColumnFilters(row, now) {
			return true
		}
		a.sortedDeployments = append(a.sortedDeployments, row)
		return true
	})
	sorts := a.enabledTableSorts()
	sort.SliceStable(a.sortedDeployments, func(i int, j int) bool {
		return compareDeploymentRows(a.sortedDeployments[i], a.sortedDeployments[j], sorts)
	})
	return total, len(a.sortedDeployments)
}

func (a *App) buildSortedServices() (int, int) {
	a.sortedServices = a.sortedServices[:0]
	total := 0
	now := time.Now()
	a.store.ForEachService(func(row state.ServiceRow) bool {
		if !a.contextMatches(row.Cluster) {
			return true
		}
		total++
		if a.namespace != "" && row.Namespace != a.namespace {
			return true
		}
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		if !a.matchesServiceColumnFilters(row, now) {
			return true
		}
		a.sortedServices = append(a.sortedServices, row)
		return true
	})
	sorts := a.enabledTableSorts()
	sort.SliceStable(a.sortedServices, func(i int, j int) bool {
		return compareServiceRows(a.sortedServices[i], a.sortedServices[j], sorts)
	})
	return total, len(a.sortedServices)
}

func (a *App) buildSortedNodes() (int, int) {
	a.sortedNodes = a.sortedNodes[:0]
	total := 0
	now := time.Now()
	a.store.ForEachNode(func(row state.NodeRow) bool {
		if !a.contextMatches(row.Cluster) {
			return true
		}
		total++
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		if !a.matchesNodeColumnFilters(row, now) {
			return true
		}
		a.sortedNodes = append(a.sortedNodes, row)
		return true
	})
	sorts := a.enabledTableSorts()
	sort.SliceStable(a.sortedNodes, func(i int, j int) bool {
		return compareNodeRows(a.sortedNodes[i], a.sortedNodes[j], sorts)
	})
	return total, len(a.sortedNodes)
}

func comparePodRows(left state.PodRow, right state.PodRow, sorts []tableSortCriterion) bool {
	return comparePodRowStack(left, right, sorts) < 0
}

func compareDeploymentRows(left state.DeploymentRow, right state.DeploymentRow, sorts []tableSortCriterion) bool {
	return compareDeploymentRowStack(left, right, sorts) < 0
}

func compareServiceRows(left state.ServiceRow, right state.ServiceRow, sorts []tableSortCriterion) bool {
	return compareServiceRowStack(left, right, sorts) < 0
}

func compareNodeRows(left state.NodeRow, right state.NodeRow, sorts []tableSortCriterion) bool {
	return compareNodeRowStack(left, right, sorts) < 0
}

func (a *App) buildSortedGenericResources() (int, int) {
	a.sortedGenericRows = a.sortedGenericRows[:0]
	query := strings.TrimSpace(a.resourceQuery2)
	now := time.Now()
	for _, row := range a.genericRows {
		if !a.contextMatches(row.Cluster) {
			continue
		}
		if a.activeResource.Namespaced && a.namespace != "" && row.Namespace != a.namespace {
			continue
		}
		if query != "" {
			if len(a.genericCompiledColumns) != 0 && row.Object != nil && len(row.PrinterValues) == 0 {
				row.PrinterValues = cluster.EvaluateCompiledPrinterColumns(row.Object, a.genericCompiledColumns)
			}
			if !matchesSearch(row.SearchText(), query) {
				continue
			}
		}
		if !a.matchesGenericColumnFilters(row, now) {
			continue
		}
		a.sortedGenericRows = append(a.sortedGenericRows, row)
	}
	sorts := a.enabledTableSorts()
	sort.SliceStable(a.sortedGenericRows, func(i int, j int) bool {
		return a.compareGenericRows(a.sortedGenericRows[i], a.sortedGenericRows[j], sorts) < 0
	})
	return len(a.genericRows), len(a.sortedGenericRows)
}

func comparePodRowStack(left state.PodRow, right state.PodRow, sorts []tableSortCriterion) int {
	for _, criterion := range sorts {
		cmp := comparePodRowCriterion(left, right, criterion)
		if criterion.Desc {
			cmp = -cmp
		}
		if cmp != 0 {
			return cmp
		}
	}
	if cmp := cmpStringCI(left.Namespace, right.Namespace); cmp != 0 {
		return cmp
	}
	if cmp := cmpStringCI(left.Name, right.Name); cmp != 0 {
		return cmp
	}
	return cmpStringCI(left.Cluster, right.Cluster)
}

func compareDeploymentRowStack(left state.DeploymentRow, right state.DeploymentRow, sorts []tableSortCriterion) int {
	for _, criterion := range sorts {
		cmp := compareDeploymentRowCriterion(left, right, criterion)
		if criterion.Desc {
			cmp = -cmp
		}
		if cmp != 0 {
			return cmp
		}
	}
	if cmp := cmpStringCI(left.Namespace, right.Namespace); cmp != 0 {
		return cmp
	}
	if cmp := cmpStringCI(left.Name, right.Name); cmp != 0 {
		return cmp
	}
	return cmpStringCI(left.Cluster, right.Cluster)
}

func compareServiceRowStack(left state.ServiceRow, right state.ServiceRow, sorts []tableSortCriterion) int {
	for _, criterion := range sorts {
		cmp := compareServiceRowCriterion(left, right, criterion)
		if criterion.Desc {
			cmp = -cmp
		}
		if cmp != 0 {
			return cmp
		}
	}
	if cmp := cmpStringCI(left.Namespace, right.Namespace); cmp != 0 {
		return cmp
	}
	if cmp := cmpStringCI(left.Name, right.Name); cmp != 0 {
		return cmp
	}
	return cmpStringCI(left.Cluster, right.Cluster)
}

func compareNodeRowStack(left state.NodeRow, right state.NodeRow, sorts []tableSortCriterion) int {
	for _, criterion := range sorts {
		cmp := compareNodeRowCriterion(left, right, criterion)
		if criterion.Desc {
			cmp = -cmp
		}
		if cmp != 0 {
			return cmp
		}
	}
	if cmp := cmpStringCI(left.Name, right.Name); cmp != 0 {
		return cmp
	}
	return cmpStringCI(left.Cluster, right.Cluster)
}

func comparePodRowCriterion(left state.PodRow, right state.PodRow, criterion tableSortCriterion) int {
	switch criterion.ColumnIndex {
	case 0:
		return cmpStringCI(left.Cluster, right.Cluster)
	case 1:
		return cmpStringCI(left.Namespace, right.Namespace)
	case 2:
		return cmpStringCI(left.Name, right.Name)
	case 3:
		return compareReadyFraction(left.Ready, right.Ready)
	case 4:
		return cmpStringCI(left.Status, right.Status)
	case 5:
		return cmpInt(left.Restarts, right.Restarts)
	case 6:
		return cmpTimeDesc(left.CreatedAt(), right.CreatedAt())
	case 7:
		return cmpStringCI(left.Node, right.Node)
	default:
		return 0
	}
}

func compareDeploymentRowCriterion(left state.DeploymentRow, right state.DeploymentRow, criterion tableSortCriterion) int {
	switch criterion.ColumnIndex {
	case 0:
		return cmpStringCI(left.Cluster, right.Cluster)
	case 1:
		return cmpStringCI(left.Namespace, right.Namespace)
	case 2:
		return cmpStringCI(left.Name, right.Name)
	case 3:
		return compareReadyFraction(left.Ready, right.Ready)
	case 4:
		return cmpInt(int(left.UpToDate), int(right.UpToDate))
	case 5:
		return cmpInt(int(left.Available), int(right.Available))
	case 6:
		return cmpTimeDesc(left.CreatedAt(), right.CreatedAt())
	default:
		return 0
	}
}

func compareServiceRowCriterion(left state.ServiceRow, right state.ServiceRow, criterion tableSortCriterion) int {
	switch criterion.ColumnIndex {
	case 0:
		return cmpStringCI(left.Cluster, right.Cluster)
	case 1:
		return cmpStringCI(left.Namespace, right.Namespace)
	case 2:
		return cmpStringCI(left.Name, right.Name)
	case 3:
		return cmpStringCI(left.Type, right.Type)
	case 4:
		return cmpStringCI(left.ClusterIP, right.ClusterIP)
	case 5:
		return cmpStringCI(left.Ports, right.Ports)
	case 6:
		return cmpTimeDesc(left.CreatedAt(), right.CreatedAt())
	default:
		return 0
	}
}

func compareNodeRowCriterion(left state.NodeRow, right state.NodeRow, criterion tableSortCriterion) int {
	switch criterion.ColumnIndex {
	case 0:
		return cmpStringCI(left.Cluster, right.Cluster)
	case 1:
		return cmpStringCI(left.Name, right.Name)
	case 2:
		return cmpStringCI(left.Status, right.Status)
	case 3:
		return cmpStringCI(left.Roles, right.Roles)
	case 4:
		return cmpStringCI(left.Version, right.Version)
	case 5:
		return cmpTimeDesc(left.CreatedAt(), right.CreatedAt())
	default:
		return 0
	}
}

func (a *App) compareGenericRows(left cluster.GenericResourceRow, right cluster.GenericResourceRow, sorts []tableSortCriterion) int {
	for _, criterion := range sorts {
		cmp := a.compareGenericRowCriterion(left, right, criterion)
		if criterion.Desc {
			cmp = -cmp
		}
		if cmp != 0 {
			return cmp
		}
	}
	if a.activeResource.Namespaced {
		if cmp := cmpStringCI(left.Namespace, right.Namespace); cmp != 0 {
			return cmp
		}
	}
	if cmp := cmpStringCI(left.Name, right.Name); cmp != 0 {
		return cmp
	}
	return cmpStringCI(left.Cluster, right.Cluster)
}

func (a *App) compareGenericRowCriterion(left cluster.GenericResourceRow, right cluster.GenericResourceRow, criterion tableSortCriterion) int {
	columns := a.currentResourceColumns()
	if criterion.ColumnIndex < 0 || criterion.ColumnIndex >= len(columns) {
		return 0
	}
	title := columns[criterion.ColumnIndex].Title
	leftValue := a.genericCellValueAt(left, criterion.ColumnIndex)
	rightValue := a.genericCellValueAt(right, criterion.ColumnIndex)
	return compareColumnValue(title, leftValue, rightValue, left.CreatedAt(), right.CreatedAt())
}

func (a *App) genericCellValueAt(row cluster.GenericResourceRow, columnIndex int) string {
	idx := columnIndex
	if idx == 0 {
		return row.Cluster
	}
	idx--
	if a.activeResource.Namespaced {
		if idx == 0 {
			return row.Namespace
		}
		idx--
	}
	if idx == 0 {
		return row.Name
	}
	idx--
	if len(a.activeResource.PrinterColumns) != 0 {
		printerValues := row.PrinterValues
		if len(printerValues) == 0 && row.Object != nil {
			a.ensureGenericCompiledColumns()
			printerValues = cluster.EvaluateCompiledPrinterColumns(row.Object, a.genericCompiledColumns)
		}
		if idx >= 0 && idx < len(printerValues) {
			return printerValues[idx]
		}
		if idx == len(printerValues) {
			return row.Age
		}
		return ""
	}
	if idx == 0 {
		return row.Ready
	}
	if idx == 1 {
		return row.Status
	}
	if idx == 2 {
		return row.Age
	}
	return ""
}

func compareColumnValue(title string, left string, right string, leftCreated time.Time, rightCreated time.Time) int {
	normalized := normalizeSortTitle(title)
	switch {
	case normalized == "AGE":
		return cmpTimeDesc(leftCreated, rightCreated)
	case normalized == "READY":
		return compareReadyFraction(left, right)
	case normalized == "RESTARTS", normalized == "UPDATED", normalized == "AVAILABLE":
		leftInt, leftOK := parseInt(left)
		rightInt, rightOK := parseInt(right)
		if leftOK && rightOK {
			return cmpInt(leftInt, rightInt)
		}
	}
	return cmpStringCI(left, right)
}

func normalizeSortTitle(title string) string {
	title = strings.TrimSpace(strings.ToUpper(title))
	title = strings.ReplaceAll(title, "-", "")
	title = strings.ReplaceAll(title, "_", "")
	title = strings.ReplaceAll(title, " ", "")
	return title
}

func compareReadyFraction(left string, right string) int {
	leftNum, leftDen, leftOK := parseFraction(left)
	rightNum, rightDen, rightOK := parseFraction(right)
	if leftOK && rightOK {
		leftScore := leftNum * max(1, rightDen)
		rightScore := rightNum * max(1, leftDen)
		if cmp := cmpInt(leftScore, rightScore); cmp != 0 {
			return cmp
		}
		return cmpInt(leftDen, rightDen)
	}
	return cmpStringCI(left, right)
}

func parseFraction(value string) (int, int, bool) {
	parts := strings.SplitN(strings.TrimSpace(value), "/", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	left, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, false
	}
	right, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, false
	}
	return left, right, true
}

func parseInt(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func cmpStringCI(left string, right string) int {
	left = strings.ToLower(left)
	right = strings.ToLower(right)
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func cmpInt(left int, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func cmpTimeDesc(left time.Time, right time.Time) int {
	switch {
	case left.After(right):
		return -1
	case left.Before(right):
		return 1
	default:
		return 0
	}
}

func windowRowsFromSlice[T interface{ WithAge(time.Time) T }](rows []T, start int, limit int, now time.Time) []T {
	if limit <= 0 || start >= len(rows) {
		return nil
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}
	window := make([]T, 0, end-start)
	for _, row := range rows[start:end] {
		window = append(window, row.WithAge(now))
	}
	return window
}
