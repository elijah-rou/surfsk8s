#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
readonly ROOT_DIR
readonly NAMESPACE="surfsk8s-e2e"
readonly K3S_IMAGE="rancher/k3s:v1.35.1-k3s1"
readonly TOTAL_TIMEOUT_SECONDS=420
mode="scripted"
if [[ "${1:-}" == "--agentic" ]]; then
  mode="agentic"
elif [[ $# -ne 0 ]]; then
  echo "usage: $0 [--agentic]" >&2
  exit 2
fi

for command in docker kubectl go tmux python3 sha256sum timeout; do
  command -v "$command" >/dev/null 2>&1 || { echo "required command missing: $command" >&2; exit 2; }
done
timeout 15s docker info >/dev/null 2>&1 || { echo "Docker daemon is unavailable" >&2; exit 2; }

umask 077
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$-$(od -An -N3 -tx1 /dev/urandom | tr -d ' \n')"
pid_suffix=$(printf '%05d' "$(( $$ % 100000 ))")
cluster_name="surfsk8s-e2e-$(date -u +%H%M%S)-$pid_suffix-$(od -An -N2 -tx1 /dev/urandom | tr -d ' \n')"
[[ ${#cluster_name} -le 32 && "$cluster_name" =~ ^surfsk8s-e2e-[a-z0-9]([a-z0-9-]{0,16}[a-z0-9])?$ ]] || {
  echo "generated unsafe cluster name: $cluster_name" >&2
  exit 2
}

artifact_root="${SURFSK8S_E2E_ARTIFACTS:-$ROOT_DIR/artifacts/e2e/$run_id}"
mkdir -p "$artifact_root"
state_root=$(mktemp -d "${TMPDIR:-/tmp}/surfsk8s-e2e.XXXXXX")
kubeconfig="$state_root/kubeconfig"
xdg_config="$state_root/xdg-config"
binary="$state_root/surfsk8s"
session="surfsk8s-e2e-${run_id:9:12}"
mkdir -p "$xdg_config"
chmod 700 "$state_root" "$xdg_config"
export XDG_CONFIG_HOME="$xdg_config"
export KUBECONFIG="$kubeconfig"
created=0
keep_cluster=0
if [[ "${SURFSK8S_E2E_KEEP_CLUSTER:-0}" == "1" ]]; then
  [[ "$mode" == "agentic" ]] || { echo "cluster preservation is allowed only in --agentic mode" >&2; exit 2; }
  keep_cluster=1
elif [[ "${SURFSK8S_E2E_KEEP_CLUSTER:-0}" != "0" ]]; then
  echo "SURFSK8S_E2E_KEEP_CLUSTER must be exactly 0 or 1" >&2
  exit 2
fi

k3d_bin=$("$ROOT_DIR/scripts/e2e/bootstrap-k3d.sh" | tail -n 1)
[[ -x "$k3d_bin" ]] || { echo "bootstrap did not return an executable k3d path" >&2; exit 1; }
context_name="k3d-$cluster_name"

k() {
  timeout 120s kubectl --kubeconfig "$kubeconfig" --context "$context_name" \
    --namespace "$NAMESPACE" --request-timeout=15s "$@"
}

capture_diagnostics() {
  local reason=$1
  {
    echo "reason=$reason"
    echo "cluster=$cluster_name"
    echo "context=$context_name"
    echo "k3d=$($k3d_bin version 2>&1 | tr '\n' ';')"
    echo "docker=$(docker version --format '{{.Server.Version}}' 2>&1)"
  } >"$artifact_root/environment.txt"
  if [[ -s "$kubeconfig" ]]; then
    k get namespace "$NAMESPACE" -o wide >"$artifact_root/namespace.txt" 2>&1 || true
    k get pods,deployments,services,widgets -o wide >"$artifact_root/resources.txt" 2>&1 || true
    k get events --sort-by=.metadata.creationTimestamp >"$artifact_root/events.txt" 2>&1 || true
    k get crd widgets.surfsk8s.dev -o yaml >"$artifact_root/crd.yaml" 2>&1 || true
  fi
  docker ps -a --filter "label=app=k3d" --filter "name=k3d-$cluster_name" \
    --format '{{.ID}} {{.Names}} {{.Status}}' >"$artifact_root/docker-containers.txt" 2>&1 || true
  while read -r container_id _; do
    [[ -n "$container_id" ]] || continue
    timeout 10s docker logs --tail 500 "$container_id" >"$artifact_root/docker-$container_id.log" 2>&1 || true
  done <"$artifact_root/docker-containers.txt"
  tmux capture-pane -p -J -t "$session" -S - >"$artifact_root/failure.screen.txt" 2>&1 || true
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM ERR
  if (( status != 0 )); then
    capture_diagnostics "exit-$status"
  fi
  tmux kill-session -t "$session" >/dev/null 2>&1 || true
  if (( created == 1 && keep_cluster == 0 )); then
    timeout 90s "$k3d_bin" cluster delete "$cluster_name" >"$artifact_root/cluster-delete.log" 2>&1 || {
      echo "failed to delete exact E2E cluster $cluster_name; see $artifact_root/cluster-delete.log" >&2
      status=1
    }
  fi
  if (( keep_cluster == 0 )); then
    rm -rf "$state_root"
  else
    {
      echo "KEPT cluster=$cluster_name"
      echo "KUBECONFIG=$kubeconfig"
      echo "XDG_CONFIG_HOME=$xdg_config"
      echo "cleanup: $k3d_bin cluster delete $cluster_name && rm -rf $state_root"
    } | tee "$artifact_root/KEEP.txt"
  fi
  echo "E2E artifacts: $artifact_root"
  exit "$status"
}
on_error() {
  local status=$?
  trap - ERR
  exit "$status"
}

trap cleanup EXIT
trap on_error ERR
trap 'exit 130' INT
trap 'exit 143' TERM

if timeout 20s "$k3d_bin" cluster list --no-headers 2>/dev/null | awk '{print $1}' | grep -Fxq "$cluster_name"; then
  echo "refusing to reuse existing cluster: $cluster_name" >&2
  exit 1
fi

echo "creating isolated cluster $cluster_name with $K3S_IMAGE"
# From this point cleanup owns the exact generated name, including partial creates.
created=1
timeout 150s "$k3d_bin" cluster create "$cluster_name" \
  --image "$K3S_IMAGE" \
  --servers 1 --agents 0 \
  --wait --timeout 120s \
  --kubeconfig-update-default=false \
  --kubeconfig-switch-context=false \
  >"$artifact_root/cluster-create.log" 2>&1

timeout 20s "$k3d_bin" kubeconfig get "$cluster_name" >"$kubeconfig"
chmod 600 "$kubeconfig"
[[ $(stat -c '%a' "$kubeconfig") == "600" ]] || { echo "isolated kubeconfig mode is not 0600" >&2; exit 1; }
[[ $(k config current-context) == "$context_name" ]] || { echo "unexpected isolated kubeconfig context" >&2; exit 1; }

api_deadline=$((SECONDS + 60))
until k get --raw=/readyz >/dev/null 2>&1; do
  (( SECONDS < api_deadline )) || { echo "Kubernetes API readiness deadline exceeded" >&2; exit 1; }
  sleep 1
done
k apply -f "$ROOT_DIR/scripts/e2e/crd.yaml" >"$artifact_root/crd-apply.log"
k wait --for=condition=Established crd/widgets.surfsk8s.dev --timeout=45s
k apply -f "$ROOT_DIR/scripts/e2e/fixtures.yaml" >"$artifact_root/fixture-apply.log"
k wait --for=condition=Ready pod/log-marker --timeout=90s
k rollout status deployment/scalable --timeout=90s
k get widget alpha -o jsonpath='{.spec.color}{" "}{.status.phase}{"\n"}' | grep -Fx 'blue Stable'

echo "building surfsk8s once"
timeout 120s go build -trimpath -o "$binary" "$ROOT_DIR"

{
  "$k3d_bin" version
  k version
  go version
  tmux -V
  echo "cluster=$cluster_name"
  echo "context=$context_name"
  echo "k3s_image=$K3S_IMAGE"
  echo "mode=$mode"
} >"$artifact_root/environment.txt"

if [[ "$mode" == "scripted" ]]; then
  timeout "$TOTAL_TIMEOUT_SECONDS" python3 "$ROOT_DIR/scripts/e2e/pty_driver.py" \
    --binary "$binary" --kubeconfig "$kubeconfig" --context "$context_name" \
    --namespace "$NAMESPACE" --cluster "$cluster_name" --session "$session" \
    --artifacts "$artifact_root"
else
  ttl="${SURFSK8S_AGENTIC_TTL_SECONDS:-1800}"
  if [[ ! "$ttl" =~ ^[1-9][0-9]{0,4}$ ]] || (( ttl > 86400 )); then
    echo "SURFSK8S_AGENTIC_TTL_SECONDS must be 1..86400" >&2
    exit 2
  fi
  raw_log="$artifact_root/agentic-raw-terminal.log"
  raw_log_quoted=$(printf '%q' "$raw_log")
  tmux new-session -d -s "$session" -x 140 -y 36 \
    env TERM=xterm-256color XDG_CONFIG_HOME="$xdg_config" \
    "$binary" -kubeconfig "$kubeconfig" -context "$context_name" -namespace "$NAMESPACE"
  tmux pipe-pane -t "$session" -o "cat >> $raw_log_quoted"
  cat <<EOF
Agentic E2E is live for at most ${ttl}s.
State: cluster=$cluster_name session=$session artifacts=$artifact_root
Inspect: tmux capture-pane -p -J -t '$session' -S -
Send text: tmux send-keys -t '$session' -l 'pods'
Send semantic key: tmux send-keys -t '$session' Enter   # Escape, C-c, Up also work
Attach (optional): tmux attach -t '$session'
End UI: tmux send-keys -t '$session' q
Cleanup now: $k3d_bin cluster delete '$cluster_name'
The wrapper cleans up when the UI exits, on signals, or at the TTL. Set SURFSK8S_E2E_KEEP_CLUSTER=1 before launch only for explicit preservation.
EOF
  deadline=$((SECONDS + ttl))
  while tmux has-session -t "$session" 2>/dev/null && (( SECONDS < deadline )); do
    sleep 1
  done
  if tmux has-session -t "$session" 2>/dev/null; then
    echo "agentic TTL reached; stopping session" >&2
    tmux kill-session -t "$session"
  fi
fi
