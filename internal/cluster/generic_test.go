package cluster

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestBuildCatalogEnrichesKnownResourceVersion(t *testing.T) {
	catalog := buildCatalog([]discoveredResource{{APIGroup: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true}})
	for _, group := range catalog {
		for _, resource := range group.Resources {
			if resource.APIGroup == "apps" && resource.Resource == "deployments" {
				if got, want := resource.Version, "v1"; got != want {
					t.Fatalf("version = %q, want %q", got, want)
				}
				return
			}
		}
	}
	t.Fatalf("deployment resource not found")
}

func TestBuildGenericResourceRowUsesPhaseAndSearchText(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "serving.knative.dev/v1",
		"kind":       "Revision",
		"metadata": map[string]interface{}{
			"name":              "api-00001",
			"namespace":         "serving",
			"creationTimestamp": metav1.NewTime(time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)).Format(time.RFC3339),
		},
		"status": map[string]interface{}{"phase": "Active"},
	}}
	row := buildGenericResourceRow("dev", object, nil)
	if got, want := row.Status, "Active"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if got, want := row.SearchText(), "dev serving api-00001 Active"; got != want {
		t.Fatalf("searchText = %q, want %q", got, want)
	}
}

func TestBuildGenericResourceRowUsesReplicaReadiness(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":              "frontend",
			"namespace":         "default",
			"creationTimestamp": metav1.NewTime(time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)).Format(time.RFC3339),
		},
		"spec":   map[string]interface{}{"replicas": int64(3)},
		"status": map[string]interface{}{"readyReplicas": int64(2)},
	}}
	row := buildGenericResourceRow("dev", object, nil)
	if got, want := row.Ready, "2/3"; got != want {
		t.Fatalf("ready = %q, want %q", got, want)
	}
}

func TestStatusFromUnstructuredUsesReadyCondition(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"status": map[string]interface{}{
			"conditions": []interface{}{
				map[string]interface{}{"type": "Ready", "status": "True", "reason": "Ready"},
			},
		},
	}}
	ready, status := readinessAndStatusFromUnstructured(object)
	if got, want := ready, "True"; got != want {
		t.Fatalf("ready = %q, want %q", got, want)
	}
	if got, want := status, "Ready"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestEvaluatePrinterColumns(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "serving.knative.dev/v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":      "api",
			"namespace": "serving",
		},
		"status": map[string]interface{}{
			"latestCreatedRevisionName": "api-00001",
			"conditions": []interface{}{
				map[string]interface{}{"type": "Ready", "status": "True", "reason": "Ready"},
			},
		},
	}}
	values := EvaluatePrinterColumns(object, []PrinterColumn{{Name: "LATESTCREATED", JSONPath: ".status.latestCreatedRevisionName"}, {Name: "READY", JSONPath: ".status.conditions[?(@.type==\"Ready\")].status"}})
	if got, want := values[0], "api-00001"; got != want {
		t.Fatalf("printer[0] = %q, want %q", got, want)
	}
	if got, want := values[1], "True"; got != want {
		t.Fatalf("printer[1] = %q, want %q", got, want)
	}
}

func TestVisiblePrinterColumnsPrefersPriorityZero(t *testing.T) {
	columns := visiblePrinterColumns([]PrinterColumn{{Name: "A", Priority: 1}, {Name: "B", Priority: 0}, {Name: "C", Priority: 0}})
	if got, want := len(columns), 2; got != want {
		t.Fatalf("columns = %d, want %d", got, want)
	}
	if got, want := columns[0].Name, "B"; got != want {
		t.Fatalf("columns[0] = %q, want %q", got, want)
	}
}
