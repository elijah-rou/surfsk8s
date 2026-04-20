# Performance Plan

This document is the focused plan for the next performance pass.

## Problem Statement

Recent feature work improved capability, but introduced noticeable lag when:
- entering the catalog/dashboard
- opening pod/resource list screens
- navigating between sections
- refreshing large list screens

Two broad regressions showed up:
1. **blocking work on UI refresh paths**
2. **too many allocations in hot local render/filter/sort paths**

Both need to be addressed. Fixing only one side will not be enough.

## Current Measured Baseline

Latest benchmark snapshot (local run, `go test ./internal/app -bench ... -benchmem -count=3`):

| Benchmark | Approx result |
|-----------|----------------|
| `BenchmarkRefreshPods10000CachedUsage` | ~`274µs/op`, ~`3.2KB/op`, `27 allocs/op` |
| `BenchmarkBuildPodOverviewCard10000` | ~`273µs/op`, ~`2.9KB/op`, `3 allocs/op` |
| `BenchmarkBuildRecentRestartLines10000` | ~`2.0ms/op`, ~`29KB/op`, `95 allocs/op` *(after top-K heaps + streaming generic rows; varies by machine)* |
| `BenchmarkViewPods10000` | ~`2.06ms/op`, ~`415KB/op`, `7848 allocs/op` |
| `BenchmarkPodsTableView10000` | ~`1.84ms/op`, ~`222KB/op`, `7825 allocs/op` |
| `BenchmarkMatchPodRowAt10000NoColumnFilters` | ~`16ns/op`, `0 allocs/op` |

**Earlier plan baseline** (for comparison — before fast paths / caching / render fixes):

- `BenchmarkRefreshPods10000CachedUsage` was reported at very high alloc counts in older notes; current microbench shows **27 allocs/op** on refresh with cached usage.
- `BenchmarkBuildRecentRestartLines10000` was ~`12.8ms/op`, `29.8MB/op`, `68.7k allocs/op`; recent work (**`PodObjectByKey`** vs deep copy, etc.) reduced this materially.

What this says now:
- **Pod list refresh** (microbench) is low-allocation; keep it there when adding features.
- **Recent restarts** is improved but still the heaviest catalog-local path; further wins need top-N caps / less sorting work.
- **Table `View()`** still dominates frame time on huge lists; style/divider caching helps; profile when changing `table.go`.

## Confirmed Regressions / Suspected Hotspots

### 1. Blocking work on navigation / refresh paths

These were introduced or worsened by newer UX/data work:

- synchronous dashboard overview building on catalog refresh
- synchronous pod/node usage snapshot fetching on list refresh
- repeated list/catalog refreshes on timer instead of only when data actually changed

These make the app feel laggy even before render allocations are considered.

### 2. Local allocation-heavy hot paths

These still need work even after blocking network work is moved off the UI path.

#### Pod/resource list refresh

Likely contributors:
- full-store scans on refresh
- namespace collection rebuilt each refresh
- column-filter evaluation building full row-cell slices even when no filters are enabled
- sort helpers building enabled-sort slices repeatedly
- repeated age formatting / row copies during scans

#### View/render path

Likely contributors:
- repeated string joining/building in `View()` and compact list header/status rendering
- table rendering allocating per row/cell
- repeated style construction in `table.go`
- repeated manager status slice building every frame
- any code that renders the same table body twice per frame

#### Dashboard overview

Likely contributors:
- generic resource listing for multiple workload cards
- recent warning/restart line construction
- repeated data aggregation even when scope/data has not changed

## Goals

### UX goals

- section changes should feel immediate
- opening catalog should not block on overview fetching
- opening pod/node lists should not block on metrics fetching
- scrolling and typing in large lists should remain smooth

### Engineering goals

- remove blocking network/API work from hot UI refresh paths
- cut pod refresh allocations by a large margin
- add benchmarks for the actual hot code so regressions are visible
- make performance work measurable, not anecdotal

## Work Plan

## Phase 1: Stop blocking the UI thread

### A. Catalog overview

Move overview population off the synchronous catalog refresh path.

Do:
- keep `refreshCatalog()` cheap
- populate overview asynchronously in the background
- cache overview by:
  - scope (`contextScope`, `namespace`)
  - relevant store/manager version
- show stale overview briefly or a loading placeholder instead of blocking

Success criteria:
- entering catalog does not wait on workload/event aggregation
- scope changes remain responsive even with overview enabled

### B. Pod/node table usage snapshots

Move list usage snapshots off synchronous list refresh.

Do:
- async background fetch for pod list usage
- async background fetch for node list usage
- cache results by:
  - scope
  - store version
- keep details refresh separate from list refresh
- never block list rendering on metrics fetch

Success criteria:
- opening pods/nodes lists is immediate
- metrics appear after render, not before it

### C. Data-driven refresh policy

Ensure list/catalog screens refresh only when needed.

Do:
- remove timer-driven full recompute for list/catalog screens when data did not change
- keep timed refresh only where it is genuinely needed
  - details screens with age/usage updates

Success criteria:
- no full list rebuild every second just to keep the screen alive

## Phase 2: Attack hot-path allocations in large list refresh

This is the biggest remaining issue.

### A. Zero-filter fast paths

Avoid row-cell slice construction unless filters are actually enabled.

Do:
- short-circuit `matchesPodColumnFilters`, `matchesNodeColumnFilters`, etc. when there are zero enabled filters
- avoid calling `podCells(...)`, `nodeCells(...)`, `genericCells(...)` on the scan path unless required

Expected win:
- major alloc drop on large list refresh with default settings

### B. Zero-sort / enabled-sort fast paths

Avoid building enabled-sort slices in hot code.

Do:
- replace `enabledTableSorts()` hot-path allocations with direct checks/iteration
- cache sort label / metadata where practical
- only materialize sorted slices when an enabled sort actually exists

Expected win:
- fewer per-refresh allocations and less repeated sort bookkeeping

### C. Namespace caching

Stop rebuilding namespace lists on every refresh.

Do:
- cache namespace lists per screen/resource/scope
- invalidate only when relevant store version or scope changes

Expected win:
- cheaper refreshes on pod/deployment/service screens

### D. Reduce row-copy churn

Do:
- audit `WithAge(now)` usage on scan paths
- avoid creating aged row copies unless the row is actually entering the visible window or needed for a filter comparison
- keep age formatting/rendering as late as possible

Expected win:
- fewer temporary row structs and string rebuilds

## Phase 3: Render-path cleanup

Once refresh-path allocations are under control, reduce frame allocations further.

### A. `View()` / header/status work

Do:
- ensure `currentView()` is called once per frame
- reuse cluster status buffers
- replace repeated `strings.Join`/small temporary slices with builders where hot
- review status/footer helpers for needless allocations

### B. Table rendering

Do:
- profile `internal/ui/components/table.go`
- cache styles used per column/selection state where possible
- reduce per-cell style creation in `renderDataRow`
- check header wrapping/divider rendering for repeated small allocations

Success criteria:
- large-list view renders allocate materially less per frame

## Phase 4: Dashboard-specific local optimizations

The dashboard should not become the slow path again after async loading.

### A. Recent restarts

This benchmark is still expensive.

Do:
- avoid deep detail fetches where row data is already enough
- reduce temporary object/line allocations
- cap work early before full sorting when possible
- sort only what is needed for the top N rows

### B. Recent warnings

Do:
- apply same top-N / early-cap approach
- avoid unnecessary string formatting until final selected rows

### C. Workload cards

Do:
- reuse/cap generic rows for overview cards where practical
- avoid repeated conversions when data did not change

## Phase 5: Add permanent perf coverage

Performance work should stay visible.

### Benchmarks to keep

Add/keep benchmarks for:
- `refreshPods` on 10k rows with cached usage
- `View()` on large pod list
- table render on visible window with rich rows
- catalog overview build
- recent restart summary build
- generic resource list filter/sort/render path

### Optional profiling workflow

Add documented commands for:
- `go test -bench ... -benchmem`
- `-cpuprofile`
- `-memprofile`
- `pprof` inspection

## Suggested Implementation Order

1. remove blocking network work from catalog/list navigation
2. add zero-filter fast paths
3. add zero-sort fast paths
4. cache namespace lists
5. benchmark again
6. attack recent restart allocations
7. attack `View()` / table render allocations
8. benchmark again

## Acceptance Criteria

At minimum, the next pass should achieve all of these:

- opening catalog does not block on overview data *(async overview + loading placeholder in `catalog` view — verify in UI)*
- opening pod/node lists does not block on metrics fetch *(usage snapshots wired to avoid blocking list render — verify in UI)*
- no timer-driven full rebuilds of catalog/list screens when data unchanged *(refresh policy tightened — verify with logging/profiling if needed)*
- pod refresh allocs materially lower than current baseline *(microbench: ~`27 allocs/op` for `BenchmarkRefreshPods10000CachedUsage`)*
- a dedicated `View()` benchmark exists *(see `BenchmarkViewPods10000`, `BenchmarkPodsTableView10000` in `internal/app/perf_bench_test.go`)*
- documented perf workflow exists for future passes *(see `docs/PERFORMANCE-WORKFLOW.md`)*

## Implementation status (this repo)

Completed or largely addressed in code + benches:

- **Phase 2A–C**: zero-column-filter fast paths; zero-sort / materialized-sort checks; namespace list caching (`internal/app/table_filters.go`, `table_sorts.go`, `app.go`).
- **Phase 2D**: reduced `WithAge(now)` churn — sort compares use **`podCellStringAt` / `nodeCellStringAt`** (`resource_usage.go`, `sort.go`); column filters call **`WithAge` only when an enabled filter targets the AGE column** (`table_filters.go`); generic column filters only age when needed.
- **Phase 3A**: single `currentView()` per frame; status/builder reuse (`app.go`, `cluster/manager.go`, `statusbar.go`).
- **Phase 3B**: table style/divider caching (`internal/ui/components/table.go`).
- **Phase 4A**: recent restarts — **`PodObjectByKey`**, **no alloc merging container statuses** in `podRestartOverviewItem`, **top-K heap** for overview lines (`catalog_overview.go`).
- **Phase 4B**: recent warnings — **streamed `ForEachGenericResourceRow` + min-heap top-5** (no full `[]eventOverviewItem` for every warning).
- **Phase 4C**: workload / CronJob / Job cards — **`forEachFilteredCatalogRow`** + **`Manager.ForEachGenericResourceRow`** / **`genericResourceWatch.forEachRow`** to avoid allocating merged generic row slices on hot paths (`catalog_overview.go`, `internal/cluster/manager.go`, `generic_watch.go`).
- **Phase 5**: microbenchmarks + `PERFORMANCE-WORKFLOW.md`.
- **Tests**: `internal/state/store_test.go` (`TestStorePodObjectByKey`); `internal/app/catalog_overview_test.go` (heap top-K tests).

Optional follow-up (diminishing returns):

- Further reduce `formatAge` calls in **`windowRowsFromSlice`** / per-frame table row paths if profiling shows it.
- **ListGenericResource** could internally reuse **`forEachRow`** to avoid per-watch slice copies when callers only need iteration (API change).

## Non-Goals

Not part of this pass unless needed for correctness:
- redesigning the dashboard itself
- removing rich usage bars
- deleting typed views in favor of generic ones
- adding new features before perf baselines are under control
