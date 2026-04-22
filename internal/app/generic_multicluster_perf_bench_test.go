package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

func seedBenchmarkGenericMultiClusterRows(app *App, clusters int, perCluster int, resource cluster.ResourceKind) {
	now := time.Now()
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
			obj := cluster.GenericResourceRow{
				Key:             cluster.GenericResourceKey{Cluster: clusterName, Namespace: "default", Name: fmt.Sprintf("svc-%02d-%05d", c, i)},
				Cluster:         clusterName,
				Namespace:       "default",
				Name:            fmt.Sprintf("svc-%02d-%05d", c, i),
				Ready:           "True",
				Status:          "Active",
				ResourceVersion: fmt.Sprintf("%d", i+1),
			}.WithAge(now)
			app.genericRows = append(app.genericRows, obj)
			app.genericRowsByKey[obj.Key.String()] = obj
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

func benchmarkGenericResourceUnderManagerChurn(b *testing.B, detail bool) {
	manager := newTestManagerForBenchmark(b)
	app := New(state.NewStore(), manager, Config{})
	resource := cluster.ResourceKind{ID: "serving.knative.dev/services", Display: "Services", Resource: "services", APIGroup: "serving.knative.dev", Version: "v1", Kind: "Service", Namespaced: true, Custom: true}
	app.width = 220
	app.height = 40
	app.resizeTables()
	seedBenchmarkGenericMultiClusterRows(app, 8, 2000, resource)
	app.screen = screenResourceList
	app.lastManagerVersion = manager.Version()
	if detail {
		row, ok := app.genericResourceRowAt(0, time.Now())
		if !ok {
			b.Fatalf("expected generic row")
		}
		app.activeGenericDetails = cluster.GenericResourceDetails{Row: row}
		app.screen = screenResourceDetails
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.BumpVersionForTest()
		_, _ = app.Update(tickMsg(time.Now()))
	}
}

func BenchmarkTickGenericResourceList8ClustersUnderManagerChurn(b *testing.B) {
	benchmarkGenericResourceUnderManagerChurn(b, false)
}

func BenchmarkTickGenericResourceDetails8ClustersUnderManagerChurn(b *testing.B) {
	benchmarkGenericResourceUnderManagerChurn(b, true)
}

func BenchmarkTickGenericResourceDetails8ClustersUnderSameResourceChurn(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	app := New(state.NewStore(), manager, Config{})
	resource := cluster.ResourceKind{ID: "serving.knative.dev/services", Display: "Services", Resource: "services", APIGroup: "serving.knative.dev", Version: "v1", Kind: "Service", Namespaced: true, Custom: true}
	app.width = 220
	app.height = 40
	app.resizeTables()
	seedBenchmarkGenericMultiClusterRows(app, 8, 2000, resource)
	key := cluster.GenericResourceKey{Cluster: "cluster-00", Namespace: "default", Name: "svc-00-00000"}
	fixture := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "serving.knative.dev/v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":              key.Name,
			"namespace":         key.Namespace,
			"resourceVersion":   "1",
			"creationTimestamp": metav1.NewTime(time.Now().Add(-time.Hour)).Format(time.RFC3339),
		},
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{map[string]interface{}{"image": "example/api:latest", "env": []interface{}{map[string]interface{}{"name": "LONG_PAYLOAD", "value": strings.Repeat("x", 2048)}}}},
				},
			},
		},
		"status": map[string]interface{}{
			"url":        "https://api.example.com",
			"conditions": []interface{}{map[string]interface{}{"type": "Ready", "status": "True", "reason": "Ready"}},
		},
	}}
	manager.SetGenericResourceFixtureForTest(resource.ID, key.Cluster, fixture)
	details, err := manager.GenericResourceDetails(context.Background(), resource, key, time.Now())
	if err != nil {
		b.Fatalf("generic details: %v", err)
	}
	app.activeResource = resource
	app.activeGenericDetails = details
	app.screen = screenResourceDetails
	app.lastManagerVersion = manager.GenericResourceVersion(resource.ID)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.BumpGenericResourceVersionForTest(resource.ID)
		_, _ = app.Update(tickMsg(time.Now()))
	}
}

func setupGenericOffScopeListBenchmark(b *testing.B) (*App, *cluster.Manager, *unstructured.Unstructured, cluster.ResourceKind, cluster.GenericResourceKey) {
	b.Helper()
	manager := newTestManagerForBenchmark(b)
	app := New(state.NewStore(), manager, Config{})
	resource := cluster.ResourceKind{ID: "serving.knative.dev/services", Display: "Services", Resource: "services", APIGroup: "serving.knative.dev", Version: "v1", Kind: "Service", Namespaced: true, Custom: true}
	app.width = 220
	app.height = 40
	app.resizeTables()
	app.activeResource = resource
	app.namespace = "default"
	app.screen = screenResourceList

	for c := 0; c < 8; c++ {
		clusterName := fmt.Sprintf("cluster-%02d", c)
		for i := 0; i < 2000; i++ {
			object := &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "serving.knative.dev/v1",
				"kind":       "Service",
				"metadata": map[string]interface{}{
					"name":              fmt.Sprintf("svc-%02d-%05d", c, i),
					"namespace":         "default",
					"resourceVersion":   fmt.Sprintf("%d", i+1),
					"creationTimestamp": metav1.NewTime(time.Now().Add(-time.Hour)).Format(time.RFC3339),
				},
				"status": map[string]interface{}{
					"conditions": []interface{}{map[string]interface{}{"type": "Ready", "status": "True", "reason": "Ready"}},
				},
			}}
			manager.SetGenericResourceFixtureForTest(resource.ID, clusterName, object)
		}
	}

	fixtureKey := cluster.GenericResourceKey{Cluster: "cluster-00", Namespace: "kube-system", Name: "hidden-service"}
	fixture := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "serving.knative.dev/v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":              fixtureKey.Name,
			"namespace":         fixtureKey.Namespace,
			"resourceVersion":   "1",
			"creationTimestamp": metav1.NewTime(time.Now().Add(-time.Hour)).Format(time.RFC3339),
		},
		"status": map[string]interface{}{
			"conditions": []interface{}{map[string]interface{}{"type": "Ready", "status": "True", "reason": "Ready"}},
		},
	}}
	manager.SetGenericResourceFixtureForTest(resource.ID, fixtureKey.Cluster, fixture)
	app.refreshGenericResourceList(time.Now())
	return app, manager, fixture, resource, fixtureKey
}

func BenchmarkRefreshGenericResourceList8ClustersUnderOffScopeSameKindFullReload(b *testing.B) {
	app, manager, fixture, resource, fixtureKey := setupGenericOffScopeListBenchmark(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fixture.Object["metadata"].(map[string]interface{})["resourceVersion"] = fmt.Sprintf("%d", i+2)
		manager.SetGenericResourceFixtureForTest(resource.ID, fixtureKey.Cluster, fixture)
		manager.BumpGenericResourceVersionForTest(resource.ID)
		app.refreshGenericResourceList(time.Now())
	}
}

func BenchmarkRefreshGenericResourceList8ClustersUnderOffScopeSameKindDelta(b *testing.B) {
	app, manager, fixture, resource, fixtureKey := setupGenericOffScopeListBenchmark(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fixture.Object["metadata"].(map[string]interface{})["resourceVersion"] = fmt.Sprintf("%d", i+2)
		manager.SetGenericResourceFixtureForTest(resource.ID, fixtureKey.Cluster, fixture)
		app.refreshGenericResourceList(time.Now())
	}
}
