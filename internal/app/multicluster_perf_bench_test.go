package app

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

func seedBenchmarkMultiClusterTyped(store *state.Store, clusters int, perClusterDeployments int, perClusterServices int, perClusterNodes int, perClusterPods int) []state.PodKey {
	now := time.Now()
	keys := make([]state.PodKey, 0, clusters*perClusterPods)
	for c := 0; c < clusters; c++ {
		clusterName := fmt.Sprintf("cluster-%02d", c)
		for i := 0; i < perClusterDeployments; i++ {
			replicas := int32(3)
			name := fmt.Sprintf("deploy-%02d-%05d", c, i)
			store.UpsertDeployment(clusterName, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", ResourceVersion: fmt.Sprintf("%d", i+1), CreationTimestamp: metav1.NewTime(now.Add(-time.Duration(i) * time.Minute))},
				Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
				Status:     appsv1.DeploymentStatus{UpdatedReplicas: 3, AvailableReplicas: 3, ReadyReplicas: 3},
			})
		}
		for i := 0; i < perClusterServices; i++ {
			name := fmt.Sprintf("svc-%02d-%05d", c, i)
			store.UpsertService(clusterName, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", ResourceVersion: fmt.Sprintf("%d", i+1), CreationTimestamp: metav1.NewTime(now.Add(-time.Duration(i) * time.Minute))},
				Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: fmt.Sprintf("10.%d.%d.%d", c%255, (i/255)%255, i%255), Ports: []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}}},
			})
		}
		for i := 0; i < perClusterNodes; i++ {
			name := fmt.Sprintf("node-%02d-%05d", c, i)
			store.UpsertNode(clusterName, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, ResourceVersion: fmt.Sprintf("%d", i+1), CreationTimestamp: metav1.NewTime(now.Add(-time.Duration(i) * time.Hour))}})
		}
		for i := 0; i < perClusterPods; i++ {
			name := fmt.Sprintf("pod-%02d-%05d", c, i)
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", ResourceVersion: fmt.Sprintf("%d", i+1), CreationTimestamp: metav1.NewTime(now.Add(-time.Duration(i) * time.Second))},
				Spec:       corev1.PodSpec{NodeName: fmt.Sprintf("node-%02d-%05d", c, i%max(1, perClusterNodes)), Containers: []corev1.Container{{Name: "main"}}},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}},
			}
			store.UpsertPod(clusterName, pod)
			keys = append(keys, state.PodKey{Cluster: clusterName, Namespace: "default", Name: name})
		}
	}
	return keys
}

func churnBenchmarkPod(store *state.Store, key state.PodKey, iter int) {
	store.UpsertPod(key.Cluster, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, ResourceVersion: fmt.Sprintf("rv-%d", iter), CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Minute))},
		Spec:       corev1.PodSpec{NodeName: "node", Containers: []corev1.Container{{Name: "main"}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true, RestartCount: int32(iter % 3)}}},
	})
}

func benchmarkTypedResourceUnderPodChurn(b *testing.B, resource cluster.ResourceKind) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	podKeys := seedBenchmarkMultiClusterTyped(store, 8, 400, 400, 64, 2000)
	app.screen = screenResourceList
	app.activeResource = resource
	app.width = 220
	app.height = 40
	app.resizeTables()
	app.refreshResourceList(time.Now())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		churnBenchmarkPod(store, podKeys[i%len(podKeys)], i)
		_, _ = app.Update(tickMsg(time.Now()))
	}
}

func BenchmarkTickDeployments8ClustersUnderPodChurn(b *testing.B) {
	benchmarkTypedResourceUnderPodChurn(b, cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true})
}

func BenchmarkTickServices8ClustersUnderPodChurn(b *testing.B) {
	benchmarkTypedResourceUnderPodChurn(b, cluster.ResourceKind{Display: "Services", Resource: "services", Namespaced: true})
}

func BenchmarkTickNodes8ClustersUnderPodChurn(b *testing.B) {
	benchmarkTypedResourceUnderPodChurn(b, cluster.ResourceKind{Display: "Nodes", Resource: "nodes", Namespaced: false})
}

func BenchmarkTickPodDetails8ClustersOtherPodChurn(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	podKeys := seedBenchmarkMultiClusterTyped(store, 8, 100, 100, 32, 2000)
	app.screen = screenPods
	app.width = 220
	app.height = 40
	app.resizeTables()
	app.refreshPods(time.Now())
	details, ok := store.PodDetailsByKey(podKeys[0], time.Now())
	if !ok {
		b.Fatalf("expected pod details")
	}
	app.activePod = details
	app.screen = screenPodDetails
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		churnBenchmarkPod(store, podKeys[(i+1)%len(podKeys)], i)
		_, _ = app.Update(tickMsg(time.Now()))
	}
}

func BenchmarkTickDeploymentDetails8ClustersPodChurn(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	podKeys := seedBenchmarkMultiClusterTyped(store, 8, 400, 100, 32, 2000)
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	app.screen = screenResourceList
	app.width = 220
	app.height = 40
	app.resizeTables()
	app.refreshResourceList(time.Now())
	details, ok := store.DeploymentDetailsByKey(state.DeploymentKey{Cluster: "cluster-00", Namespace: "default", Name: "deploy-00-00000"}, time.Now())
	if !ok {
		b.Fatalf("expected deployment details")
	}
	app.activeDeployment = details
	app.screen = screenResourceDetails
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		churnBenchmarkPod(store, podKeys[i%len(podKeys)], i)
		_, _ = app.Update(tickMsg(time.Now()))
	}
}

func BenchmarkShouldRefreshDeployments8ClustersPodChurn(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	podKeys := seedBenchmarkMultiClusterTyped(store, 8, 400, 100, 32, 2000)
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	app.screen = screenResourceList
	app.refreshResourceList(time.Now())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		churnBenchmarkPod(store, podKeys[i%len(podKeys)], i)
		_ = app.shouldRefresh(time.Now())
	}
}

var _ = tea.KeyMsg{}

func BenchmarkTickPods4ClustersUnderPodChurn(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	podKeys := seedBenchmarkMultiClusterTyped(store, 4, 200, 200, 32, 1250)
	app.screen = screenPods
	app.width = 220
	app.height = 40
	app.resizeTables()
	app.refreshPods(time.Now())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		churnBenchmarkPod(store, podKeys[i%len(podKeys)], i)
		_, _ = app.Update(tickMsg(time.Now()))
	}
}

func BenchmarkListPodResourceUsages5000(b *testing.B) {
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
	store := state.NewStore()
	manager, err := cluster.NewManager(store, cluster.Config{KubeconfigPath: kubeconfigPath})
	if err != nil {
		b.Fatalf("new manager: %v", err)
	}
	b.Cleanup(manager.Close)
	seedBenchmarkMultiClusterTyped(store, 4, 0, 0, 0, 1250)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = manager.ListPodResourceUsages(context.Background(), "", "")
	}
}

func BenchmarkHandlePodUsageSnapshot5000LazyCache(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	seedBenchmarkMultiClusterTyped(store, 4, 0, 0, 0, 1250)
	app.screen = screenPods
	app.width = 220
	app.height = 40
	app.resizeTables()
	app.refreshPods(time.Now())
	usages := make(map[string]cluster.PodResourceUsage, 5000)
	store.ForEachPod(func(row state.PodRow) bool {
		usages[row.Key.String()] = cluster.PodResourceUsage{Key: row.Key, CPUUsedMilli: 100, CPULimitMilli: 500, HasCPUUsage: true, MemoryUsedBytes: 256 * 1024 * 1024, MemoryLimitBytes: 512 * 1024 * 1024, HasMemoryUsage: true}
		return true
	})
	app.podUsageListGeneration = 1
	msg := podUsageSnapshotMsg{scopeKey: app.podUsageScopeKey(), generation: app.podUsageListGeneration, usages: usages}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		clone := *app
		clone.podUsageByKey = nil
		clone.podUsageCellByKey = nil
		clone.Update(msg)
	}
}

func BenchmarkViewPods5000LazyUsageWindow(b *testing.B) {
	manager := newTestManagerForBenchmark(b)
	store := state.NewStore()
	app := New(store, manager, Config{})
	seedBenchmarkMultiClusterTyped(store, 4, 0, 0, 0, 1250)
	app.screen = screenPods
	app.width = 220
	app.height = 40
	app.resizeTables()
	app.refreshPods(time.Now())
	app.podUsageByKey = make(map[string]cluster.PodResourceUsage, 5000)
	store.ForEachPod(func(row state.PodRow) bool {
		app.podUsageByKey[row.Key.String()] = cluster.PodResourceUsage{Key: row.Key, CPUUsedMilli: 100, CPULimitMilli: 500, HasCPUUsage: true, MemoryUsedBytes: 256 * 1024 * 1024, MemoryLimitBytes: 512 * 1024 * 1024, HasMemoryUsage: true}
		return true
	})
	app.podUsageListScopeKey = app.podUsageScopeKey()
	app.podUsageListFetchedAt = time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.podUsageCellByKey = nil
		_ = app.View()
	}
}
