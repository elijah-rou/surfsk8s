# Resource jump changelog

Date: 2026-04-24
Status: in progress, not committed

## Goal

Add keyboard-driven resource relationship navigation:

- `g` jumps upward to owners/parents.
- `G` jumps downward to dependents/children.
- `g`/`G` must not be generic top/bottom navigation anywhere.
- `home`/`end` are the edge-navigation keys.

## Current behavior implemented

### Owner jump, `g`

Works from pod list, resource list, pod details, and resource details.

Targets are built from Kubernetes `ownerReferences`, then walked transitively up the chain. Example:

`Pod -> ReplicaSet -> Deployment`

Pods also expose their scheduled node as an owner-like parent target:

`Pod -> Node`

If multiple targets exist, an action picker opens. If exactly one target exists, it opens directly. If no targets exist, the app shows `no owner targets` and does not move table/viewport position.

### Dependent jump, `G`

Works from pod list, resource list, pod details, and resource details.

Current dependent discovery:

- `Deployment -> Pods` via deployment selector.
- `Service -> Pods` via service selector.
- `Node -> Pods` via scheduled node name.
- Owner-reference descendants for these workload kinds when discovered/cached:
  - ReplicaSets
  - DaemonSets
  - StatefulSets
  - Jobs
  - CronJobs

The descendant walk is bounded by `resourceJumpDepthLimit = 8` and `resourceJumpTargetLimit = 64`.

If multiple targets exist, an action picker opens. If exactly one target exists, it opens directly. If no targets exist, the app shows `no dependent targets` and does not move table/viewport position.

## Navigation key cleanup

`g`/`G` were removed as top/bottom navigation keys across app screens. Edge navigation is now only `home`/`end`.

Updated areas:

- main app screens
- command prompt
- resource finder
- namespace/context scope picker
- action picker
- table filter picker/manager
- table sort picker/manager
- text viewports
- logs/footer hints
- confirm-action/footer hints

## Files changed

- `internal/app/resource_jump.go` — new resource jump target resolution and opening logic.
- `internal/app/action_flow.go` — action picker support for resource jump targets.
- `internal/app/app.go` — `g`/`G` wiring on resource screens, `home`/`end` nav cleanup, footer updates.
- `internal/app/input_actions.go` — command prompt nav cleanup.
- `internal/app/resource_finder.go` — finder nav cleanup.
- `internal/app/scope_picker.go` — scope picker nav cleanup.
- `internal/app/table_filters.go` — filter picker/manager nav cleanup.
- `internal/app/table_sorts.go` — sort picker/manager nav cleanup.
- `internal/app/scroll.go` — viewport nav cleanup and detail-pane hint update.
- `internal/app/logs.go` — logs footer hint update.
- `internal/app/behavior_test.go` — tests for owner chain, dependent jump, and `home`/`end` command prompt nav.

## Verification run

Last verified commands:

```sh
gofmt -w internal/app/behavior_test.go
go test ./internal/app/...
go test ./...
```

Result: pass.

## Known limits

- Downward CRD/custom-resource discovery is intentionally limited. Upward owner jumps can resolve discovered custom/CRD owners via `ownerReferences`, but downward scans only cover pods and known workload resources listed above.
- Back-stack/history is not implemented. Jumping to another resource opens that detail screen, but `esc` follows existing screen-specific return behavior rather than returning through the jump chain.
- Generic descendant discovery depends on discovery/cache availability. If a workload resource is not discovered for the connected cluster, it will not be offered as a generic descendant.

## Resume points

Recommended next checks when resuming:

1. Manually smoke-test against a live cluster:
   - pod details: `g` should offer ReplicaSet, Deployment, Node.
   - deployment list/details: `G` should offer matching pods.
   - node details: `G` should offer scheduled pods.
   - service details: `G` should offer selector-matched pods.
2. Decide whether jump history/back-stack is worth adding before shipping.
3. Decide whether downward CRD discovery should stay out of scope or become a bounded opt-in scan.
4. Commit the current changes once manual smoke-test passes.
