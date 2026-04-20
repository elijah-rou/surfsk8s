package cluster

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func BenchmarkBuildGenericResourceRows10000(b *testing.B) {
	list := &unstructured.UnstructuredList{Items: make([]unstructured.Unstructured, 0, 10000)}
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	for idx := 0; idx < 10000; idx++ {
		list.Items = append(list.Items, *newBenchmarkGenericObject(idx, now))
	}
	b.ResetTimer()
	for idx := 0; idx < b.N; idx++ {
		rows := buildGenericResourceRows("dev", list, nil)
		if len(rows) != 10000 {
			b.Fatalf("rows = %d, want 10000", len(rows))
		}
	}
}

func BenchmarkFilterAndSortGenericRows10000(b *testing.B) {
	rows := make([]GenericResourceRow, 0, 10000)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	for idx := 0; idx < 10000; idx++ {
		rows = append(rows, buildGenericResourceRow("dev", newBenchmarkGenericObject(idx, now), nil))
	}
	query := "revision-09"
	b.ResetTimer()
	for idx := 0; idx < b.N; idx++ {
		filtered := make([]GenericResourceRow, 0, len(rows))
		for _, row := range rows {
			if strings.Contains(row.SearchText(), query) {
				filtered = append(filtered, row)
			}
		}
		sort.SliceStable(filtered, func(i int, j int) bool {
			if filtered[i].Namespace != filtered[j].Namespace {
				return filtered[i].Namespace < filtered[j].Namespace
			}
			return filtered[i].Name < filtered[j].Name
		})
		if len(filtered) == 0 {
			b.Fatalf("expected filtered rows")
		}
	}
}

func BenchmarkBuildGenericResourceRowsWithPrinterColumns10000(b *testing.B) {
	list := &unstructured.UnstructuredList{Items: make([]unstructured.Unstructured, 0, 10000)}
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	for idx := 0; idx < 10000; idx++ {
		list.Items = append(list.Items, *newBenchmarkGenericObject(idx, now))
	}
	columns := []PrinterColumn{
		{Name: "URL", JSONPath: ".status.url"},
		{Name: "LATESTCREATED", JSONPath: ".status.latestCreatedRevisionName"},
		{Name: "LATESTREADY", JSONPath: ".status.latestReadyRevisionName"},
		{Name: "READY", JSONPath: ".status.conditions[?(@.type==\"Ready\")].status"},
		{Name: "REASON", JSONPath: ".status.conditions[?(@.type==\"Ready\")].reason"},
	}
	b.ResetTimer()
	for idx := 0; idx < b.N; idx++ {
		rows := buildGenericResourceRows("dev", list, columns)
		if len(rows) != 10000 {
			b.Fatalf("rows = %d, want 10000", len(rows))
		}
	}
}

func newBenchmarkGenericObject(idx int, now time.Time) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "serving.knative.dev/v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":              fmt.Sprintf("revision-%05d", idx),
			"namespace":         fmt.Sprintf("ns-%02d", idx%50),
			"creationTimestamp": now.Add(-time.Duration(idx) * time.Second).Format(time.RFC3339),
		},
		"status": map[string]interface{}{
			"url":                       fmt.Sprintf("https://revision-%05d.example.com", idx),
			"latestCreatedRevisionName": fmt.Sprintf("revision-%05d", idx),
			"latestReadyRevisionName":   fmt.Sprintf("revision-%05d", idx),
			"phase":                     "Active",
			"conditions": []interface{}{
				map[string]interface{}{"type": "Ready", "status": "True", "reason": "Ready"},
			},
		},
	}}
}
