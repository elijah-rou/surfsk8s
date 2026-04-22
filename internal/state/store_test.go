package state

import (
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStoreUpsertDeleteAndFilterPods(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	frontend := newTestPod("frontend", "web", corev1.PodRunning, 2, 1, "node-a", now.Add(-5*time.Minute), "1")
	backend := newTestPod("backend", "api", corev1.PodPending, 1, 0, "node-b", now.Add(-2*time.Minute), "2")

	store.UpsertPod("dev", frontend)
	store.UpsertPod("dev", backend)

	all := store.SnapshotPods(PodQuery{}, now)
	if got, want := len(all.Rows), 2; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	if got, want := all.Version, uint64(2); got != want {
		t.Fatalf("version = %d, want %d", got, want)
	}
	if got, want := all.Rows[0].Name, "backend"; got != want {
		t.Fatalf("first row = %q, want %q", got, want)
	}

	apiOnly := store.SnapshotPods(PodQuery{Namespace: "api"}, now)
	if got, want := len(apiOnly.Rows), 1; got != want {
		t.Fatalf("namespace rows = %d, want %d", got, want)
	}
	if got, want := apiOnly.Rows[0].Name, "backend"; got != want {
		t.Fatalf("namespace row = %q, want %q", got, want)
	}

	filtered := store.SnapshotPods(PodQuery{Search: "node-a"}, now)
	if got, want := len(filtered.Rows), 1; got != want {
		t.Fatalf("filtered rows = %d, want %d", got, want)
	}
	if got, want := filtered.Rows[0].Ready, "1/2"; got != want {
		t.Fatalf("ready = %q, want %q", got, want)
	}
	if got, want := filtered.Rows[0].Restarts, 3; got != want {
		t.Fatalf("restarts = %d, want %d", got, want)
	}

	store.DeletePodByKey("dev", "api", "backend")
	afterDelete := store.SnapshotPods(PodQuery{}, now)
	if got, want := len(afterDelete.Rows), 1; got != want {
		t.Fatalf("rows after delete = %d, want %d", got, want)
	}
	if got, want := afterDelete.Rows[0].Name, "frontend"; got != want {
		t.Fatalf("remaining row = %q, want %q", got, want)
	}
}

func TestStoreDoesNotBumpVersionForUnchangedPod(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	pod := newTestPod("frontend", "web", corev1.PodRunning, 1, 0, "node-a", now.Add(-time.Minute), "1")

	store.UpsertPod("dev", pod)
	store.UpsertPod("dev", pod.DeepCopy())

	snapshot := store.SnapshotPods(PodQuery{}, now)
	if got, want := snapshot.Version, uint64(1); got != want {
		t.Fatalf("version = %d, want %d", got, want)
	}
}

func TestStorePodResourceVersionByKey(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	pod := newTestPod("frontend", "web", corev1.PodRunning, 1, 1, "node-a", now.Add(-time.Minute), "42")

	store.UpsertPod("dev", pod)

	resourceVersion, ok := store.PodResourceVersionByKey(PodKey{Cluster: "dev", Namespace: "web", Name: "frontend"})
	if !ok {
		t.Fatalf("expected pod resource version")
	}
	if got, want := resourceVersion, "42"; got != want {
		t.Fatalf("resourceVersion = %q, want %q", got, want)
	}
}

func TestStorePodObjectByKey(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	pod := newTestPod("frontend", "web", corev1.PodRunning, 1, 1, "node-a", now.Add(-time.Minute), "42")
	store.UpsertPod("dev", pod)

	key := PodKey{Cluster: "dev", Namespace: "web", Name: "frontend"}
	obj, ok := store.PodObjectByKey(key)
	if !ok {
		t.Fatalf("expected pod object")
	}
	if obj == nil || obj.Name != "frontend" {
		t.Fatalf("unexpected pod object")
	}
	if _, ok := store.PodObjectByKey(PodKey{Cluster: "dev", Namespace: "missing", Name: "x"}); ok {
		t.Fatalf("expected miss")
	}
}

func newTestPod(name string, namespace string, phase corev1.PodPhase, containers int, ready int, node string, createdAt time.Time, resourceVersion string) *corev1.Pod {
	statuses := make([]corev1.ContainerStatus, 0, containers)
	for idx := range containers {
		status := corev1.ContainerStatus{Ready: idx < ready, RestartCount: int32(idx + 1)}
		statuses = append(statuses, status)
	}

	containersSpec := make([]corev1.Container, 0, containers)
	for idx := range containers {
		containersSpec = append(containersSpec, corev1.Container{Name: fmt.Sprintf("%s-c%d", name, idx)})
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.NewTime(createdAt),
			ResourceVersion:   resourceVersion,
		},
		Spec: corev1.PodSpec{
			NodeName:   node,
			Containers: containersSpec,
		},
		Status: corev1.PodStatus{
			Phase:             phase,
			ContainerStatuses: statuses,
		},
	}
}

func TestStoreKindVersionsAdvanceIndependently(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	store.UpsertPod("dev", newTestPod("frontend", "web", corev1.PodRunning, 1, 1, "node-a", now.Add(-time.Minute), "1"))
	if got, want := store.PodVersion(), uint64(1); got != want {
		t.Fatalf("podVersion = %d, want %d", got, want)
	}
	if got, want := store.DeploymentVersion(), uint64(0); got != want {
		t.Fatalf("deploymentVersion = %d, want %d", got, want)
	}
	if got, want := store.ServiceVersion(), uint64(0); got != want {
		t.Fatalf("serviceVersion = %d, want %d", got, want)
	}
	if got, want := store.NodeVersion(), uint64(0); got != want {
		t.Fatalf("nodeVersion = %d, want %d", got, want)
	}
}
