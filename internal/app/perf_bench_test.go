package app

import (
	"fmt"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
