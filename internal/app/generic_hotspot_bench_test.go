package app

import (
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

func benchmarkGenericResourceKind() cluster.ResourceKind {
	return cluster.ResourceKind{
		ID:         "serving.knative.dev/services",
		Display:    "Services",
		Resource:   "services",
		APIGroup:   "serving.knative.dev",
		Version:    "v1",
		Kind:       "Service",
		Namespaced: true,
		Custom:     true,
		PrinterColumns: []cluster.PrinterColumn{
			{Name: "URL", JSONPath: ".status.url"},
			{Name: "READY", JSONPath: ".status.conditions[?(@.type==\"Ready\")].status"},
		},
	}
}

func seedBenchmarkGenericObjects(app *App, clusters int, perCluster int) {
	now := time.Now()
	resource := benchmarkGenericResourceKind()
	app.activeResource = resource
	app.genericCompiledColumns = cluster.CompilePrinterColumns(resource.PrinterColumns)
	app.genericCompiledColumnsVersion = resource.ID
	app.genericRows = make([]cluster.GenericResourceRow, 0, clusters*perCluster)
	app.genericRowsByKey = make(map[string]cluster.GenericResourceRow, clusters*perCluster)
	app.genericNamespaceCounts = map[string]int{"default": clusters * perCluster}
	app.sortedGenericRows = make([]cluster.GenericResourceRow, 0, clusters*perCluster)
	for c := 0; c < clusters; c++ {
		clusterName := fmt.Sprintf("cluster-%02d", c)
		for i := 0; i < perCluster; i++ {
			name := fmt.Sprintf("svc-%02d-%05d", c, i)
			obj := &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "serving.knative.dev/v1",
				"kind":       "Service",
				"metadata": map[string]interface{}{
					"name":              name,
					"namespace":         "default",
					"resourceVersion":   fmt.Sprintf("%d", i+1),
					"creationTimestamp": metav1.NewTime(now.Add(-time.Duration(i) * time.Minute)).Format(time.RFC3339),
				},
				"status": map[string]interface{}{
					"url": fmt.Sprintf("https://%s.example.com", name),
					"conditions": []interface{}{
						map[string]interface{}{"type": "Ready", "status": "True", "reason": "Ready"},
					},
				},
			}}
			row := cluster.GenericResourceRow{
				Key:             cluster.GenericResourceKey{Cluster: clusterName, Namespace: "default", Name: name},
				Cluster:         clusterName,
				Namespace:       "default",
				Name:            name,
				Ready:           "True",
				Status:          "Ready",
				Object:          obj,
				ResourceVersion: fmt.Sprintf("%d", i+1),
			}.WithAge(now)
			app.genericRows = append(app.genericRows, row)
			app.genericRowsByKey[row.Key.String()] = row
		}
	}
	app.genericRowsResourceID = resource.ID
	app.genericNamespaces = []string{"default"}
	app.lastGenericFetchAt = now
	total, filtered := app.buildSortedGenericResources()
	app.visibleRows = filtered
	app.totalRows = total
	app.genericListCacheKey = app.genericListCacheState()
}

func setupGenericHotspotApp(b *testing.B) *App {
	b.Helper()
	manager := newTestManagerForBenchmark(b)
	app := New(state.NewStore(), manager, Config{})
	app.width = 220
	app.height = 40
	app.resizeTables()
	app.screen = screenResourceList
	seedBenchmarkGenericObjects(app, 8, 2000)
	return app
}

func BenchmarkBuildSortedGenericResources8ClustersNoFilter(b *testing.B) {
	app := setupGenericHotspotApp(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.buildSortedGenericResources()
	}
}

func BenchmarkBuildSortedGenericResources8ClustersFuzzyQuery(b *testing.B) {
	app := setupGenericHotspotApp(b)
	app.resourceQuery2 = "svc 07 01999"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.buildSortedGenericResources()
	}
}

func BenchmarkBuildSortedGenericResources8ClustersColumnFilter(b *testing.B) {
	app := setupGenericHotspotApp(b)
	app.resourceColumnFilters = map[string][]tableColumnFilter{
		app.activeResource.ID: {{ID: 1, ColumnIndex: 3, ColumnTitle: "URL", Query: "*svc-07-01999*", Enabled: true}},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.buildSortedGenericResources()
	}
}

func BenchmarkGenericRowMatchesScope8ClustersColumnFilter(b *testing.B) {
	app := setupGenericHotspotApp(b)
	app.resourceColumnFilters = map[string][]tableColumnFilter{
		app.activeResource.ID: {{ID: 1, ColumnIndex: 3, ColumnTitle: "URL", Query: "*svc-07-01999*", Enabled: true}},
	}
	row := app.genericRows[0]
	now := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.genericRowMatchesScope(row, now)
	}
}

func BenchmarkGenericPrinterValuesWide(b *testing.B) {
	app := setupGenericHotspotApp(b)
	row := app.genericRows[0]
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.genericPrinterValues(row)
		delete(app.genericPrinterValueCache, genericPrinterCacheKey(row))
	}
}

func BenchmarkGenericTableRowsWindow200(b *testing.B) {
	app := setupGenericHotspotApp(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.genericTableRows(0, 200, time.Now())
	}
}

func BenchmarkViewGenericResourceList8ClustersWarm(b *testing.B) {
	app := setupGenericHotspotApp(b)
	app.resourceTable.SetColumns(app.genericView.Columns(app.activeResource))
	app.resourceTable.SetWindowProvider(app.visibleRows, func(start int, end int) [][]string {
		return app.genericTableRows(start, end-start, time.Now())
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.resourceTable.View()
	}
}
