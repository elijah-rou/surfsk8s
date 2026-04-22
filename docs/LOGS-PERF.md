# Logs Performance Plan

This doc records two things:

1. deferred logs UX follow-ups worth doing after core perf is healthy
2. concrete microbench targets for the logs perf refactor

## Deferred Logs UX Follow-Ups

These came up during the logs pass and are intentionally deferred until the logs screen is fast enough to be a stable base.

### Near-term UX

- true streaming/follow mode instead of periodic refresh for pod/deployment logs
- collapsible source groups
  - pod/container
  - deployment pod/container
  - errors group
- match highlighting + next/prev match jump inside logs
- richer JSON rendering
  - level-aware colors
  - common field detection: `level`, `msg`, `message`, `ts`, `time`, `timestamp`
  - compact single-line mode when wrap is off
- export modes beyond rendered view
  - raw merged output
  - selected-source-only export
- clearer node-log range semantics
  - kubelet `/logs` file access is not a true since-time API
  - UI should not imply exact time slicing for node logs

### Longer-term

- direct streaming path for node logs where cluster/runtime allows it
- source pinning / solo mode / mute mode
- optional structured side panel for parsed JSON fields
- smarter retention controls exposed in UI

## Perf Refactor Goals

### Known hot paths

- full logs render rebuild on nearly every view update
- full viewport content reset on nearly every view update
- full-entry filtering on each render when a query is active
- global re-sort after live append batches
- serial multi-source fetch for deployment logs
- unbounded live in-memory growth

### Refactor priorities

1. cache rendered logs content so scroll/input does not rebuild the full body unless data/settings change
2. avoid resetting viewport content when content is unchanged
3. bound live log retention
4. parallelize per-source fetch with bounded concurrency
5. keep tests + smoke green on `aws-dev-virginia`

## Microbench Targets

Initial targets are practical, not heroic. They should make the logs screen feel responsive on large outputs.

Bench cases to track:

- `BenchmarkRenderLogs10000Plain`
- `BenchmarkRenderLogs10000FuzzyFilter`
- `BenchmarkRenderLogs5000JSONWrap`
- `BenchmarkViewLogs10000Plain`
- `BenchmarkHandleLogsResultAppend200Into10000`

Target ceilings:

- render 10k plain logs: <= 8 ms/op
- render 10k logs with fuzzy filter: <= 10 ms/op
- render 5k JSON pretty-wrap logs: <= 12 ms/op
- full logs screen view of 10k plain logs: <= 10 ms/op
- append 200 new live lines into 10k retained lines: <= 3 ms/op

## Benchmark Workflow

1. add/maintain microbenches
2. run baseline before perf changes
3. save raw output under `results/logs-perf/`
4. refactor
5. rerun same benches
6. save post-change output under `results/logs-perf/`
7. compare before/after in summary notes
