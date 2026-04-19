package informer

import (
	"context"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/elijahrou/surfsk8s/internal/state"
)

const (
	resyncPeriod    = 5 * time.Minute
	eventBufferSize = 4096
)

type eventOp uint8

const (
	eventOpUpsert eventOp = iota
	eventOpDelete
)

type eventKind uint8

const (
	eventKindPod eventKind = iota
	eventKindDeployment
	eventKindService
	eventKindNode
)

type resourceEvent struct {
	op     eventOp
	kind   eventKind
	object interface{}
}

// Watcher wraps SharedIndexInformer instances for the core vertical slice.
// Streams deltas into the unified state store.
type Watcher struct {
	clusterID    string
	factory      informers.SharedInformerFactory
	informers    []cache.SharedIndexInformer
	store        *state.Store
	events       chan resourceEvent
	stop         <-chan struct{}
	done         chan struct{}
	decodeErrors atomic.Uint64
}

func NewWatcher(clientset kubernetes.Interface, clusterID string, store *state.Store) *Watcher {
	if clientset == nil {
		panic("informer.NewWatcher: nil clientset")
	}
	if clusterID == "" {
		panic("informer.NewWatcher: empty clusterID")
	}
	if store == nil {
		panic("informer.NewWatcher: nil store")
	}

	factory := informers.NewSharedInformerFactory(clientset, resyncPeriod)
	watcher := &Watcher{
		clusterID: clusterID,
		factory:   factory,
		store:     store,
		events:    make(chan resourceEvent, eventBufferSize),
		done:      make(chan struct{}),
	}

	podInformer := factory.Core().V1().Pods().Informer()
	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod, ok := watcher.podFromObject(obj)
			if !ok {
				return
			}
			watcher.enqueue(resourceEvent{op: eventOpUpsert, kind: eventKindPod, object: pod})
		},
		UpdateFunc: func(_ interface{}, newObj interface{}) {
			pod, ok := watcher.podFromObject(newObj)
			if !ok {
				return
			}
			watcher.enqueue(resourceEvent{op: eventOpUpsert, kind: eventKindPod, object: pod})
		},
		DeleteFunc: func(obj interface{}) {
			pod, ok := watcher.deletedPodFromObject(obj)
			if !ok {
				return
			}
			watcher.enqueue(resourceEvent{op: eventOpDelete, kind: eventKindPod, object: pod})
		},
	})

	deploymentInformer := factory.Apps().V1().Deployments().Informer()
	deploymentInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			deployment, ok := watcher.deploymentFromObject(obj)
			if !ok {
				return
			}
			watcher.enqueue(resourceEvent{op: eventOpUpsert, kind: eventKindDeployment, object: deployment})
		},
		UpdateFunc: func(_ interface{}, newObj interface{}) {
			deployment, ok := watcher.deploymentFromObject(newObj)
			if !ok {
				return
			}
			watcher.enqueue(resourceEvent{op: eventOpUpsert, kind: eventKindDeployment, object: deployment})
		},
		DeleteFunc: func(obj interface{}) {
			deployment, ok := watcher.deletedDeploymentFromObject(obj)
			if !ok {
				return
			}
			watcher.enqueue(resourceEvent{op: eventOpDelete, kind: eventKindDeployment, object: deployment})
		},
	})

	serviceInformer := factory.Core().V1().Services().Informer()
	serviceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			service, ok := watcher.serviceFromObject(obj)
			if !ok {
				return
			}
			watcher.enqueue(resourceEvent{op: eventOpUpsert, kind: eventKindService, object: service})
		},
		UpdateFunc: func(_ interface{}, newObj interface{}) {
			service, ok := watcher.serviceFromObject(newObj)
			if !ok {
				return
			}
			watcher.enqueue(resourceEvent{op: eventOpUpsert, kind: eventKindService, object: service})
		},
		DeleteFunc: func(obj interface{}) {
			service, ok := watcher.deletedServiceFromObject(obj)
			if !ok {
				return
			}
			watcher.enqueue(resourceEvent{op: eventOpDelete, kind: eventKindService, object: service})
		},
	})

	nodeInformer := factory.Core().V1().Nodes().Informer()
	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			node, ok := watcher.nodeFromObject(obj)
			if !ok {
				return
			}
			watcher.enqueue(resourceEvent{op: eventOpUpsert, kind: eventKindNode, object: node})
		},
		UpdateFunc: func(_ interface{}, newObj interface{}) {
			node, ok := watcher.nodeFromObject(newObj)
			if !ok {
				return
			}
			watcher.enqueue(resourceEvent{op: eventOpUpsert, kind: eventKindNode, object: node})
		},
		DeleteFunc: func(obj interface{}) {
			node, ok := watcher.deletedNodeFromObject(obj)
			if !ok {
				return
			}
			watcher.enqueue(resourceEvent{op: eventOpDelete, kind: eventKindNode, object: node})
		},
	})

	watcher.informers = []cache.SharedIndexInformer{podInformer, deploymentInformer, serviceInformer, nodeInformer}
	return watcher
}

func (w *Watcher) Start(ctx context.Context) {
	if ctx == nil {
		panic("informer.Watcher.Start: nil context")
	}
	w.stop = ctx.Done()
	go w.runReducer(ctx)
	w.factory.Start(ctx.Done())
}

func (w *Watcher) Wait() {
	if w.done == nil {
		return
	}
	<-w.done
}

func (w *Watcher) WaitForSync(ctx context.Context) bool {
	if ctx == nil {
		panic("informer.Watcher.WaitForSync: nil context")
	}
	synced := make([]cache.InformerSynced, 0, len(w.informers))
	for _, informer := range w.informers {
		synced = append(synced, informer.HasSynced)
	}
	return cache.WaitForCacheSync(ctx.Done(), synced...)
}

func (w *Watcher) DecodeErrorCount() uint64 {
	return w.decodeErrors.Load()
}

func (w *Watcher) enqueue(event resourceEvent) {
	if w.stop == nil {
		w.events <- event
		return
	}
	select {
	case w.events <- event:
	case <-w.stop:
	}
}

func (w *Watcher) runReducer(ctx context.Context) {
	defer close(w.done)
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-w.events:
			w.apply(event)
			drain := len(w.events)
			for range drain {
				w.apply(<-w.events)
			}
		}
	}
}

func (w *Watcher) apply(event resourceEvent) {
	switch event.kind {
	case eventKindPod:
		pod := event.object.(*corev1.Pod)
		if event.op == eventOpDelete {
			w.store.DeletePod(w.clusterID, pod)
			return
		}
		w.store.UpsertPod(w.clusterID, pod)
	case eventKindDeployment:
		deployment := event.object.(*appsv1.Deployment)
		if event.op == eventOpDelete {
			w.store.DeleteDeployment(w.clusterID, deployment)
			return
		}
		w.store.UpsertDeployment(w.clusterID, deployment)
	case eventKindService:
		service := event.object.(*corev1.Service)
		if event.op == eventOpDelete {
			w.store.DeleteService(w.clusterID, service)
			return
		}
		w.store.UpsertService(w.clusterID, service)
	case eventKindNode:
		node := event.object.(*corev1.Node)
		if event.op == eventOpDelete {
			w.store.DeleteNode(w.clusterID, node)
			return
		}
		w.store.UpsertNode(w.clusterID, node)
	default:
		panic("informer.Watcher.apply: unknown event kind")
	}
}

func (w *Watcher) podFromObject(obj interface{}) (*corev1.Pod, bool) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		w.recordDecodeError("pod", obj)
		return nil, false
	}
	return pod, true
}

func (w *Watcher) deletedPodFromObject(obj interface{}) (*corev1.Pod, bool) {
	if pod, ok := obj.(*corev1.Pod); ok {
		return pod, true
	}
	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		w.recordDecodeError("pod tombstone", obj)
		return nil, false
	}
	pod, ok := tombstone.Obj.(*corev1.Pod)
	if !ok {
		w.recordDecodeError("pod tombstone payload", tombstone.Obj)
		return nil, false
	}
	return pod, true
}

func (w *Watcher) deploymentFromObject(obj interface{}) (*appsv1.Deployment, bool) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		w.recordDecodeError("deployment", obj)
		return nil, false
	}
	return deployment, true
}

func (w *Watcher) deletedDeploymentFromObject(obj interface{}) (*appsv1.Deployment, bool) {
	if deployment, ok := obj.(*appsv1.Deployment); ok {
		return deployment, true
	}
	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		w.recordDecodeError("deployment tombstone", obj)
		return nil, false
	}
	deployment, ok := tombstone.Obj.(*appsv1.Deployment)
	if !ok {
		w.recordDecodeError("deployment tombstone payload", tombstone.Obj)
		return nil, false
	}
	return deployment, true
}

func (w *Watcher) serviceFromObject(obj interface{}) (*corev1.Service, bool) {
	service, ok := obj.(*corev1.Service)
	if !ok {
		w.recordDecodeError("service", obj)
		return nil, false
	}
	return service, true
}

func (w *Watcher) deletedServiceFromObject(obj interface{}) (*corev1.Service, bool) {
	if service, ok := obj.(*corev1.Service); ok {
		return service, true
	}
	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		w.recordDecodeError("service tombstone", obj)
		return nil, false
	}
	service, ok := tombstone.Obj.(*corev1.Service)
	if !ok {
		w.recordDecodeError("service tombstone payload", tombstone.Obj)
		return nil, false
	}
	return service, true
}

func (w *Watcher) nodeFromObject(obj interface{}) (*corev1.Node, bool) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		w.recordDecodeError("node", obj)
		return nil, false
	}
	return node, true
}

func (w *Watcher) deletedNodeFromObject(obj interface{}) (*corev1.Node, bool) {
	if node, ok := obj.(*corev1.Node); ok {
		return node, true
	}
	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		w.recordDecodeError("node tombstone", obj)
		return nil, false
	}
	node, ok := tombstone.Obj.(*corev1.Node)
	if !ok {
		w.recordDecodeError("node tombstone payload", tombstone.Obj)
		return nil, false
	}
	return node, true
}

func (w *Watcher) recordDecodeError(_ string, _ interface{}) {
	w.decodeErrors.Add(1)
}
