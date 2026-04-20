# surfsk8s

Fast multi-cluster Kubernetes TUI built for real fleet scale.

`sufsk8s` targets the failure mode where existing tools get slow or unusable once you have thousands of pods, many deployments, and multiple kubeconfig contexts open at once. It keeps informer-backed state in memory, renders only the visible table window, and treats multi-cluster as the default model instead of an afterthought.

## Status

Working, daily-usable build. Current scope includes:
- startup context picker with persisted selected contexts
- grouped resource catalog with user-managed favourites
- informer-backed built-in views for pods, deployments, services, nodes
- generic discovered-resource and CRD browsing via dynamic client/informer path
- fuzzy, wildcard, exact, and structured per-column filtering
- stacked multi-column sorting
- quick resource finder, namespace picker, context scope picker
- resource details, manifest editing, exec, port-forward, scale, restart
- pod/node resource usage in details and tables
- live smoke coverage against real clusters plus fixture-based smoke tooling

See `docs/POC.md` for original goals. See `docs/ROADMAP.md` for next-step improvements and pod-specific needs.

## Why

k9s and similar tools degrade badly once object counts get large. Typical failures:
- slow startup from repeated list/poll work
- no real multi-cluster unified view
- poor handling of CRDs
- action workflows that are either missing or too brittle

surfsk8s is built around informer/event flow, bounded rendering work, and cluster-aware state from the start.

## Design Principles

- **Informers, not polling.** Shared informer streams deltas. Full list work only when needed.
- **Multi-cluster first.** Every row carries cluster identity. Unified fleet view is normal.
- **Virtual scrolling.** Render visible rows only. Large tables stay responsive.
- **Generic resource support.** Built-ins get typed views where it matters. Everything else still works generically.
- **Bounded background work.** Lazy dynamic watches, bounded generic-watch count, cached filtered/sorted slices.
- **Local-only operation.** Reads kubeconfig directly. No server, DB, or agent sidecar.

## Architecture

```text
┌─────────────────────────────────────────┐
│              Bubble Tea App             │
│  ┌─────────┐ ┌──────────┐ ┌─────────┐   │
│  │  Views  │ │ Filters  │ │ Status  │   │
│  │ typed + │ │ fuzzy /  │ │ scopes, │   │
│  │ generic │ │ column   │ │ health  │   │
│  └────┬────┘ └────┬─────┘ └────┬────┘   │
│       └────────────┼───────────┘         │
│                    │                     │
│             ┌──────┴──────┐              │
│             │ State Store │              │
│             │ indexed,    │              │
│             │ cross-cluster│             │
│             └──────┬──────┘              │
│      ┌─────────────┼─────────────┐       │
│  ┌───┴────┐  ┌─────┴────┐  ┌─────┴────┐  │
│  │typed    │  │generic    │  │actions   │  │
│  │informers│  │dyn watches│  │kubectl   │  │
│  │pods/etc │  │CRDs/GVRs  │  │editor    │  │
│  └─────────┘  └───────────┘  └──────────┘  │
└────────────────────────────────────────────┘
```

## Stack

- **Go**
- **client-go**
- **Bubble Tea**
- **Lip Gloss**

## Project Structure

```text
surfsk8s/
├── main.go
├── cmd/                    # CLI entrypoint, flags
├── internal/
│   ├── app/                # Bubble Tea model, screens, workflows
│   ├── actions/            # kubectl-backed exec/edit/port-forward/scale/restart
│   ├── cluster/            # discovery, typed clients, dynamic clients, watches
│   ├── informer/           # typed informer wrappers
│   ├── state/              # cross-cluster store, indexes, detail snapshots
│   └── ui/
│       ├── components/     # table, filter, statusbar
│       ├── theme/          # colors/styles
│       └── views/          # typed resource views
└── docs/
    ├── POC.md
    └── ROADMAP.md
```

## Run

```bash
go run .
```

Flags:

```bash
go run . -kubeconfig ~/.kube/config -context aws-dev-virginia -namespace default
```

Available flags:
- `-kubeconfig` path to kubeconfig
- `-context` initial kubeconfig context to load
- `-namespace` initial namespace scope

## Core UX

### Navigation

Common list keys:
- `j/k`, arrows: move
- `g/G`: top/bottom
- `h/l`, `H/L`: horizontal move / edge
- `enter`: open selected item
- `esc`: clear active prompt or go back
- `q`: quit

### Find, filter, sort, scope

- `/` global list filter
  - fuzzy by default
  - wildcard with `*` / `?`
  - exact-substring with `=value`
- `f` add structured column filter
- `F` manage column filters
- `o` add sort
- `O` manage stacked sorts
- `n` namespace picker
- `c` context scope picker
- `a` reset namespace to all
- `r` quick resource finder
- `R` quick resource finder from resource details

### Copy

- `y` copy selected row
- `Y` copy full filtered table as CSV with headers

### Resource actions

Pod details:
- `x` exec shell
- `p` port-forward
- `e` edit manifest

Service details:
- `p` port-forward
- `e` edit manifest

Deployment details:
- `s` scale
- `r` restart
- `e` edit manifest

Resource list row actions:
- deployments: `S` scale selected row, `R` restart selected row
- services: `P` port-forward selected row

Mutating actions use confirmation screens. Multi-container exec and multi-port port-forward use explicit picker flows.

## Resource Coverage

Typed first-class views:
- Pods
- Deployments
- Services
- Nodes

Generic browsable support:
- native discovered resources not covered by typed views
- CRDs via dynamic client path
- CRD `additionalPrinterColumns`
- generic details and manifest editing

## Resource Usage Semantics

### Pod table/details

- **CPU**: live usage from `metrics.k8s.io`
- **Memory**: live usage from `metrics.k8s.io`
- **Ephemeral**: kubelet summary when reachable
- **GPU**: allocated GPU count from pod spec requests

Table cell format:
- `<bar> <used>/<total>`
- pod total = limit, fallback request
- vertical marker inside bar = request when request < limit

### Node table/details

- **CPU**: live usage / allocatable
- **Memory**: live usage / allocatable
- **Ephemeral**: kubelet summary / allocatable when reachable
- **GPU**: allocated on scheduled pods / allocatable

Table cell format:
- `<bar> <used>/<total>`
- node total = allocatable

### Optional sources

Missing optional sources stay blank, by design.

Typical cases:
- CPU/memory blank → metrics API missing/unavailable
- ephemeral blank → kubelet summary unavailable or blocked
- GPU blank → no allocatable/allocated GPU resource present

## Requirements

Required:
- valid kubeconfig
- network access to selected clusters

Needed for action workflows:
- `kubectl` in `PATH`
- `$VISUAL` or `$EDITOR` for manifest edit, else `vi`

Needed for richer metrics:
- `metrics.k8s.io` for pod/node CPU and memory
- kubelet node proxy summary access for ephemeral usage
- advertised GPU resources on pods/nodes for GPU allocation display

## Smoke / Verification

Main commands:

```bash
go test ./...
./scripts/smoke.sh aws-dev-virginia
```

Smoke setup:
- live smoke against explicit contexts is read-only
- fixture smoke exists for richer action flows
- local kind/fixture path currently depends on working Docker runtime

## Known Limits

- typed views exist for high-value built-ins only; many resources use generic view
- ephemeral usage depends on kubelet summary reachability
- actions shell out through `kubectl`, not direct API exec/port-forward streams yet
- list usage bars are intentionally compact; exact formatting may still evolve
- fixture smoke requires local Docker/orbstack health

## License

MIT
