# surfsk8s Roadmap

Current build already covers the core workflow: multi-context connect, catalog browsing, typed and generic resources, action flows, filtering, sorting, favourites, and resource usage.

This doc tracks the next improvements worth doing from here.

For the focused next performance pass, see `PERFORMANCE-PLAN.md`.

## Suggested Improvements

### Near-term UX

- Tighten usage-bar formatting further
  - unit-deduped suffixes
  - smarter per-column bar width
  - clearer request marker styling
- Add light help/discoverability pass
  - per-screen hotkey cheatsheet consistency
  - command palette descriptions cleanup
- Improve generic-resource details
  - better formatting for conditions, owner refs, events, selector-like fields
- Add more live smoke assertions around pod/node usage tables
- Add fixture smoke coverage for list-row actions
  - deployment `S` scale
  - deployment `R` restart
  - service `P` port-forward

### Medium-term architecture

- Move more action flows off `kubectl` shell-outs where direct client-go path is clearly better
- Improve generic watch/cache eviction visibility and instrumentation
- Add benchmark coverage for mixed typed + generic large-fleet scenarios
- Broaden typed first-class views only where UX gain is clear
  - ReplicaSets
  - StatefulSets
  - Jobs/CronJobs

### Long-term

- Logs view maturity pass
  - follow mode
  - container picker polish
  - scope-aware log workflow
- Safer multi-resource operations with explicit batch confirmation
- Better event and warning surfacing across screens
- Cluster overview/dashboard iteration beyond current catalog block

## Pod-Specific Needs

These are the pod-focused improvements with best ROI.

### Pod list

- Optional pod age/restarts/status compaction to free horizontal space
- Better presentation of pending reasons and crashloop state
- Optional split of `STATUS` into phase + reason where useful
- Fast jump from pod row to owning deployment/replicaset if resolvable

### Pod details

- Stronger container section
  - image
  - resource requests/limits
  - restart count per container
  - readiness/liveness/startup probe summary
- Better condition rendering
- Events section with warning-first ordering
- Owner references shown prominently

### Pod actions

- Exec shell selection beyond `sh` fallback
  - `bash`, `sh`, `ash`, detected in sensible order if feasible
- Better multi-container UX in crowded pods
- Log workflow integrated directly from pod details/list
- Safer manifest edit preview before apply

### Pod resource usage

- Better table suffix format
- Optional burst indicator when usage exceeds request but stays below limit
- Consider pod request/limit columns only if density remains acceptable
- Revisit ephemeral usage if a cheaper/more reliable source becomes available

## Operational/Data Needs

These are not code features, but cluster capabilities the richer UI depends on.

- `metrics.k8s.io` for CPU/memory usage
- kubelet summary access via node proxy for ephemeral usage
- GPU extended resources advertised consistently if GPU allocation matters
- stable kubeconfig hygiene across many local contexts
- `kubectl` available locally for action workflows
- editor configured via `$VISUAL` or `$EDITOR` for manifest edits

## Explicit Non-Goals Right Now

- Heuristic prod detection for confirms
- Unbounded generic informer sprawl
- Heavy plugin surface before core workflows settle
- Fancy visual row spacing tricks that reduce density without clear gain
- Replacing typed views with generic views where typed UX is materially better
