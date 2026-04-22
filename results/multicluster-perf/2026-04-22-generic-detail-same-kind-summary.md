# Generic Detail Same-Kind Churn Perf Summary (2026-04-22)

Environment:
- macOS darwin/arm64
- Apple M1
- package: `github.com/elijahrou/surfsk8s/internal/app`

Raw results:
- baseline: `results/multicluster-perf/2026-04-22-generic-detail-same-kind-baseline.txt`
- after: `results/multicluster-perf/2026-04-22-generic-detail-same-kind-after.txt`

Bench intent:
- simulate active generic detail page
- bump the same generic resource-kind version repeatedly
- keep the active object's `resourceVersion` unchanged
- measure wasted detail rebuilds caused by unrelated same-kind object churn

Key delta:
- `BenchmarkTickGenericResourceDetails8ClustersUnderSameResourceChurn`
  - before: ~93.1 µs/op
  - after: ~1.94-1.98 µs/op
  - result: ~47x faster

Change behind the gain:
- generic detail refresh now asks manager for the active object's current `resourceVersion`
- if the resource-kind changed but the active object's version did not, surfsk8s only updates age/tick state
- avoids rebuilding YAML/details for irrelevant same-kind churn elsewhere in the list/cluster set

Expected user-visible impact:
- generic/CRD detail pages should remain responsive even when many other objects of the same kind are updating
