# Resource details: what's next

Current state:
- Core typed detail pages exist for pods, deployments, services, nodes.
- Detail pages now use stronger boxed sections instead of flat lists.
- Deployments have rollout, template resources, and runtime pod-usage sections.
- Deployments, services, and nodes have embedded associated-pod tables.
- Detail pages now use explicit pane switching:
  - `]` / `tab` -> pods pane
  - `[` / `shift+tab` -> details pane
- Typed generic detail pages exist for:
  - DaemonSets
  - StatefulSets
  - Jobs

## Immediate polish

### 1. Tighten pane UX
- Make active pane even more obvious with stronger border/background contrast.
- Consider showing per-pane key hints inside each pane header, not only footer.
- Consider preserving per-pane cursor/scroll position more deliberately when switching.
- Consider a small divider/status row between panes showing current focus + pod count.

### 2. Improve core detail pages

#### Pods
- Add stronger summary layout at top.
- Improve condition rendering readability.
- Add clearer separation for networking/runtime/scheduling/metadata.
- Consider optional raw spec/status block at bottom.

#### Deployments
- Keep split resource sections:
  - template requests/limits
  - runtime usage from matched pods
- Improve runtime usage formatting:
  - aggregate totals more prominent
  - better per-pod usage rows
- Add richer rollout info:
  - observed generation
  - unavailable/current replicas
  - collision count when present
- Consider container image/version grouping.

#### Services
- Expand traffic-policy/networking block.
- Add better port presentation for named ports, target ports, node ports, app protocol.
- Consider endpoint summary if cheap/reliable enough.

#### Nodes
- Improve utilization presentation further.
- Add clearer system-info grouping.
- Keep taints/conditions/capacity distinct and easy to scan.

## Next typed resources to add
Prioritized order:
1. ConfigMaps
2. HorizontalPodAutoscalers
3. PodDisruptionBudgets
4. PersistentVolumeClaims
5. ServiceAccounts
6. Roles
7. RoleBindings
8. PriorityClasses
9. RuntimeClasses
10. MutatingWebhookConfigurations
11. ValidatingWebhookConfigurations

## Generic-resource renderer direction
For resources with stable known schemas, prefer typed renderers over raw generic YAML-first views.
For everything else, keep generic details but improve formatting:
- boxed summary block
- key printer/status fields above YAML
- YAML retained as fallback/raw source of truth

## Table-in-detail follow-ups
- Reuse more of normal resource-list affordances in embedded pod tables where sensible.
- Consider future embedded tables for non-pod related resources too.
- Consider whether embedded pod table should support filtering/sorting in-place later.

## Validation to keep doing
For each resource-detail iteration:
- add focused renderer tests
- add navigation tests for pane switching + in-pane movement
- run `go test ./...`
- do live TUI sanity checks on real clusters when practical

## Suggested next implementation slice
Best next slice:
1. ConfigMap typed detail page
2. HPA typed detail page
3. PDB typed detail page
4. PVC typed detail page
5. polish embedded-pane visuals one more round after live use
