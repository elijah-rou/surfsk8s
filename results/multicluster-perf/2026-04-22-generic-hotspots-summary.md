# Generic Hotspot Summary (2026-04-22)

Environment:
- macOS darwin/arm64
- Apple M1
- package: `github.com/elijahrou/surfsk8s/internal/app`

Raw results:
- baseline: `results/multicluster-perf/2026-04-22-generic-hotspots-baseline.txt`
- after sort/search pass: `results/multicluster-perf/2026-04-22-generic-hotspots-after.txt`
- after filter-cache pass: `results/multicluster-perf/2026-04-22-generic-hotspots-after2.txt`
- no-filter CPU profile: `results/multicluster-perf/2026-04-22-generic-buildsorted-nofilter.pprof.txt`
- fuzzy CPU profile: `results/multicluster-perf/2026-04-22-generic-buildsorted-fuzzy.pprof.txt`
- column-filter CPU profile: `results/multicluster-perf/2026-04-22-generic-buildsorted-columnfilter.pprof.txt`

Bench-driven findings:
- no-filter hotspot was full generic-list sorting work
- fuzzy hotspot was search candidate rebuild/lowercasing
- column-filter hotspot was wildcard match + ANSI strip + repeated filter normalization

Changes shipped:
1. sort/search pass
- cached lowercase sort keys on generic rows
- cached lowercase generic search text
- no-custom-sort fast path for generic lists
- preserved base sorted order in `genericRows`

2. filter-cache pass
- cached normalized generic filter cell values per row/column
- reused precomputed columns during generic row scope checks
- stopped re-normalizing candidate/query per filter match
- reduced ANSI strip / lowercase churn in generic column filters

Key deltas vs original baseline:
- `BenchmarkBuildSortedGenericResources8ClustersNoFilter`
  - before: ~32.7 ms/op
  - after filter-cache pass: ~1.35 ms/op
  - result: ~24x faster
- `BenchmarkBuildSortedGenericResources8ClustersFuzzyQuery`
  - before: ~12.3 ms/op
  - after filter-cache pass: ~6.21-6.24 ms/op
  - result: ~2x faster
  - allocs: ~118k -> ~36k
- `BenchmarkBuildSortedGenericResources8ClustersColumnFilter`
  - before: ~14.9 ms/op
  - after sort/search pass: ~13.7 ms/op
  - after filter-cache pass: ~9.72 ms/op
  - result: ~1.53x faster overall
  - allocs: ~71k -> ~22k
- `BenchmarkGenericRowMatchesScope8ClustersColumnFilter`
  - before: ~799 ns/op, 4 allocs/op
  - after filter-cache pass: ~616-619 ns/op, 2 allocs/op

Interpretation:
- default generic-list rebuild path is now much healthier
- query path materially improved
- column-filter path improved enough to move it down, but it is still the most expensive generic rebuild mode
- next likely ROI is row/window render alloc reduction (`genericTableRows`) or compiled wildcard filter matching if column filters remain a problem in real use
