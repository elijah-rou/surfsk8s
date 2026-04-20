# Performance workflow

Use this when measuring or regressing performance work (see `docs/PERFORMANCE-PLAN.md`).

## Microbenchmarks (allocations + time)

From repo root:

```bash
go test ./internal/app -bench . -benchmem -count=5
```

Key benches (large lists):

| Benchmark | What it measures |
|-----------|------------------|
| `BenchmarkRefreshPods10000CachedUsage` | Pod list refresh + window provider wiring |
| `BenchmarkViewPods10000` | Full `App.View()` on pod list |
| `BenchmarkPodsTableView10000` | Rich `Table.View()` only |
| `BenchmarkMatchPodRowAt10000NoColumnFilters` | Per-row match path (filters off) |
| `BenchmarkBuildPodOverviewCard10000` | Catalog pod card |
| `BenchmarkBuildRecentRestartLines10000` | Recent restart lines |

Example snapshot (Apple M1 Pro, darwin/arm64, `-count=3`): refresh ~`274µs/op` / `27 allocs/op`; recent restarts ~`2ms/op` / `~95 allocs/op` (top-K heap + streaming); full `View()` ~`2.06ms/op`; table-only ~`1.84ms/op`. Numbers drift with hardware — use `benchstat` for comparisons.

Compare before/after by saving `bench.txt` and using `benchstat` (golang.org/x/perf/cmd/benchstat):

```bash
go test ./internal/app -bench 'BenchmarkRefreshPods10000CachedUsage|BenchmarkViewPods10000|BenchmarkPodsTableView10000|BenchmarkMatchPodRowAt10000NoColumnFilters' -benchmem -count=10 > /tmp/bench-new.txt
benchstat /tmp/bench-old.txt /tmp/bench-new.txt
```

## CPU profile

```bash
go test ./internal/app -bench BenchmarkRefreshPods10000CachedUsage -cpuprofile=/tmp/cpu.prof
go tool pprof -http=:0 /tmp/cpu.prof
```

## Heap / allocation profile

```bash
go test ./internal/app -bench BenchmarkRefreshPods10000CachedUsage -memprofile=/tmp/mem.prof
go tool pprof -http=:0 /tmp/mem.prof
```

Use `top`, `list`, and the flame graph in the web UI to find hot functions.
