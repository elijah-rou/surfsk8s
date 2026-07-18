package cluster

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type genericFixtureRegistry struct {
	mu         sync.RWMutex
	byResource map[string]map[GenericResourceKey]*unstructured.Unstructured
}

var genericFixtures sync.Map

func genericFixtureForManager(m *Manager) *genericFixtureRegistry {
	if m == nil {
		panic("cluster.genericFixtureForManager: nil manager")
	}
	if existing, ok := genericFixtures.Load(m); ok {
		return existing.(*genericFixtureRegistry)
	}
	registry := &genericFixtureRegistry{byResource: make(map[string]map[GenericResourceKey]*unstructured.Unstructured, 4)}
	actual, _ := genericFixtures.LoadOrStore(m, registry)
	return actual.(*genericFixtureRegistry)
}

func (m *Manager) genericFixtureObject(resourceID string, key GenericResourceKey) (*unstructured.Unstructured, bool) {
	if m == nil || resourceID == "" {
		return nil, false
	}
	registryAny, ok := genericFixtures.Load(m)
	if !ok {
		return nil, false
	}
	registry := registryAny.(*genericFixtureRegistry)
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	objects := registry.byResource[resourceID]
	if objects == nil {
		return nil, false
	}
	object, ok := objects[key]
	if !ok || object == nil {
		return nil, false
	}
	return object.DeepCopy(), true
}

// SetGenericResourceFixtureForTest registers a synthetic generic object for benchmarks/tests.
func (m *Manager) SetGenericResourceFixtureForTest(resourceID string, clusterName string, object *unstructured.Unstructured) {
	if m == nil {
		panic("cluster.Manager.SetGenericResourceFixtureForTest: nil manager")
	}
	if resourceID == "" {
		panic("cluster.Manager.SetGenericResourceFixtureForTest: empty resourceID")
	}
	if clusterName == "" {
		panic("cluster.Manager.SetGenericResourceFixtureForTest: empty clusterName")
	}
	if object == nil {
		panic("cluster.Manager.SetGenericResourceFixtureForTest: nil object")
	}
	key := GenericResourceKey{Cluster: clusterName, Namespace: object.GetNamespace(), Name: object.GetName()}
	registry := genericFixtureForManager(m)
	registry.mu.Lock()
	objects := registry.byResource[resourceID]
	if objects == nil {
		objects = make(map[GenericResourceKey]*unstructured.Unstructured, 16)
		registry.byResource[resourceID] = objects
	}
	objects[key] = object.DeepCopy()
	registry.mu.Unlock()
	m.recordGenericResourceUpsert(resourceID, buildGenericResourceRowCompiled(clusterName, object.DeepCopy()))
}

// BumpVersionForTest increments the manager catalog/global version.
// Used by internal benchmarks to simulate unrelated manager churn.
func (m *Manager) BumpVersionForTest() {
	if m == nil {
		panic("cluster.Manager.BumpVersionForTest: nil manager")
	}
	m.version.Add(1)
}

// BumpGenericResourceVersionForTest increments the version for one generic resource ID.
func (m *Manager) BumpGenericResourceVersionForTest(resourceID string) {
	if m == nil {
		panic("cluster.Manager.BumpGenericResourceVersionForTest: nil manager")
	}
	m.bumpGenericResourceVersion(resourceID)
}

// GenericResourceObjectVersionForTest returns the fixture object's resourceVersion when present.
func (m *Manager) genericFixtureRows(resourceID string) ([]GenericResourceRow, bool) {
	if m == nil || resourceID == "" {
		return nil, false
	}
	registryAny, ok := genericFixtures.Load(m)
	if !ok {
		return nil, false
	}
	registry := registryAny.(*genericFixtureRegistry)
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	objects := registry.byResource[resourceID]
	if len(objects) == 0 {
		return nil, false
	}
	rows := make([]GenericResourceRow, 0, len(objects))
	for key, object := range objects {
		if object == nil {
			continue
		}
		rows = append(rows, buildGenericResourceRowCompiled(key.Cluster, object.DeepCopy()))
	}
	sortGenericResourceRows(rows)
	return rows, true
}

func (m *Manager) GenericResourceObjectVersionForTest(resourceID string, key GenericResourceKey) (string, bool) {
	object, ok := m.genericFixtureObject(resourceID, key)
	if !ok {
		return "", false
	}
	return object.GetResourceVersion(), true
}

// GenericResourceDetailsForTest builds details from a synthetic object fixture.
func (m *Manager) GenericResourceDetailsForTest(resource ResourceKind, key GenericResourceKey, now time.Time) (GenericResourceDetails, bool, error) {
	object, ok := m.genericFixtureObject(resource.ID, key)
	if !ok {
		return GenericResourceDetails{}, false, nil
	}
	details, err := buildGenericResourceDetails(key.Cluster, object, now, resource.PrinterColumns)
	if err != nil {
		return GenericResourceDetails{}, false, err
	}
	return details, true, nil
}

// SetDiscoveredResourcesForTest seeds discovery results for Catalog()/resolveResourceLocked.
func (m *Manager) SetDiscoveredResourcesForTest(clusterName string, resources []ResourceKind) {
	if m == nil {
		panic("cluster.Manager.SetDiscoveredResourcesForTest: nil manager")
	}
	if clusterName == "" {
		panic("cluster.Manager.SetDiscoveredResourcesForTest: empty clusterName")
	}
	converted := make([]discoveredResource, 0, len(resources))
	for _, resource := range resources {
		converted = append(converted, discoveredResource{
			APIGroup:       resource.APIGroup,
			Version:        resource.Version,
			Resource:       resource.Resource,
			Kind:           resource.Kind,
			Namespaced:     resource.Namespaced,
			PrinterColumns: append([]PrinterColumn(nil), resource.PrinterColumns...),
		})
	}
	m.mu.Lock()
	if m.resources == nil {
		m.resources = make(map[string][]discoveredResource, 1)
	}
	m.resources[clusterName] = converted
	m.insertConnOrderLocked(clusterName)
	m.mu.Unlock()
	m.version.Add(1)
}
