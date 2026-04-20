package app

import (
	"sort"
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

func (a *App) cyclePodSort() {
	a.podSort.key = nextSortKey(a.podSort.key, []sortKey{sortKeyDefault, sortKeyName, sortKeyNamespace, sortKeyStatus, sortKeyAge, sortKeyCluster})
}

func (a *App) togglePodSortReverse() {
	a.podSort.reverse = !a.podSort.reverse
}

func (a *App) cycleResourceSort() {
	state := a.currentResourceSortPointer()
	keys := []sortKey{sortKeyDefault, sortKeyName, sortKeyAge, sortKeyCluster}
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		keys = []sortKey{sortKeyDefault, sortKeyName, sortKeyNamespace, sortKeyAvailable, sortKeyAge, sortKeyCluster}
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		keys = []sortKey{sortKeyDefault, sortKeyName, sortKeyNamespace, sortKeyType, sortKeyAge, sortKeyCluster}
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		keys = []sortKey{sortKeyDefault, sortKeyName, sortKeyStatus, sortKeyAge, sortKeyCluster}
	default:
		keys = []sortKey{sortKeyDefault, sortKeyName, sortKeyNamespace, sortKeyStatus, sortKeyAge, sortKeyCluster}
		if !a.activeResource.Namespaced {
			keys = []sortKey{sortKeyDefault, sortKeyName, sortKeyStatus, sortKeyAge, sortKeyCluster}
		}
	}
	state.key = nextSortKey(state.key, keys)
}

func (a *App) toggleResourceSortReverse() {
	a.currentResourceSortPointer().reverse = !a.currentResourceSortPointer().reverse
}

func (a *App) currentResourceSort() listSortState {
	return *a.currentResourceSortPointer()
}

func (a *App) currentResourceSortPointer() *listSortState {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		return &a.deploymentSort
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		return &a.serviceSort
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		return &a.nodeSort
	default:
		return &a.genericSort
	}
}

func nextSortKey(current sortKey, keys []sortKey) sortKey {
	if len(keys) == 0 {
		panic("app.nextSortKey: empty keys")
	}
	for idx, key := range keys {
		if key == current {
			return keys[(idx+1)%len(keys)]
		}
	}
	return keys[0]
}

func (a *App) podNeedsMaterializedSort() bool {
	return a.podSort.key != sortKeyDefault || a.podSort.reverse
}

func (a *App) deploymentNeedsMaterializedSort() bool {
	return a.deploymentSort.key != sortKeyDefault || a.deploymentSort.reverse
}

func (a *App) serviceNeedsMaterializedSort() bool {
	return a.serviceSort.key != sortKeyDefault || a.serviceSort.reverse
}

func (a *App) nodeNeedsMaterializedSort() bool {
	return a.nodeSort.key != sortKeyDefault || a.nodeSort.reverse
}

func (a *App) genericNeedsMaterializedSort() bool {
	return a.genericSort.key != sortKeyDefault || a.genericSort.reverse
}

func (a *App) buildSortedPods() (int, int) {
	a.sortedPods = a.sortedPods[:0]
	total := 0
	a.store.ForEachPod(func(row state.PodRow) bool {
		if !a.contextMatches(row.Cluster) {
			return true
		}
		total++
		if !a.matchPodRow(row) {
			return true
		}
		a.sortedPods = append(a.sortedPods, row)
		return true
	})
	sort.SliceStable(a.sortedPods, func(i int, j int) bool {
		return comparePodRows(a.sortedPods[i], a.sortedPods[j], a.podSort)
	})
	return total, len(a.sortedPods)
}

func (a *App) buildSortedDeployments() (int, int) {
	a.sortedDeployments = a.sortedDeployments[:0]
	total := 0
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
		a.sortedDeployments = append(a.sortedDeployments, row)
		return true
	})
	sort.SliceStable(a.sortedDeployments, func(i int, j int) bool {
		return compareDeploymentRows(a.sortedDeployments[i], a.sortedDeployments[j], a.deploymentSort)
	})
	return total, len(a.sortedDeployments)
}

func (a *App) buildSortedServices() (int, int) {
	a.sortedServices = a.sortedServices[:0]
	total := 0
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
		a.sortedServices = append(a.sortedServices, row)
		return true
	})
	sort.SliceStable(a.sortedServices, func(i int, j int) bool {
		return compareServiceRows(a.sortedServices[i], a.sortedServices[j], a.serviceSort)
	})
	return total, len(a.sortedServices)
}

func (a *App) buildSortedNodes() (int, int) {
	a.sortedNodes = a.sortedNodes[:0]
	total := 0
	a.store.ForEachNode(func(row state.NodeRow) bool {
		if !a.contextMatches(row.Cluster) {
			return true
		}
		total++
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		a.sortedNodes = append(a.sortedNodes, row)
		return true
	})
	sort.SliceStable(a.sortedNodes, func(i int, j int) bool {
		return compareNodeRows(a.sortedNodes[i], a.sortedNodes[j], a.nodeSort)
	})
	return total, len(a.sortedNodes)
}

func comparePodRows(left state.PodRow, right state.PodRow, state listSortState) bool {
	cmp := comparePodRowValue(left, right, state.key)
	if cmp == 0 {
		cmp = comparePodRowValue(left, right, sortKeyDefault)
	}
	return sortComparison(cmp, state.reverse)
}

func compareDeploymentRows(left state.DeploymentRow, right state.DeploymentRow, state listSortState) bool {
	cmp := compareDeploymentRowValue(left, right, state.key)
	if cmp == 0 {
		cmp = compareDeploymentRowValue(left, right, sortKeyDefault)
	}
	return sortComparison(cmp, state.reverse)
}

func compareServiceRows(left state.ServiceRow, right state.ServiceRow, state listSortState) bool {
	cmp := compareServiceRowValue(left, right, state.key)
	if cmp == 0 {
		cmp = compareServiceRowValue(left, right, sortKeyDefault)
	}
	return sortComparison(cmp, state.reverse)
}

func compareNodeRows(left state.NodeRow, right state.NodeRow, state listSortState) bool {
	cmp := compareNodeRowValue(left, right, state.key)
	if cmp == 0 {
		cmp = compareNodeRowValue(left, right, sortKeyDefault)
	}
	return sortComparison(cmp, state.reverse)
}

func (a *App) buildSortedGenericResources() (int, int) {
	a.sortedGenericRows = a.sortedGenericRows[:0]
	query := strings.TrimSpace(a.resourceQuery2)
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
		a.sortedGenericRows = append(a.sortedGenericRows, row)
	}
	sort.SliceStable(a.sortedGenericRows, func(i int, j int) bool {
		return compareGenericRows(a.sortedGenericRows[i], a.sortedGenericRows[j], a.genericSort)
	})
	return len(a.genericRows), len(a.sortedGenericRows)
}

func compareGenericRows(left cluster.GenericResourceRow, right cluster.GenericResourceRow, state listSortState) bool {
	cmp := compareGenericRowValue(left, right, state.key)
	if cmp == 0 {
		cmp = compareGenericRowValue(left, right, sortKeyDefault)
	}
	return sortComparison(cmp, state.reverse)
}

func comparePodRowValue(left state.PodRow, right state.PodRow, key sortKey) int {
	switch key {
	case sortKeyName:
		return cmpString3(left.Name, right.Name)
	case sortKeyNamespace:
		return cmpString3(left.Namespace, right.Namespace)
	case sortKeyStatus:
		return cmpString3(left.Status, right.Status)
	case sortKeyAge:
		return cmpTimeDesc(left.CreatedAt(), right.CreatedAt())
	case sortKeyCluster:
		return cmpString3(left.Cluster, right.Cluster)
	default:
		if cmp := cmpString3(left.Namespace, right.Namespace); cmp != 0 {
			return cmp
		}
		if cmp := cmpString3(left.Name, right.Name); cmp != 0 {
			return cmp
		}
		return cmpString3(left.Cluster, right.Cluster)
	}
}

func compareDeploymentRowValue(left state.DeploymentRow, right state.DeploymentRow, key sortKey) int {
	switch key {
	case sortKeyName:
		return cmpString3(left.Name, right.Name)
	case sortKeyNamespace:
		return cmpString3(left.Namespace, right.Namespace)
	case sortKeyAvailable:
		return cmpInt32(left.Available, right.Available)
	case sortKeyAge:
		return cmpTimeDesc(left.CreatedAt(), right.CreatedAt())
	case sortKeyCluster:
		return cmpString3(left.Cluster, right.Cluster)
	default:
		if cmp := cmpString3(left.Namespace, right.Namespace); cmp != 0 {
			return cmp
		}
		if cmp := cmpString3(left.Name, right.Name); cmp != 0 {
			return cmp
		}
		return cmpString3(left.Cluster, right.Cluster)
	}
}

func compareServiceRowValue(left state.ServiceRow, right state.ServiceRow, key sortKey) int {
	switch key {
	case sortKeyName:
		return cmpString3(left.Name, right.Name)
	case sortKeyNamespace:
		return cmpString3(left.Namespace, right.Namespace)
	case sortKeyType:
		return cmpString3(left.Type, right.Type)
	case sortKeyAge:
		return cmpTimeDesc(left.CreatedAt(), right.CreatedAt())
	case sortKeyCluster:
		return cmpString3(left.Cluster, right.Cluster)
	default:
		if cmp := cmpString3(left.Namespace, right.Namespace); cmp != 0 {
			return cmp
		}
		if cmp := cmpString3(left.Name, right.Name); cmp != 0 {
			return cmp
		}
		return cmpString3(left.Cluster, right.Cluster)
	}
}

func compareNodeRowValue(left state.NodeRow, right state.NodeRow, key sortKey) int {
	switch key {
	case sortKeyName:
		return cmpString3(left.Name, right.Name)
	case sortKeyStatus:
		return cmpString3(left.Status, right.Status)
	case sortKeyAge:
		return cmpTimeDesc(left.CreatedAt(), right.CreatedAt())
	case sortKeyCluster:
		return cmpString3(left.Cluster, right.Cluster)
	default:
		if cmp := cmpString3(left.Name, right.Name); cmp != 0 {
			return cmp
		}
		return cmpString3(left.Cluster, right.Cluster)
	}
}

func compareGenericRowValue(left cluster.GenericResourceRow, right cluster.GenericResourceRow, key sortKey) int {
	switch key {
	case sortKeyName:
		return cmpString3(left.Name, right.Name)
	case sortKeyNamespace:
		return cmpString3(left.Namespace, right.Namespace)
	case sortKeyStatus:
		return cmpString3(left.Status, right.Status)
	case sortKeyAge:
		return cmpTimeDesc(left.CreatedAt(), right.CreatedAt())
	case sortKeyCluster:
		return cmpString3(left.Cluster, right.Cluster)
	default:
		if cmp := cmpString3(left.Namespace, right.Namespace); cmp != 0 {
			return cmp
		}
		if cmp := cmpString3(left.Name, right.Name); cmp != 0 {
			return cmp
		}
		return cmpString3(left.Cluster, right.Cluster)
	}
}

func sortComparison(cmp int, reverse bool) bool {
	if reverse {
		return cmp > 0
	}
	return cmp < 0
}

func cmpString3(left string, right string) int {
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

func cmpInt32(left int32, right int32) int {
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
