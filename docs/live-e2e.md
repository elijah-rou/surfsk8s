# Live k3d + real-PTY E2E

surfsk8s has three complementary test layers:

1. Fast Go model, Python policy, and static checks. These do not need Docker.
2. `scripts/e2e/run.sh`, a fixed real-k3d/PTY acceptance replay.
3. `scripts/e2e/run.sh --semantic`, a credential-free semantic observation-action walk. It classifies each live capture, derives safe UI actions from visible evidence plus explicitly recorded environment capabilities such as resize, chooses with a seeded local PRNG, executes one action, and waits for a semantic outcome.

The manual `scripts/e2e/agentic.sh` protocol remains available for humans and agents. It is not the automated semantic policy and can retain a cluster only when explicitly requested.

## Prerequisites and blast radius

Required: a healthy Docker daemon, Linux amd64 or arm64, `kubectl`, Go, tmux, Python 3, `timeout`, `flock`, and either curl or wget. k3d is downloaded at a pinned version into `${SURFSK8S_E2E_CACHE:-$XDG_CACHE_HOME/surfsk8s-e2e}`. Set `K3D_BIN=/absolute/path/to/k3d` to use an installed executable.

The pinned cluster image is `rancher/k3s:v1.35.1-k3s1@sha256:634920385dc89133d80060b3a3b2b547e734d711ef8c050e6b5c6341800d53fd`.

**Docker access is effectively host-root access.** Run this harness only against a Docker daemon where creating containers, a network, and a volume is acceptable. It never deletes all k3d clusters, reads or changes the default kubeconfig, uses host ports/mounts/networking, or creates Secrets. Every run owns a random validated cluster, kubeconfig, XDG tree, tmux socket/session, and cleanup state.

## Fixed scripted run

```bash
./scripts/e2e/bootstrap-k3d.sh
./scripts/e2e/run.sh
```

The scripted driver follows a fixed scenario. It covers connection, resize, Pods and logs, generic Widget discovery/detail/watch churn, Deployment scale cancellation and mutation, API verification, preferences, quit, and `SURFSK8S_SHELL_RESTORED rc=0`.

## Automated semantic run and replay

```bash
./scripts/e2e/run.sh --semantic --seed 743389
./scripts/e2e/run.sh --semantic --replay /path/to/semantic-decisions.jsonl
```

The seed is a validated decimal uint64. The default is `743389`. Replay and seed are mutually exclusive. Replay provisions a fresh isolated cluster and fails at the first state, fingerprint, affordance, target, candidate, action, result, schema, or ordering divergence. Generated context names and volatile ages are canonicalized. A divergence writes a bounded diagnostic screen.

The classifier uses stable titles, prompts, footers, and fixture rows. It rejects unknown or ambiguous screens. Candidate actions are rebuilt from each observation and sorted before PRNG selection. UI actions require a currently visible affordance and target; resize is modeled separately as an explicit tmux environment capability recorded in the trace. Finder selection first filters to one exact allowlisted resource, then permits Enter only when that resource is the sole visible resource target. The allowlist covers context selection/connection, command and resource finders, fixture-backed visible resource selection, visible-row details, Pod logs, filter/back/Escape, terminal resize, confirmation cancellation, transient Widget watch churn, and quit/restoration. Delete, edit, exec, port-forward, restart, scale, and arbitrary external actions are unrepresentable. The only mutation is an explicit-kubeconfig, seed-named transient Widget used to prove watch behavior. The driver asserts prior absence, creates rather than applies, captures its UID, deletes with a UID precondition, proves post-absence, and independently snapshots baseline fixture UIDs/values and Deployment replicas.

No screen coordinates, fixed-delay synchronization, hidden fixed route, model call, network API credential, or host preference is used. Polling is semantic at 200 ms. Bounds are 48 actions, five minutes wall time, 30 seconds per transition/watch outcome, 2 MiB per interpreted capture, six repeated identical states, eight decisions without a new goal, 64 trace records, 1 MiB JSONL, 16 MiB raw terminal by default, and live tmux checks before capture/input. Semantic mode rejects `SURFSK8S_E2E_KEEP_CLUSTER=1`.

Required goals include connection, at least three post-connect states, a route chosen from visible candidates, a visible-row detail, back navigation, resize, the fixture log marker, generic Widget watch observation, resource finder coverage, at least ten actions, unchanged API fixtures, clean quit, and shell restoration.

## Isolation, cleanup, and artifacts

`run.sh` is the sole cluster, kubeconfig, XDG, tmux, and cleanup boundary. Every kubectl command passes the isolated kubeconfig and context. The wrapper fingerprints host preferences and the default kubeconfig/current context before and after a run. EXIT, ERR, INT, and TERM invoke ownership-scoped cleanup under `flock`, then verify exact cluster, Docker, tmux, and state absence. Ambiguous cleanup retains recovery state and prints an exact retry command.

Artifacts are under `artifacts/e2e/<run-id>/` or `SURFSK8S_E2E_ARTIFACTS`. Semantic success includes `semantic-decisions.jsonl`, `semantic-goals.json`, bounded screens/raw terminal, `kubectl-assertions.log`, environment, cleanup, and host-restoration evidence. Trace records contain schema, seed, step, a bounded structured canonical fingerprint, sorted visible affordances/targets, environment capabilities, candidates, chosen action, result state/fingerprint, and new goals. Canonical semantic rows preserve meaningful titles, rows, values, and warnings while normalizing generated contexts, Pod names, timestamps, and ages. Records exclude full screens, kubeconfig, credentials, environment values, and volatile paths. Never add Secrets to fixtures or diagnostics.

## Manual agentic protocol

```bash
SURFSK8S_AGENTIC_TTL_SECONDS=1800 ./scripts/e2e/agentic.sh
```

This provisions the same boundary and prints explicit tmux/kubectl/cleanup commands for manual exploration. TTL defaults to 30 minutes and is capped at 24 hours. Only this mode permits explicit preservation:

```bash
SURFSK8S_E2E_KEEP_CLUSTER=1 SURFSK8S_AGENTIC_TTL_SECONDS=1800 ./scripts/e2e/agentic.sh
```

Preservation leaves credentials and a cluster on disk. Run the printed cleanup helper when finished.

## CI

`.github/workflows/live-e2e.yml` reports `deterministic-tests`, `scripted-live`, and `semantic-live` separately. The two live jobs depend on deterministic tests and then run in parallel on independent runners/clusters. Actions, Go, kubectl, k3d, and k3s are pinned. Live commands have a 14-minute outer timeout inside a 20-minute job, reserving cleanup time. Semantic CI uses seed `743389`, no secrets or model environment, and keep mode disabled. Failure diagnostics and short-retention semantic success evidence exclude kubeconfig, XDG, and Secret material.
