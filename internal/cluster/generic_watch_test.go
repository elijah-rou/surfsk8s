package cluster

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestGenericResourceWatchCachesSortedRows(t *testing.T) {
	watch := &genericResourceWatch{
		clusterName: "dev",
		resource:    ResourceKind{ID: "serving.knative.dev/services"},
		objects:     make(map[GenericResourceKey]*unstructured.Unstructured, 4),
		rows:        make(map[GenericResourceKey]GenericResourceRow, 4),
		sortedDirty: true,
	}
	watch.upsertObject(&unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": "b", "namespace": "default", "creationTimestamp": time.Now().Format(time.RFC3339)},
	}})
	watch.upsertObject(&unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": "a", "namespace": "default", "creationTimestamp": time.Now().Format(time.RFC3339)},
	}})

	rows := watch.listRows()
	if got, want := rows[0].Name, "a"; got != want {
		t.Fatalf("rows[0] = %q, want %q", got, want)
	}
	if watch.sortedDirty {
		t.Fatalf("expected cached sorted rows")
	}

	rows2 := watch.listRows()
	if got, want := len(rows2), 2; got != want {
		t.Fatalf("rows2 len = %d, want %d", got, want)
	}
}
