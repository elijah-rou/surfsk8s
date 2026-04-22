package app

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

func TestBuildPodOverviewCardSummarizesStatuses(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})

	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "run", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pending", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodPending}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "crash", Namespace: "default"}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}}})

	card := app.buildPodOverviewCard()
	if got, want := card.Title, "Pods"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
	if len(card.Metrics) < 3 {
		t.Fatalf("metrics = %d, want >= 3", len(card.Metrics))
	}
	if got, want := card.Metrics[0].Label, "Running"; got != want {
		t.Fatalf("metric[0] = %q, want %q", got, want)
	}
	if got, want := card.Metrics[1].Label, "Pending"; got != want {
		t.Fatalf("metric[1] = %q, want %q", got, want)
	}
	if got, want := card.Metrics[2].Label, "CrashLoopBackOff"; got != want {
		t.Fatalf("metric[2] = %q, want %q", got, want)
	}
}

func TestGenericReplicaOverviewBucket(t *testing.T) {
	cases := []struct {
		row  cluster.GenericResourceRow
		want string
		tone overviewTone
	}{
		{row: cluster.GenericResourceRow{Ready: "3/3"}, want: "Running", tone: overviewToneOK},
		{row: cluster.GenericResourceRow{Ready: "1/3"}, want: "Pending", tone: overviewToneWarn},
		{row: cluster.GenericResourceRow{Ready: "0/3"}, want: "Unavailable", tone: overviewToneError},
		{row: cluster.GenericResourceRow{Ready: "0/0"}, want: "Idle", tone: overviewToneMuted},
	}
	for _, tc := range cases {
		got, tone := genericReplicaOverviewBucket(tc.row)
		if got != tc.want || tone != tc.tone {
			t.Fatalf("bucket(%q) = (%q,%d), want (%q,%d)", tc.row.Ready, got, tone, tc.want, tc.tone)
		}
	}
}

func TestPodRestartOverviewItemUsesLastTermination(t *testing.T) {
	finished := metav1.NewTime(time.Now().Add(-3 * time.Minute))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour))},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			RestartCount:         7,
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137, FinishedAt: finished}},
		}}},
	}
	row := state.PodRow{Cluster: "dev", Namespace: "default", Name: "api", Status: "Running"}
	item, ok := podRestartOverviewItem(pod, row)
	if !ok {
		t.Fatalf("expected restart item")
	}
	if got, want := item.Reason, "OOMKilled"; got != want {
		t.Fatalf("reason = %q, want %q", got, want)
	}
	if got, want := item.ExitCode, int32(137); got != want {
		t.Fatalf("exit code = %d, want %d", got, want)
	}
	if got, want := item.Restarts, 7; got != want {
		t.Fatalf("restarts = %d, want %d", got, want)
	}
}

func TestJobOverviewBucket(t *testing.T) {
	job := &unstructured.Unstructured{Object: map[string]interface{}{"status": map[string]interface{}{"failed": int64(1)}}}
	label, tone := jobOverviewBucket(job)
	if label != "Failed" || tone != overviewToneError {
		t.Fatalf("job bucket = (%q,%d), want (%q,%d)", label, tone, "Failed", overviewToneError)
	}
}

func TestRenderCatalogOverviewIncludesSections(t *testing.T) {
	rendered := renderCatalogOverview(catalogOverviewData{
		ScopeLabel: "All namespaces",
		Cards:      []overviewCard{{Title: "Pods", Metrics: []overviewMetric{{Label: "Running", Count: 2, Tone: overviewToneOK}}}},
		Warnings:   []overviewLine{{Primary: "FailedScheduling (7x)", Secondary: "1m ago", Tone: overviewToneWarn}},
		Restarts:   []overviewLine{{Primary: "default / api", Secondary: "OOMKilled (ExitCode: 137)  (4x)  1m ago", Tone: overviewToneError}},
	}, 120, 20)
	for _, fragment := range []string{"All namespaces", "Pods", "Recent Warnings", "Recent Restarts"} {
		if !containsText(rendered, fragment) {
			t.Fatalf("missing fragment %q in\n%s", fragment, rendered)
		}
	}
}

func containsText(value string, fragment string) bool {
	return strings.Contains(value, fragment)
}

func TestWarningMinHeapKeepsNewestByTime(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	items := make([]eventOverviewItem, 0, 2)
	items = keepWarningCandidate(items, eventOverviewItem{When: now.Add(-3 * time.Hour), Reason: "old"}, 2)
	items = keepWarningCandidate(items, eventOverviewItem{When: now, Reason: "newest"}, 2)
	items = keepWarningCandidate(items, eventOverviewItem{When: now.Add(-time.Hour), Reason: "mid"}, 2)
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
	reasons := map[string]struct{}{}
	for _, it := range items {
		reasons[it.Reason] = struct{}{}
	}
	if _, ok := reasons["newest"]; !ok {
		t.Fatalf("missing newest: %#v", items)
	}
	if _, ok := reasons["mid"]; !ok {
		t.Fatalf("missing mid: %#v", items)
	}
}

func TestRestartMinHeapKeepsBestBySortOrder(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	items := make([]restartOverviewItem, 0, 2)
	candidates := []restartOverviewItem{
		{Pod: "a", When: now.Add(-time.Hour), Restarts: 1},
		{Pod: "b", When: now, Restarts: 9},
		{Pod: "c", When: now, Restarts: 3},
		{Pod: "d", When: now.Add(time.Minute), Restarts: 1},
	}
	for _, it := range candidates {
		items = keepRestartCandidate(items, it, 2)
	}
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
	pods := map[string]struct{}{}
	for _, it := range items {
		pods[it.Pod] = struct{}{}
	}
	if _, ok := pods["b"]; !ok {
		t.Fatalf("missing b: %#v", items)
	}
	if _, ok := pods["d"]; !ok {
		t.Fatalf("missing d: %#v", items)
	}
}

func TestBuildGenericWorkloadOverviewCardFallsBackToManagerCatalog(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	resource := cluster.ResourceKind{ID: "apps/deployments", Display: "Deployments", Resource: "deployments", APIGroup: "apps", Version: "v1", Kind: "Deployment", Namespaced: true}
	manager.SetDiscoveredResourcesForTest("dev", []cluster.ResourceKind{resource})
	manager.SetGenericResourceFixtureForTest(resource.ID, "dev", &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":              "api",
			"namespace":         "default",
			"resourceVersion":   "1",
			"creationTimestamp": time.Now().Add(-time.Hour).Format(time.RFC3339),
		},
		"status": map[string]interface{}{
			"readyReplicas": int64(3),
			"replicas":      int64(3),
		},
	}})

	card := app.buildGenericWorkloadOverviewCard(context.Background(), "Deployments", "apps", "deployments")
	if got, want := card.Title, "Deployments"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
	if len(card.Metrics) != 1 {
		t.Fatalf("metrics = %d, want 1", len(card.Metrics))
	}
	if got, want := card.Metrics[0].Label, "Running"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
	if got, want := card.Metrics[0].Count, 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
}
