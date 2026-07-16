# TUI model tests and smoke

## Deterministic model tests (CI / agents)

```bash
# Focused stability regressions
timeout 90s go test ./internal/app -run 'Test(ModelScenario|Generic.*DoesNotBlock|GenericListResult|ForcedLogRefreshCancelsPreviousRequest|ListSelectionPreservesResourceIdentity|HeadlessProgramStartupQuit|ModelHarnessBatch)' -count=1 -timeout=60s

timeout 90s go test ./internal/cluster -run 'Test(NodeLogResponseIsBounded|ManagerCloseCompletesWhenDiscoveryStalls)' -count=1 -timeout=60s

# Full suite
timeout 150s go test ./... -count=1 -shuffle=on -timeout=120s
timeout 240s go test -race ./... -count=1 -shuffle=on -timeout=180s
```

Model tests use an offline harness (`modelHarness`) with channels/barriers — no sleeps, no live cluster, no PTY. Batch commands run concurrently to match Bubble Tea. `modelHarness.Resize` is exercised during generic-list loading, confirmation modals, and in-flight log requests (`TestModelScenarioResizePreservesTransientUIState`).

## PTY / expect smoke

`scripts/smoke.expect` is optional. This environment may lack `expect`, and the script is delay/cluster dependent. Prefer `TestHeadlessProgramStartupQuit` for CI-safe Bubble Tea loop coverage.
