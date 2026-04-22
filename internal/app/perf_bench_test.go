package app

import (
	"fmt"
	"os"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

func BenchmarkRefreshPods10000CachedUsage(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	seedBenchmarkPods(store, app, 10000)
	app.screen = screenPods
	app.width = 220
	app.height = 40
	app.resizeTables()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.refreshPods(time.Now())
	}
}

func BenchmarkBuildPodOverviewCard10000(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	seedBenchmarkPods(store, app, 10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.buildPodOverviewCard()
	}
}

func BenchmarkBuildRecentRestartLines10000(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	seedBenchmarkPods(store, app, 10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.buildRecentRestartLines(time.Now())
	}
}

func newTestManagerForBenchmark(b *testing.B) *cluster.Manager {
	b.Helper()
	dir := b.TempDir()
	kubeconfigPath := dir + "/config"
	content := `apiVersion: v1
kind: Config
current-context: dev
clusters:
- cluster:
    server: https://example.invalid
  name: dev-cluster
contexts:
- context:
    cluster: dev-cluster
    user: dev-user
  name: dev
users:
- name: dev-user
  user:
    token: test
`
	if err := os.WriteFile(kubeconfigPath, []byte(content), 0o644); err != nil {
		b.Fatalf("write kubeconfig: %v", err)
	}
	manager, err := cluster.NewManager(state.NewStore(), cluster.Config{KubeconfigPath: kubeconfigPath})
	if err != nil {
		b.Fatalf("new manager: %v", err)
	}
	b.Cleanup(manager.Close)
	return manager
}

func seedBenchmarkPods(store *state.Store, app *App, count int) {
	now := time.Now()
	app.podUsageByKey = make(map[string]cluster.PodResourceUsage, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("pod-%05d", i)
		namespace := "default"
		phase := corev1.PodRunning
		status := []corev1.ContainerStatus{{RestartCount: int32(i % 4)}}
		if i%17 == 0 {
			phase = corev1.PodPending
		}
		if i%29 == 0 {
			status = []corev1.ContainerStatus{{RestartCount: int32(3 + i%7), LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 137, FinishedAt: metav1.NewTime(now.Add(-time.Duration(i%300) * time.Second))}}}}
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, CreationTimestamp: metav1.NewTime(now.Add(-time.Duration(i) * time.Minute))},
			Spec:       corev1.PodSpec{NodeName: fmt.Sprintf("node-%02d", i%25), Containers: []corev1.Container{{Name: "main", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("256Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi")}}}}},
			Status:     corev1.PodStatus{Phase: phase, ContainerStatuses: status},
		}
		store.UpsertPod("dev", pod)
		key := state.PodKey{Cluster: "dev", Namespace: namespace, Name: name}.String()
		app.podUsageByKey[key] = cluster.PodResourceUsage{
			CPUUsedMilli:       int64(50 + i%700),
			CPULimitMilli:      1000,
			CPURequestMilli:    250,
			MemoryUsedBytes:    int64((128 + i%512) * 1024 * 1024),
			MemoryLimitBytes:   int64(1024 * 1024 * 1024),
			MemoryRequestBytes: int64(256 * 1024 * 1024),
			HasCPUUsage:        true,
			HasMemoryUsage:     true,
		}
	}
}

func markBenchmarkPodUsageReady(app *App) {
	app.podUsageListScopeKey = app.podUsageScopeKey()
	app.podUsageListFetchedAt = time.Now()
	app.podUsageCellByKey = buildPodUsageTableCellCache(app.podUsageByKey)
}

func BenchmarkViewPods10000(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	seedBenchmarkPods(store, app, 10000)
	app.screen = screenPods
	app.width = 220
	app.height = 40
	app.resizeTables()
	app.refreshPods(time.Now())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.View()
	}
}

func BenchmarkPodsTableView10000(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	seedBenchmarkPods(store, app, 10000)
	app.screen = screenPods
	app.width = 220
	app.height = 40
	app.resizeTables()
	app.refreshPods(time.Now())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.podTable.View()
	}
}

func BenchmarkViewPods10000WarmUsage(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	seedBenchmarkPods(store, app, 10000)
	app.screen = screenPods
	app.width = 220
	app.height = 40
	markBenchmarkPodUsageReady(app)
	app.resizeTables()
	app.refreshPods(time.Now())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.View()
	}
}

func BenchmarkRefreshAndViewPods5300WarmUsage4Clusters(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	seedBenchmarkMultiClusterTyped(store, 4, 0, 0, 32, 1325)
	app.screen = screenPods
	app.width = 220
	app.height = 40
	app.contextScope = ""
	app.podUsageByKey = make(map[string]cluster.PodResourceUsage, 5300)
	store.ForEachPod(func(row state.PodRow) bool {
		app.podUsageByKey[row.Key.String()] = cluster.PodResourceUsage{
			Key:                row.Key,
			CPUUsedMilli:       100,
			CPURequestMilli:    250,
			CPULimitMilli:      1000,
			MemoryUsedBytes:    256 * 1024 * 1024,
			MemoryRequestBytes: 256 * 1024 * 1024,
			MemoryLimitBytes:   1024 * 1024 * 1024,
			HasCPUUsage:        true,
			HasMemoryUsage:     true,
		}
		return true
	})
	app.podUsageListScopeKey = app.podUsageScopeKey()
	app.podUsageListGeneration = 1
	app.podUsageListFetchedAt = time.Now()
	app.podUsageListLoading = true
	app.resizeTables()
	app.refreshPods(time.Now())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.podUsageCellByKey = nil
		app.refreshPods(time.Now())
		_ = app.View()
	}
}

func BenchmarkPodsTableView10000WarmUsage(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	seedBenchmarkPods(store, app, 10000)
	app.screen = screenPods
	app.width = 220
	app.height = 40
	markBenchmarkPodUsageReady(app)
	app.resizeTables()
	app.refreshPods(time.Now())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.podTable.View()
	}
}

func seedBenchmarkDeployments(store *state.Store, count int) {
	now := time.Now()
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("deploy-%05d", i)
		replicas := int32(3)
		updated := int32(3)
		available := int32(2 + i%2)
		store.UpsertDeployment("dev", &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", CreationTimestamp: metav1.NewTime(now.Add(-time.Duration(i) * time.Minute))},
			Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
			Status:     appsv1.DeploymentStatus{UpdatedReplicas: updated, AvailableReplicas: available, ReadyReplicas: available},
		})
	}
}

func seedBenchmarkServices(store *state.Store, count int) {
	now := time.Now()
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("svc-%05d", i)
		store.UpsertService("dev", &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", CreationTimestamp: metav1.NewTime(now.Add(-time.Duration(i) * time.Minute))},
			Spec: corev1.ServiceSpec{
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: fmt.Sprintf("10.0.%d.%d", (i/255)%255, i%255),
				Ports:     []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
			},
		})
	}
}

func seedBenchmarkNodes(store *state.Store, app *App, count int) {
	now := time.Now()
	app.nodeUsageByKey = make(map[string]cluster.NodeResourceUsage, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("node-%05d", i)
		store.UpsertNode("dev", &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: metav1.NewTime(now.Add(-time.Duration(i) * time.Hour))},
			Status: corev1.NodeStatus{
				Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("16Gi"), corev1.ResourceEphemeralStorage: resource.MustParse("100Gi")},
				Conditions:  []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
				NodeInfo:    corev1.NodeSystemInfo{KubeletVersion: "v1.32.0"},
			},
		})
		key := state.NodeKey{Cluster: "dev", Name: name}.String()
		app.nodeUsageByKey[key] = cluster.NodeResourceUsage{CPUUsedMilli: int64(200 + i%2000), CPUAllocatableMilli: 4000, MemoryUsedBytes: int64((2 + i%8) * 1024 * 1024 * 1024), MemoryAllocatable: 16 * 1024 * 1024 * 1024, HasCPUUsage: true, HasMemoryUsage: true}
	}
}

func markBenchmarkNodeUsageReady(app *App) {
	app.nodeUsageListScopeKey = app.nodeUsageScopeKey()
	app.nodeUsageListFetchedAt = time.Now()
	app.nodeUsageCellByKey = buildNodeUsageTableCellCache(app.nodeUsageByKey)
}

func seedBenchmarkGenericRows(app *App, count int) {
	now := time.Now()
	app.activeResource = cluster.ResourceKind{ID: "serving.knative.dev/services", Display: "Services", Resource: "services", APIGroup: "serving.knative.dev", Version: "v1", Namespaced: true, Custom: true, PrinterColumns: []cluster.PrinterColumn{{Name: "URL", JSONPath: ".status.url"}, {Name: "READY", JSONPath: ".status.conditions[?(@.type==\"Ready\")].status"}}}
	app.genericCompiledColumns = cluster.CompilePrinterColumns(app.activeResource.PrinterColumns)
	app.genericCompiledColumnsVersion = app.activeResource.ID
	app.genericRows = make([]cluster.GenericResourceRow, 0, count)
	app.genericRowsByKey = make(map[string]cluster.GenericResourceRow, count)
	app.genericNamespaceCounts = map[string]int{"default": count}
	app.sortedGenericRows = make([]cluster.GenericResourceRow, 0, count)
	for i := 0; i < count; i++ {
		obj := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "serving.knative.dev/v1",
			"kind":       "Service",
			"metadata":   map[string]interface{}{"name": fmt.Sprintf("svc-%05d", i), "namespace": "default", "resourceVersion": fmt.Sprintf("%d", i+1), "creationTimestamp": now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339)},
			"status":     map[string]interface{}{"url": fmt.Sprintf("https://svc-%05d.example.com", i), "conditions": []interface{}{map[string]interface{}{"type": "Ready", "status": "True"}}},
		}}
		row := cluster.GenericResourceRow{Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: obj.GetName()}, Cluster: "dev", Namespace: "default", Name: obj.GetName(), Object: obj, ResourceVersion: obj.GetResourceVersion()}.WithAge(now)
		app.genericRows = append(app.genericRows, row)
		app.genericRowsByKey[row.Key.String()] = row
		app.sortedGenericRows = append(app.sortedGenericRows, row)
	}
}

func BenchmarkViewDeployments10000Warm(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	seedBenchmarkDeployments(store, 10000)
	app.screen = screenResourceList
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	app.width = 220
	app.height = 40
	app.resizeTables()
	app.refreshResourceList(time.Now())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.resourceTable.View()
	}
}

func BenchmarkViewServices10000Warm(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	seedBenchmarkServices(store, 10000)
	app.screen = screenResourceList
	app.activeResource = cluster.ResourceKind{Display: "Services", Resource: "services", Namespaced: true}
	app.width = 220
	app.height = 40
	app.resizeTables()
	app.refreshResourceList(time.Now())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.resourceTable.View()
	}
}

func BenchmarkViewNodes10000Warm(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	seedBenchmarkNodes(store, app, 10000)
	markBenchmarkNodeUsageReady(app)
	app.screen = screenResourceList
	app.activeResource = cluster.ResourceKind{Display: "Nodes", Resource: "nodes", Namespaced: false}
	app.width = 220
	app.height = 40
	app.resizeTables()
	app.refreshResourceList(time.Now())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.resourceTable.View()
	}
}

func BenchmarkViewGenericResources10000Warm(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	_ = store
	seedBenchmarkGenericRows(app, 10000)
	app.screen = screenResourceList
	app.width = 220
	app.height = 40
	app.resourceTable.SetColumns(app.genericView.Columns(app.activeResource))
	app.resourceTable.SetWindowProvider(len(app.sortedGenericRows), func(start int, end int) [][]string {
		return app.genericTableRows(start, end-start, time.Now())
	})
	app.resizeTables()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.resourceTable.View()
	}
}

func BenchmarkMatchPodRowAt10000NoColumnFilters(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	seedBenchmarkPods(store, app, 10000)
	app.screen = screenPods
	now := time.Now()
	var row state.PodRow
	store.ForEachPod(func(r state.PodRow) bool {
		row = r
		return false
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.matchPodRowAt(row, now)
	}
}
