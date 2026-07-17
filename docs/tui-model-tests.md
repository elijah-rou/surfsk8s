# TUI test layers

surfsk8s keeps three distinct automated layers plus a manual protocol.

## 1. Deterministic model, policy, and static tests

```bash
timeout 150s go test ./... -count=1 -shuffle=on -timeout=120s
python3 -m unittest discover -s scripts/e2e -p 'test_*.py'
bash -n scripts/e2e/run.sh scripts/e2e/agentic.sh
```

Go model tests use offline channels and barriers, not sleeps, a cluster, or a PTY. Python tests cover PTY helpers and semantic classification, safe policy, seeds, bounds, trace validation, and strict replay contracts. This is the fast feedback layer.

## 2. Fixed live Kubernetes / PTY replay

```bash
./scripts/e2e/run.sh
```

This acceptance layer uses pinned k3d, isolated kubeconfig/XDG state, deterministic fixtures, and a real tmux PTY. Its route is intentionally fixed and checks specific terminal and API behavior.

## 3. Seeded semantic live walkthrough

```bash
./scripts/e2e/run.sh --semantic --seed 743389
./scripts/e2e/run.sh --semantic --replay /path/to/semantic-decisions.jsonl
```

This layer observes normalized live screens, classifies mutually exclusive semantic states, extracts visible allowlisted affordances and fixture targets, combines them with recorded environment capabilities such as tmux resize, derives sorted candidates, selects with a local seeded PRNG weighted toward unmet goals, performs one safe action, and polls for a semantic result. It has no model/API credentials, coordinates, arbitrary synchronization delays, or destructive TUI actions. Strict JSONL replay works across fresh generated clusters with bounded structured fingerprints that canonicalize generated context, Pod, timestamp, and age data, and fails at the first divergence.

The three layers are reported independently in CI. See [`live-e2e.md`](live-e2e.md) for safety policy, resource and time bounds, trace schema, artifacts, cleanup guarantees, and Docker blast radius.

## Manual agentic exploration

`scripts/e2e/agentic.sh` remains a bounded manual environment for humans or agents. It is not the automated semantic policy and is the only mode where explicit cluster preservation is permitted.
