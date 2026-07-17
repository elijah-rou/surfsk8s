# TUI model tests and live E2E

## Deterministic model tests (CI / agents)

```bash
# Focused stability regressions
timeout 90s go test ./internal/app -run 'Test(ModelScenario|Generic.*DoesNotBlock|GenericListResult|ForcedLogRefreshCancelsPreviousRequest|QuitCancelsInFlightLogFetch|CtrlCCancelsInFlightGenericListFetch|ListSelectionPreservesResourceIdentity|HeadlessProgramStartupQuit|ModelHarnessBatch)' -count=1 -timeout=60s

timeout 90s go test ./internal/cluster -run 'Test(NodeLogResponseIsBounded|ManagerCloseCompletesWhenDiscoveryStalls)' -count=1 -timeout=60s

# Full suite
timeout 150s go test ./... -count=1 -shuffle=on -timeout=120s
timeout 240s go test -race ./... -count=1 -shuffle=on -timeout=180s
```

Model tests use an offline harness (`modelHarness`) with channels/barriers — no sleeps, no live cluster, no PTY. Batch commands run concurrently to match Bubble Tea. `modelHarness.Resize` is exercised during generic-list loading, confirmation modals, and in-flight log requests (`TestModelScenarioResizePreservesTransientUIState`). Quit/`ctrl+c` cancel the app root context and any active log request (`TestQuitCancelsInFlightLogFetch`, `TestCtrlCCancelsInFlightGenericListFetch`) so in-flight fetches observe `ctx.Done` before shutdown.

## Live Kubernetes / real-PTY layer

```bash
./scripts/e2e/run.sh
```

This separate acceptance layer uses a pinned k3d install, isolated kubeconfig/config state, deterministic fixtures, and a real tmux PTY. It validates terminal behavior and live API integration that the model harness intentionally does not simulate. See [`live-e2e.md`](live-e2e.md) for scenarios, safety boundaries, artifacts, CI, and agentic exploration.
