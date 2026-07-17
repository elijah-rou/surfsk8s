# Live k3d + real-PTY E2E

The live layer validates surfsk8s against a real Kubernetes API through a real terminal. It complements, and does not replace, the deterministic model tests in [`tui-model-tests.md`](tui-model-tests.md).

## Why the old smoke was retired

The previous `smoke.sh`/Expect path could inspect an existing context, read the default kubeconfig, create or reuse a fixed cluster, switch between fake and live behavior, and synchronize with fixed delays. Expect was not installed in the supported environment. Those properties made a pass environment-dependent and made its blast radius unclear. The replacement always owns one uniquely named k3d cluster, one kubeconfig, one config directory, and one tmux session.

## Prerequisites and warning

Required: a healthy Docker daemon, Linux amd64 or arm64, `kubectl`, Go, tmux, Python 3, `timeout`, and either curl or wget. k3d is downloaded at the pinned version into `${SURFSK8S_E2E_CACHE:-$XDG_CACHE_HOME/surfsk8s-e2e}`. The bootstrap checks both the release checksum manifest and a checksum pinned in the script. Set `K3D_BIN=/absolute/path/to/k3d` to use an already installed executable.

**Docker access is effectively host-root access.** The harness creates containers and a Docker network. Run it only against a Docker daemon where that is acceptable. It never runs `k3d cluster delete --all`, does not use the default kubeconfig, and fixtures use no privileged containers, host mounts, host networking, host ports, or Secrets.

## Scripted acceptance run

```bash
./scripts/e2e/bootstrap-k3d.sh
./scripts/e2e/run.sh
```

`run.sh` has per-operation deadlines and a bounded PTY run. It creates a random validated `surfsk8s-e2e-*` cluster, disables k3d's default-kubeconfig update and context switch, writes a mode-0600 temporary kubeconfig, passes the isolated `XDG_CONFIG_HOME` explicitly to the TUI, builds once, applies fixtures, and waits on API readiness conditions. Every kubectl invocation supplies that kubeconfig and context explicitly. The harness fingerprints the host preferences and default kubeconfig/context before and after each run. EXIT, ERR, INT, and TERM paths invoke the generated cleanup helper, which stops only the run's dedicated tmux socket/session, deletes exactly the owned cluster, and removes exactly the temporary state tree.

The tmux driver polls interpreted screen text at semantic checkpoints through a collision-resistant dedicated tmux socket. It records each screen plus the raw terminal stream. It checks startup selection and connection, catalog rendering, small/large resizes, Pod navigation and a stable log marker, CRD discovery/list/detail and printer columns, watch add/delete churn, a visible Deployment scale hint at 160 columns, cancelled then confirmed scaling, an independent API replica/rollout assertion, isolated preference persistence, bounded quit, and a shell-restoration marker after alternate-screen exit.

Artifacts are written under `artifacts/e2e/<run-id>/`. On failure they include the last screen, Kubernetes resources/events, CRD schema, bounded container logs, and cluster lifecycle logs. The isolated kubeconfig and XDG state remain in the temporary state directory and are never copied into artifacts. Avoid adding Secrets to fixtures or diagnostic collection.

## Agentic exploratory protocol

This mode provisions the same isolated cluster and fixture set, then leaves a bounded real tmux PTY for semantic exploration:

```bash
SURFSK8S_AGENTIC_TTL_SECONDS=1800 ./scripts/e2e/agentic.sh
```

The command prints shell-quoted kubeconfig, context, namespace, state, XDG, artifacts, tmux socket/session, explicit `kubectl`, `tmux capture-pane`, `tmux send-keys`, attach, quit, and full cleanup commands. Inspect interpreted state with `capture-pane`; send literal queries with `send-keys -l`; send keys such as `Enter`, `Escape`, `Up`, or `C-c` by name. The default TTL is 30 minutes and the maximum is 24 hours. Exiting the UI, signals, errors, and TTL expiry run the same ownership-scoped cleanup helper.

Preservation is opt-in only:

```bash
SURFSK8S_E2E_KEEP_CLUSTER=1 SURFSK8S_AGENTIC_TTL_SECONDS=1800 ./scripts/e2e/agentic.sh
```

The final output and `KEEP.txt` print the exact kubeconfig, config-state, and cleanup-helper command. This leaves credentials on disk and a running cluster; execute the printed helper when finished. It removes the dedicated tmux server, exact cluster, and exact state tree. Scripted CI never enables keep mode.

## CI

`.github/workflows/live-e2e.yml` runs model tests and the live suite on pull requests and manual dispatch. It pins action commits, Go, kubectl, k3d, and k3s versions; bounds job time; serializes redundant branch runs; and uploads failure artifacts only. Kubeconfigs and Secrets are excluded by design and artifact patterns.
