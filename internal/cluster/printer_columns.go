package cluster

import (
	"context"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	jsonpath "k8s.io/client-go/util/jsonpath"
)

const maxVisiblePrinterColumns = 5

type CompiledPrinterColumn struct {
	column PrinterColumn
	path   *jsonpath.JSONPath
}

func fetchCustomResourcePrinterColumns(ctx context.Context, client dynamic.Interface) map[string][]PrinterColumn {
	if ctx == nil {
		panic("cluster.fetchCustomResourcePrinterColumns: nil context")
	}
	if client == nil {
		panic("cluster.fetchCustomResourcePrinterColumns: nil client")
	}

	list, err := client.Resource(schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	columnsByVersionKey := make(map[string][]PrinterColumn, len(list.Items))
	for idx := range list.Items {
		object := &list.Items[idx]
		group, ok, err := unstructured.NestedString(object.Object, "spec", "group")
		if err != nil || !ok || group == "" {
			continue
		}
		plural, ok, err := unstructured.NestedString(object.Object, "spec", "names", "plural")
		if err != nil || !ok || plural == "" {
			continue
		}
		versions, ok, err := unstructured.NestedSlice(object.Object, "spec", "versions")
		if err != nil || !ok {
			continue
		}
		for _, rawVersion := range versions {
			version, columns, ok := printerColumnsForVersion(rawVersion)
			if !ok || len(columns) == 0 {
				continue
			}
			columnsByVersionKey[group+"/"+version+"/"+plural] = columns
		}
	}
	return columnsByVersionKey
}

func printerColumnsForVersion(rawVersion interface{}) (string, []PrinterColumn, bool) {
	versionMap, ok := rawVersion.(map[string]interface{})
	if !ok {
		return "", nil, false
	}
	versionName, _ := versionMap["name"].(string)
	if versionName == "" {
		return "", nil, false
	}
	rawColumns, ok := versionMap["additionalPrinterColumns"].([]interface{})
	if !ok || len(rawColumns) == 0 {
		return versionName, nil, false
	}
	columns := make([]PrinterColumn, 0, len(rawColumns))
	for _, rawColumn := range rawColumns {
		columnMap, ok := rawColumn.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := columnMap["name"].(string)
		jsonPathValue, _ := columnMap["jsonPath"].(string)
		if name == "" || jsonPathValue == "" {
			continue
		}
		priority := int32(0)
		switch value := columnMap["priority"].(type) {
		case int32:
			priority = value
		case int64:
			priority = int32(value)
		case float64:
			priority = int32(value)
		}
		typeName, _ := columnMap["type"].(string)
		columns = append(columns, PrinterColumn{
			Name:     strings.ToUpper(strings.TrimSpace(name)),
			JSONPath: strings.TrimSpace(jsonPathValue),
			Type:     strings.TrimSpace(typeName),
			Priority: priority,
		})
	}
	columns = visiblePrinterColumns(columns)
	if len(columns) == 0 {
		return versionName, nil, false
	}
	return versionName, columns, true
}

func visiblePrinterColumns(columns []PrinterColumn) []PrinterColumn {
	if len(columns) == 0 {
		return nil
	}
	sorted := append([]PrinterColumn(nil), columns...)
	sort.SliceStable(sorted, func(i int, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority < sorted[j].Priority
		}
		return sorted[i].Name < sorted[j].Name
	})
	visible := make([]PrinterColumn, 0, min(len(sorted), maxVisiblePrinterColumns))
	for _, column := range sorted {
		if column.Priority > 0 {
			continue
		}
		visible = append(visible, column)
		if len(visible) >= maxVisiblePrinterColumns {
			return visible
		}
	}
	if len(visible) != 0 {
		return visible
	}
	for _, column := range sorted {
		visible = append(visible, column)
		if len(visible) >= maxVisiblePrinterColumns {
			break
		}
	}
	return visible
}

func CompilePrinterColumns(columns []PrinterColumn) []CompiledPrinterColumn {
	if len(columns) == 0 {
		return nil
	}
	compiled := make([]CompiledPrinterColumn, 0, len(columns))
	for _, column := range columns {
		if column.JSONPath == "" {
			continue
		}
		parser := jsonpath.New(column.Name)
		parser.AllowMissingKeys(true)
		if err := parser.Parse(normalizePrinterJSONPath(column.JSONPath)); err != nil {
			continue
		}
		compiled = append(compiled, CompiledPrinterColumn{column: column, path: parser})
	}
	return compiled
}

func EvaluatePrinterColumns(object *unstructured.Unstructured, columns []PrinterColumn) []string {
	if object == nil {
		panic("cluster.evaluatePrinterColumns: nil object")
	}
	return EvaluateCompiledPrinterColumns(object, CompilePrinterColumns(columns))
}

func EvaluateCompiledPrinterColumns(object *unstructured.Unstructured, columns []CompiledPrinterColumn) []string {
	if object == nil {
		panic("cluster.evaluateCompiledPrinterColumns: nil object")
	}
	if len(columns) == 0 {
		return nil
	}
	values := make([]string, 0, len(columns))
	for _, column := range columns {
		var builder strings.Builder
		if err := column.path.Execute(&builder, object.UnstructuredContent()); err != nil {
			values = append(values, "")
			continue
		}
		values = append(values, strings.TrimSpace(builder.String()))
	}
	return values
}

func normalizePrinterJSONPath(path string) string {
	if !strings.Contains(path, "{") {
		return "{" + path + "}"
	}
	return path
}

func printerColumnsKey(apiGroup string, version string, resource string) string {
	return apiGroup + "/" + version + "/" + resource
}
