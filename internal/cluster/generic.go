package cluster

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

type GenericResourceKey struct {
	Cluster   string
	Namespace string
	Name      string
}

type GenericResourceRow struct {
	Key             GenericResourceKey
	Cluster         string
	Namespace       string
	Name            string
	Ready           string
	Status          string
	PrinterValues   []string
	Object          *unstructured.Unstructured
	Age             string
	ResourceVersion string

	createdAt time.Time
}

func (r GenericResourceRow) WithAge(now time.Time) GenericResourceRow {
	r.Age = formatAge(now.Sub(r.createdAt))
	return r
}

func (r GenericResourceRow) SearchText() string {
	return buildGenericSearchText(r.Cluster, r.Namespace, r.Name, r.Ready, r.Status, r.PrinterValues)
}

func (r GenericResourceRow) CreatedAt() time.Time {
	return r.createdAt
}

type GenericResourceDetails struct {
	Row    GenericResourceRow
	Object *unstructured.Unstructured
	YAML   string
}

func resourceGVR(resource ResourceKind) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: resource.APIGroup, Version: resource.Version, Resource: resource.Resource}
}

func buildGenericResourceRows(clusterName string, list *unstructured.UnstructuredList, _ []PrinterColumn) []GenericResourceRow {
	if list == nil {
		panic("cluster.buildGenericResourceRows: nil list")
	}
	rows := make([]GenericResourceRow, 0, len(list.Items))
	for idx := range list.Items {
		rows = append(rows, buildGenericResourceRow(clusterName, &list.Items[idx], nil))
	}
	sortGenericResourceRows(rows)
	return rows
}

func buildGenericResourceRow(clusterName string, object *unstructured.Unstructured, _ []PrinterColumn) GenericResourceRow {
	return buildGenericResourceRowCompiled(clusterName, object)
}

func buildGenericResourceRowCompiled(clusterName string, object *unstructured.Unstructured) GenericResourceRow {
	if object == nil {
		panic("cluster.buildGenericResourceRow: nil object")
	}
	ready, status := readinessAndStatusFromUnstructured(object)
	return GenericResourceRow{
		Key:             GenericResourceKey{Cluster: clusterName, Namespace: object.GetNamespace(), Name: object.GetName()},
		Cluster:         clusterName,
		Namespace:       object.GetNamespace(),
		Name:            object.GetName(),
		Ready:           ready,
		Status:          status,
		Object:          object,
		ResourceVersion: object.GetResourceVersion(),
		createdAt:       object.GetCreationTimestamp().Time,
	}
}

func buildGenericResourceDetails(clusterName string, object *unstructured.Unstructured, now time.Time, printerColumns []PrinterColumn) (GenericResourceDetails, error) {
	if object == nil {
		return GenericResourceDetails{}, fmt.Errorf("resource disappeared")
	}
	content, err := yaml.Marshal(object.Object)
	if err != nil {
		return GenericResourceDetails{}, fmt.Errorf("marshal resource yaml: %w", err)
	}
	row := buildGenericResourceRowCompiled(clusterName, object)
	row.PrinterValues = EvaluatePrinterColumns(object, printerColumns)
	return GenericResourceDetails{
		Row:    row.WithAge(now),
		Object: object.DeepCopy(),
		YAML:   strings.TrimSpace(string(content)),
	}, nil
}

func readinessAndStatusFromUnstructured(object *unstructured.Unstructured) (string, string) {
	if object == nil {
		panic("cluster.readinessAndStatusFromUnstructured: nil object")
	}
	if ready, ok := replicaReadinessFromUnstructured(object); ok {
		return ready, statusSummaryFromUnstructured(object)
	}

	conditions, ok, err := unstructured.NestedSlice(object.Object, "status", "conditions")
	if err == nil && ok {
		ready := ""
		fallbackStatus := ""
		for _, rawCondition := range conditions {
			condition, ok := rawCondition.(map[string]interface{})
			if !ok {
				continue
			}
			conditionType, _ := condition["type"].(string)
			conditionStatus, _ := condition["status"].(string)
			reason, _ := condition["reason"].(string)
			message, _ := condition["message"].(string)
			if fallbackStatus == "" {
				fallbackStatus = firstNonEmpty(reason, message)
			}
			if strings.EqualFold(conditionType, "Ready") || strings.EqualFold(conditionType, "Succeeded") {
				ready = conditionStatus
				status := firstNonEmpty(reason, message, conditionType)
				return ready, status
			}
		}
		if fallbackStatus != "" {
			return "", fallbackStatus
		}
	}
	return "", statusSummaryFromUnstructured(object)
}

func buildGenericSearchText(clusterName string, namespace string, name string, ready string, status string, printerValues []string) string {
	var builder strings.Builder
	appendPart := func(part string) {
		part = strings.TrimSpace(part)
		if part == "" {
			return
		}
		if builder.Len() != 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(part)
	}
	appendPart(clusterName)
	appendPart(namespace)
	appendPart(name)
	appendPart(ready)
	appendPart(status)
	for _, value := range printerValues {
		appendPart(value)
	}
	return builder.String()
}

func statusFromUnstructured(object *unstructured.Unstructured) string {
	_, status := readinessAndStatusFromUnstructured(object)
	return status
}

func statusSummaryFromUnstructured(object *unstructured.Unstructured) string {
	if object == nil {
		panic("cluster.statusSummaryFromUnstructured: nil object")
	}
	if phase, ok, err := unstructured.NestedString(object.Object, "status", "phase"); err == nil && ok && phase != "" {
		return phase
	}
	if reason, ok, err := unstructured.NestedString(object.Object, "status", "reason"); err == nil && ok && reason != "" {
		return reason
	}
	if message, ok, err := unstructured.NestedString(object.Object, "status", "message"); err == nil && ok && message != "" {
		return message
	}
	return ""
}

func replicaReadinessFromUnstructured(object *unstructured.Unstructured) (string, bool) {
	if object == nil {
		panic("cluster.replicaReadinessFromUnstructured: nil object")
	}
	pairs := [][2]string{
		{"readyReplicas", "replicas"},
		{"availableReplicas", "replicas"},
		{"currentReplicas", "replicas"},
		{"numberReady", "desiredNumberScheduled"},
		{"updatedNumberScheduled", "desiredNumberScheduled"},
	}
	for _, pair := range pairs {
		ready, readyOK, err := unstructured.NestedInt64(object.Object, "status", pair[0])
		if err != nil || !readyOK {
			continue
		}
		total, totalOK, err := unstructured.NestedInt64(object.Object, "status", pair[1])
		if err != nil || !totalOK {
			if total, totalOK, err = unstructured.NestedInt64(object.Object, "spec", pair[1]); err != nil || !totalOK {
				continue
			}
		}
		return fmt.Sprintf("%d/%d", ready, total), true
	}
	return "", false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func formatAge(age time.Duration) string {
	if age < 0 {
		return "0s"
	}
	if age < time.Minute {
		return fmt.Sprintf("%ds", int(age.Seconds()))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	if age < 24*time.Hour {
		return fmt.Sprintf("%dh", int(age.Hours()))
	}
	return fmt.Sprintf("%dd", int(age.Hours()/24))
}

func genericNamespaces(rows []GenericResourceRow) []string {
	namespaces := make([]string, 0, 16)
	seen := make(map[string]struct{}, 16)
	for _, row := range rows {
		if row.Namespace == "" {
			continue
		}
		if _, ok := seen[row.Namespace]; ok {
			continue
		}
		seen[row.Namespace] = struct{}{}
		namespaces = append(namespaces, row.Namespace)
	}
	sort.Strings(namespaces)
	return namespaces
}
