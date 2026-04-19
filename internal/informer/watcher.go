package informer

// Watcher wraps SharedIndexInformer for a single resource type.
// Streams deltas (add/update/delete) into the unified state store.
type Watcher struct{}
