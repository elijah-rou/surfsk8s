# Generic Resource Multi-Cluster Perf Summary (2026-04-22)

Environment:
- macOS darwin/arm64
- Apple M1
- package: `github.com/elijahrou/surfsk8s/internal/app`

Raw results:
- baseline: `results/multicluster-perf/2026-04-22-generic-baseline.txt`
- after: `results/multicluster-perf/2026-04-22-generic-after.txt`

Bench intent:
- measure generic resource list/detail screens with 8 clusters of cached rows
- inject unrelated manager/catalog churn
- verify generic screens stop rebuilding/reloading on irrelevant global version bumps

Key deltas:
- `BenchmarkTickGenericResourceList8ClustersUnderManagerChurn`
  - before: ~1.51 ms/op
  - after: ~0.181 ms/op
  - result: ~8.4x faster
- `BenchmarkTickGenericResourceDetails8ClustersUnderManagerChurn`
  - before: ~0.357 µs/op
  - after: ~0.182 µs/op
  - result: ~2.0x faster

Changes behind the gain:
- manager global/catalog version split from generic resource versions
- generic watch events bump per-resource generic version, not manager-wide version
- generic list/detail screens now track `GenericResourceVersion(activeResource.ID)`
- unrelated discovery/catalog churn no longer invalidates active generic pages

Expected user-visible impact:
- CRD/custom resource pages should stop freezing when unrelated manager/discovery churn happens
- active generic pages now refresh only when that same generic resource kind changes
