package cluster

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

const maxActiveGenericWatchesPerCluster = 8

type genericResourceWatch struct {
	clusterName string
	resource    ResourceKind
	informer    cache.SharedIndexInformer
	cancel      context.CancelFunc

	synced     atomic.Bool
	lastAccess atomic.Int64

	mu          sync.RWMutex
	objects     map[GenericResourceKey]*unstructured.Unstructured
	rows        map[GenericResourceKey]GenericResourceRow
	sortedRows  []GenericResourceRow
	sortedDirty bool
}

func newGenericResourceWatch(clusterName string, resource ResourceKind, cancel context.CancelFunc, informer cache.SharedIndexInformer) *genericResourceWatch {
	if clusterName == "" {
		panic("cluster.newGenericResourceWatch: empty clusterName")
	}
	if resource.ID == "" {
		panic("cluster.newGenericResourceWatch: empty resource ID")
	}
	if cancel == nil {
		panic("cluster.newGenericResourceWatch: nil cancel")
	}
	if informer == nil {
		panic("cluster.newGenericResourceWatch: nil informer")
	}
	watch := &genericResourceWatch{
		clusterName: clusterName,
		resource:    resource,
		informer:    informer,
		cancel:      cancel,
		objects:     make(map[GenericResourceKey]*unstructured.Unstructured, 128),
		rows:        make(map[GenericResourceKey]GenericResourceRow, 128),
		sortedDirty: true,
	}
	watch.touch(time.Now())
	return watch
}

func (w *genericResourceWatch) touch(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	w.lastAccess.Store(now.UnixNano())
}

func (w *genericResourceWatch) lastAccessAt() time.Time {
	nanos := w.lastAccess.Load()
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos)
}

func (w *genericResourceWatch) upsertObject(object *unstructured.Unstructured) GenericResourceRow {
	if object == nil {
		panic("cluster.genericResourceWatch.upsertObject: nil object")
	}
	copyObject := object.DeepCopy()
	key := GenericResourceKey{Cluster: w.clusterName, Namespace: copyObject.GetNamespace(), Name: copyObject.GetName()}
	row := buildGenericResourceRowCompiled(w.clusterName, copyObject)
	w.mu.Lock()
	w.objects[key] = copyObject
	w.rows[key] = row
	w.sortedDirty = true
	w.mu.Unlock()
	return row
}

func (w *genericResourceWatch) deleteObject(object *unstructured.Unstructured) GenericResourceKey {
	if object == nil {
		panic("cluster.genericResourceWatch.deleteObject: nil object")
	}
	key := GenericResourceKey{Cluster: w.clusterName, Namespace: object.GetNamespace(), Name: object.GetName()}
	w.mu.Lock()
	delete(w.objects, key)
	delete(w.rows, key)
	w.sortedDirty = true
	w.mu.Unlock()
	return key
}

func (w *genericResourceWatch) listRows() []GenericResourceRow {
	w.mu.Lock()
	if w.sortedDirty {
		w.sortedRows = w.sortedRows[:0]
		for _, row := range w.rows {
			w.sortedRows = append(w.sortedRows, row)
		}
		sortGenericResourceRows(w.sortedRows)
		w.sortedDirty = false
		result := append([]GenericResourceRow(nil), w.sortedRows...)
		w.mu.Unlock()
		return result
	}
	result := append([]GenericResourceRow(nil), w.sortedRows...)
	w.mu.Unlock()
	return result
}

// forEachRow visits cached rows in sort order without allocating a full copy slice.
// If visit returns false, iteration stops. Returns false iff visit stopped early.
func (w *genericResourceWatch) forEachRow(visit func(GenericResourceRow) bool) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sortedDirty {
		w.sortedRows = w.sortedRows[:0]
		for _, row := range w.rows {
			w.sortedRows = append(w.sortedRows, row)
		}
		sortGenericResourceRows(w.sortedRows)
		w.sortedDirty = false
	}
	for _, row := range w.sortedRows {
		if !visit(row) {
			return false
		}
	}
	return true
}

func (w *genericResourceWatch) objectResourceVersion(key GenericResourceKey) (string, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	object, ok := w.objects[key]
	if !ok || object == nil {
		return "", false
	}
	return object.GetResourceVersion(), true
}

func (w *genericResourceWatch) details(key GenericResourceKey, now time.Time) (GenericResourceDetails, bool, error) {
	w.mu.RLock()
	object, ok := w.objects[key]
	if ok {
		object = object.DeepCopy()
	}
	w.mu.RUnlock()
	if !ok {
		return GenericResourceDetails{}, false, nil
	}
	details, err := buildGenericResourceDetails(w.clusterName, object, now, w.resource.PrinterColumns)
	if err != nil {
		return GenericResourceDetails{}, false, err
	}
	return details, true, nil
}

func startGenericResourceWatch(ctx context.Context, manager *Manager, conn *ClusterConn, resource ResourceKind) *genericResourceWatch {
	if ctx == nil {
		panic("cluster.startGenericResourceWatch: nil context")
	}
	if manager == nil {
		panic("cluster.startGenericResourceWatch: nil manager")
	}
	if conn == nil {
		panic("cluster.startGenericResourceWatch: nil conn")
	}
	if conn.Dynamic == nil {
		panic("cluster.startGenericResourceWatch: nil dynamic client")
	}

	watchCtx, cancel := context.WithCancel(ctx)
	informer := dynamicinformer.NewFilteredDynamicInformer(
		conn.Dynamic,
		resourceGVR(resource),
		metav1.NamespaceAll,
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		nil,
	).Informer()
	watch := newGenericResourceWatch(conn.Name, resource, cancel, informer)
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			object, ok := genericObjectFromEvent(obj)
			if !ok {
				return
			}
			row := watch.upsertObject(object)
			manager.recordGenericResourceUpsert(resource.ID, row)
		},
		UpdateFunc: func(_, newObj interface{}) {
			object, ok := genericObjectFromEvent(newObj)
			if !ok {
				return
			}
			row := watch.upsertObject(object)
			manager.recordGenericResourceUpsert(resource.ID, row)
		},
		DeleteFunc: func(obj interface{}) {
			object, ok := genericObjectFromEvent(obj)
			if !ok {
				return
			}
			key := watch.deleteObject(object)
			manager.recordGenericResourceDelete(resource.ID, key)
		},
	})
	go informer.Run(watchCtx.Done())
	go func() {
		if cache.WaitForCacheSync(watchCtx.Done(), informer.HasSynced) {
			watch.synced.Store(true)
		}
	}()
	return watch
}

func genericObjectFromEvent(obj interface{}) (*unstructured.Unstructured, bool) {
	switch typed := obj.(type) {
	case *unstructured.Unstructured:
		return typed, true
	case cache.DeletedFinalStateUnknown:
		object, ok := typed.Obj.(*unstructured.Unstructured)
		return object, ok
	case *cache.DeletedFinalStateUnknown:
		object, ok := typed.Obj.(*unstructured.Unstructured)
		return object, ok
	default:
		return nil, false
	}
}

func sortGenericResourceRows(rows []GenericResourceRow) {
	sort.SliceStable(rows, func(i int, j int) bool {
		left := rows[i]
		right := rows[j]
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.Cluster < right.Cluster
	})
}
