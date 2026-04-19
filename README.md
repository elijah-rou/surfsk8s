# surfsk8s

Multi-cluster Kubernetes TUI built for scale. Handles 10k+ pods across 6-10 clusters without breaking a sweat.

## Why

k9s falls over at scale. 2k+ deployments = 30s startup. 10k pods = unusable. No multi-cluster support. Every alternative is either dead, read-only, or tiny.

surfsk8s is built from the ground up for large fleet management in the terminal.

## Design Principles

- **Informers, not polling.** SharedIndexInformer streams deltas. No full-list API calls every N seconds.
- **Multi-cluster first.** Not bolted on. Every resource knows which cluster it belongs to. Unified view across all contexts.
- **Virtual scrolling.** Only visible rows render. 10k pods in memory, 50 on screen.
- **Full CRUD.** Not read-only. Delete, scale, restart, cordon, drain, exec, edit.
- **Zero external deps.** Reads kubeconfig. No server component, no database, no config service.

## Architecture

```
┌─────────────────────────────────────────┐
│              Bubbletea App              │
│  ┌─────────┐ ┌──────────┐ ┌─────────┐  │
│  │  Views   │ │  Filter  │ │ Status  │  │
│  │ pods/dep │ │  fuzzy   │ │  bar    │  │
│  │ svc/logs │ │  exact   │ │ health  │  │
│  └────┬─────┘ └────┬─────┘ └────┬────┘  │
│       └─────────────┼───────────┘        │
│                     │                    │
│              ┌──────┴──────┐             │
│              │  State Store │             │
│              │  indexed,    │             │
│              │  cross-cluster│            │
│              └──────┬──────┘             │
│       ┌─────────────┼───────────┐        │
│  ┌────┴────┐  ┌─────┴────┐ ┌───┴─────┐  │
│  │Cluster A│  │Cluster B │ │Cluster N│  │
│  │informers│  │informers │ │informers│  │
│  │clientset│  │clientset │ │clientset│  │
│  └─────────┘  └──────────┘ └─────────┘  │
└─────────────────────────────────────────┘
```

## Stack

- **Go** - client-go informers battle-tested at 100k+ pod scale
- **Bubbletea** - Elm-architecture TUI framework
- **Lipgloss** - Styling
- **client-go** - Official Kubernetes client with SharedIndexInformer

## Project Structure

```
surfsk8s/
├── main.go
├── cmd/                    # CLI entrypoint, flag parsing
├── internal/
│   ├── app/                # Top-level Bubbletea model
│   ├── cluster/            # Multi-cluster client management
│   ├── informer/           # SharedIndexInformer wrappers
│   ├── state/              # Cross-cluster state store, indexing
│   ├── ui/
│   │   ├── components/     # Table, filter, statusbar (reusable)
│   │   ├── views/          # Resource-specific views (pods, deployments, etc.)
│   │   └── theme/          # Colors, styles
│   └── actions/            # CRUD operations (delete, scale, exec, etc.)
└── docs/
    └── POC.md              # Proof of concept plan
```

## Status

Early development. See [docs/POC.md](docs/POC.md) for the proof of concept plan.

## License

MIT
