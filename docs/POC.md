# surfsk8s POC Plan

Status: original proving document. Large parts are now implemented. For current status and next-step work, see `../README.md` and `ROADMAP.md`.

## Goal

Prove that a Go+Bubbletea TUI can display 10k+ pods across multiple clusters with sub-100ms interaction latency. If this works, everything else is feature work.

## POC Scope

### Phase 1: Single cluster, informer-backed pod table

**Target: working pod list with live updates, virtual scrolling**

1. Parse kubeconfig, connect single cluster
2. Start SharedIndexInformer for pods
3. Pipe informer events (add/update/delete) into state store
4. Render pod table with virtual scrolling (only draw visible rows)
5. Keyboard navigation (j/k/up/down, g/G for top/bottom)
6. Namespace filtering (all namespaces or selected)

**Validate:**
- Startup time with 1k, 5k, 10k pods
- Memory usage at 10k pods
- Latency between pod status change and screen update
- Frame rate during fast scrolling

### Phase 2: Filtering and search

1. `/` to enter filter mode
2. Fuzzy match on pod name, namespace, status, node
3. Filter 10k items on each keystroke
4. Clear filter with Esc

**Validate:**
- Filter latency at 10k items (target: <16ms per keystroke)
- No dropped frames during filter input

### Phase 3: Multi-cluster

1. Load all contexts from kubeconfig
2. Cluster selector (tab/number keys)
3. Per-cluster informer factories
4. Unified "all clusters" view
5. Cluster column in table
6. Statusbar: connection health per cluster

**Validate:**
- 6 clusters, 10k total pods
- One cluster going down doesn't affect others
- Context switching latency

### Phase 4: CRUD basics

1. `d` describe (read-only detail panel)
2. `l` logs (follow mode, container selector)
3. `x` exec (shell into container)
4. `ctrl-d` delete (with confirmation)
5. `s` scale deployment (input prompt)
6. `r` restart rollout

**Validate:**
- Operations target correct cluster
- Confirmation prompts for destructive actions
- Exec works across cluster contexts

## Non-Goals for POC

- Custom resource definitions
- Plugin system
- Config file / themes
- Helm integration
- Multi-window / split views
- YAML editing

## Key Technical Decisions

### Informer over polling

k9s polls every tick. At 10k pods, each poll = full list API call = seconds of latency + API server load.

SharedIndexInformer maintains an in-memory cache synced via watch stream. After initial list, only deltas flow. CPU and network usage stays constant regardless of pod count.

```go
factory := informers.NewSharedInformerFactory(clientset, 0)
podInformer := factory.Core().V1().Pods().Informer()
podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
    AddFunc:    func(obj interface{}) { store.Add(clusterID, obj) },
    UpdateFunc: func(old, new interface{}) { store.Update(clusterID, new) },
    DeleteFunc: func(obj interface{}) { store.Delete(clusterID, obj) },
})
factory.Start(ctx.Done())
```

### Virtual scrolling

Never render what you can't see. Table tracks viewport offset and height. Only rows `[offset, offset+height)` get rendered. Scroll position updates on navigation. 10k items = 10k entries in state store, 40-50 lipgloss render calls per frame.

### State store design

```go
type Store struct {
    mu       sync.RWMutex
    pods     map[Key]*PodInfo    // Key = cluster/namespace/name
    index    map[string][]*PodInfo // secondary indexes: by namespace, node, status
    filtered []*PodInfo           // current filter result, sorted
    version  uint64               // incremented on any mutation, drives UI refresh
}
```

Bubbletea tick checks `store.version` against last rendered version. No diff = no re-render.

### Multi-cluster connection model

```go
type ClusterConn struct {
    ID        string
    Name      string
    Clientset kubernetes.Interface
    Factory   informers.SharedInformerFactory
    Healthy   atomic.Bool
    Cancel    context.CancelFunc
}
```

Each cluster gets independent context + cancel. One cluster timing out doesn't block others. Health checks via leader election or lightweight API ping.

## Success Criteria

| Metric | Target |
|--------|--------|
| Startup (10k pods, 1 cluster) | <2s |
| Startup (10k pods, 6 clusters) | <5s |
| Scroll latency | <16ms (60fps) |
| Filter latency (10k items) | <16ms per keystroke |
| Memory (10k pods) | <200MB |
| Pod status change to screen | <500ms |
| Cluster disconnect recovery | <10s reconnect |

## Execution Order

1. Phase 1 (informer + table) - prove the core perf story
2. Phase 2 (filtering) - prove interactive perf at scale
3. Phase 3 (multi-cluster) - prove the architecture
4. Phase 4 (CRUD) - prove it's usable as a daily driver

Each phase is independently demoable. If Phase 1 doesn't hit targets, stop and investigate before proceeding.
