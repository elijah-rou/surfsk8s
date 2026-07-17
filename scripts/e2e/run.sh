#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
readonly ROOT_DIR
readonly NAMESPACE="surfsk8s-e2e"
readonly K3S_VERSION="v1.35.1-k3s1"
readonly K3S_IMAGE="rancher/k3s:${K3S_VERSION}@sha256:634920385dc89133d80060b3a3b2b547e734d711ef8c050e6b5c6341800d53fd"
readonly TOTAL_TIMEOUT_SECONDS=420
readonly DEFAULT_RAW_CAPTURE_MAX_BYTES=$((16 * 1024 * 1024))
mode="scripted"
if [[ "${1:-}" == "--agentic" ]]; then
  mode="agentic"
elif [[ $# -ne 0 ]]; then
  echo "usage: $0 [--agentic]" >&2
  exit 2
fi

for command in docker flock kubectl go tmux python3 sha256sum timeout; do
  command -v "$command" >/dev/null 2>&1 || { echo "required command missing: $command" >&2; exit 2; }
done
timeout 15s docker info >/dev/null 2>&1 || { echo "Docker daemon is unavailable" >&2; exit 2; }

umask 077
original_xdg_config_home=${XDG_CONFIG_HOME-}
original_kubeconfig=${KUBECONFIG-}
host_config_root=${original_xdg_config_home:-$HOME/.config}
host_preferences="$host_config_root/surfsk8s/preferences.json"
host_kubeconfig=${original_kubeconfig:-$HOME/.kube/config}
file_fingerprint() {
  local path=$1
  if [[ -f "$path" ]]; then
    sha256sum -- "$path" | awk '{print $1}'
  else
    printf 'missing\n'
  fi
}
kubeconfig_fingerprint() {
  local path
  local -a paths
  IFS=: read -r -a paths <<<"$host_kubeconfig"
  for path in "${paths[@]}"; do
    printf '%s\0%s\n' "$path" "$(file_fingerprint "$path")"
  done | sha256sum | awk '{print $1}'
}
host_preferences_before=$(file_fingerprint "$host_preferences")
host_kubeconfig_before=$(kubeconfig_fingerprint)
if [[ -n "$original_kubeconfig" ]]; then
  host_context_before=$(timeout 10s env KUBECONFIG="$original_kubeconfig" kubectl config current-context 2>/dev/null || true)
else
  host_context_before=$(timeout 10s env -u KUBECONFIG kubectl config current-context 2>/dev/null || true)
fi

run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$-$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')"
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
session="surfsk8s-e2e-${run_id:9:5}-${run_id:16:12}"
tmux_socket="surfsk8s-e2e-${run_id:9:5}-${run_id:16:12}"
tmux_socket_path="${TMUX_TMPDIR:-/tmp}/tmux-$(id -u)/$tmux_socket"
[[ "$session" =~ ^surfsk8s-e2e-[A-Za-z0-9_-]{1,48}$ ]] || { echo "generated unsafe tmux session: $session" >&2; exit 2; }
[[ "$tmux_socket" =~ ^surfsk8s-e2e-[A-Za-z0-9_-]{1,48}$ ]] || { echo "generated unsafe tmux socket: $tmux_socket" >&2; exit 2; }
mkdir -p "$xdg_config"
chmod 700 "$state_root" "$xdg_config"
export XDG_CONFIG_HOME="$xdg_config"
export KUBECONFIG="$kubeconfig"
keep_cluster=0
raw_capture_max_bytes=${SURFSK8S_E2E_RAW_CAPTURE_MAX_BYTES:-$DEFAULT_RAW_CAPTURE_MAX_BYTES}
if [[ ! "$raw_capture_max_bytes" =~ ^[1-9][0-9]{0,9}$ ]] || (( raw_capture_max_bytes > 1024 * 1024 * 1024 )); then
  echo "SURFSK8S_E2E_RAW_CAPTURE_MAX_BYTES must be 1..1073741824" >&2
  exit 2
fi
capture_helper="$ROOT_DIR/scripts/e2e/bounded_capture.py"
[[ -x "$capture_helper" ]] || { echo "bounded capture helper is not executable: $capture_helper" >&2; exit 2; }

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
cleanup_helper="$state_root/cleanup-owned-e2e.sh"
ownership_config="$state_root/ownership.env"
docker_network="k3d-$cluster_name"
docker_images_volume="k3d-$cluster_name-images"
install -m 700 "$ROOT_DIR/scripts/e2e/cleanup-owned.sh" "$cleanup_helper"
{
  printf 'tmux_socket=%q\n' "$tmux_socket"
  printf 'tmux_socket_path=%q\n' "$tmux_socket_path"
  printf 'session=%q\n' "$session"
  printf 'k3d_bin=%q\n' "$k3d_bin"
  printf 'cluster_name=%q\n' "$cluster_name"
  printf 'kubeconfig=%q\n' "$kubeconfig"
  printf 'state_root=%q\n' "$state_root"
  printf 'state_parent=%q\n' "$(dirname "$state_root")"
  printf 'docker_network=%q\n' "$docker_network"
  printf 'docker_images_volume=%q\n' "$docker_images_volume"
} >"$ownership_config"
chmod 600 "$ownership_config"
cleanup_command=$(printf '%q %q' "$cleanup_helper" "$ownership_config")

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
  tmux -L "$tmux_socket" capture-pane -p -J -t "$session" -S - >"$artifact_root/failure.screen.txt" 2>&1 || true
}

cleanup() {
  local status=$?
  local host_context_after
  local host_kubeconfig_after
  local host_preferences_after
  trap - EXIT INT TERM ERR
  if (( status != 0 )); then
    capture_diagnostics "exit-$status"
  fi
  if (( keep_cluster == 0 )); then
    if [[ -x "$cleanup_helper" ]]; then
      "$cleanup_helper" "$ownership_config" >"$artifact_root/cleanup.log" 2>&1 || {
        echo "failed to clean exact E2E ownership; see $artifact_root/cleanup.log; retry: $cleanup_command" >&2
        status=1
      }
    elif [[ -e "$state_root" ]]; then
      echo "cleanup helper is missing while recovery state remains: $state_root" >&2
      status=1
    else
      echo "owned resources were already post-verified and removed by the cleanup helper"
    fi
  else
    tmux -L "$tmux_socket" kill-session -t "$session" >/dev/null 2>&1 || true
    tmux -L "$tmux_socket" kill-server >/dev/null 2>&1 || true
    rm -f -- "$tmux_socket_path"
    {
      printf 'KEPT cluster=%q\n' "$cluster_name"
      printf 'KUBECONFIG=%q\n' "$kubeconfig"
      printf 'XDG_CONFIG_HOME=%q\n' "$xdg_config"
      printf 'cleanup: %s\n' "$cleanup_command"
    } | tee "$artifact_root/KEEP.txt"
  fi

  host_preferences_after=$(file_fingerprint "$host_preferences")
  host_kubeconfig_after=$(kubeconfig_fingerprint)
  if [[ -n "$original_kubeconfig" ]]; then
    host_context_after=$(timeout 10s env KUBECONFIG="$original_kubeconfig" kubectl config current-context 2>/dev/null || true)
  else
    host_context_after=$(timeout 10s env -u KUBECONFIG kubectl config current-context 2>/dev/null || true)
  fi
  {
    printf 'host_preferences_before=%s\n' "$host_preferences_before"
    printf 'host_preferences_after=%s\n' "$host_preferences_after"
    printf 'host_kubeconfig_before=%s\n' "$host_kubeconfig_before"
    printf 'host_kubeconfig_after=%s\n' "$host_kubeconfig_after"
    printf 'host_context_before=%q\n' "$host_context_before"
    printf 'host_context_after=%q\n' "$host_context_after"
  } >"$artifact_root/host-restoration.txt"
  if [[ "$host_preferences_after" != "$host_preferences_before" || "$host_kubeconfig_after" != "$host_kubeconfig_before" || "$host_context_after" != "$host_context_before" ]]; then
    echo "host preferences or default kubeconfig/context changed; see $artifact_root/host-restoration.txt" >&2
    status=1
  else
    echo "host preferences and default kubeconfig/context unchanged"
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

initial_cluster_list="$state_root/k3d-clusters.before.txt"
if ! timeout 20s "$k3d_bin" cluster list --no-headers >"$initial_cluster_list" 2>"$state_root/k3d-list-before.stderr"; then
  echo "cannot determine whether generated cluster name already exists: $cluster_name" >&2
  exit 1
fi
if awk '{print $1}' "$initial_cluster_list" | grep -Fxq "$cluster_name"; then
  echo "refusing to reuse existing cluster: $cluster_name" >&2
  exit 1
fi

echo "creating isolated cluster $cluster_name with $K3S_IMAGE"
# From this point cleanup owns the exact generated name, including partial creates.
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
  echo "k3s_version=$K3S_VERSION"
  echo "k3s_image=$K3S_IMAGE"
  echo "raw_capture_max_bytes=$raw_capture_max_bytes"
  echo "mode=$mode"
} >"$artifact_root/environment.txt"

if [[ "$mode" == "scripted" ]]; then
  timeout "$TOTAL_TIMEOUT_SECONDS" python3 "$ROOT_DIR/scripts/e2e/pty_driver.py" \
    --binary "$binary" --kubeconfig "$kubeconfig" --xdg-config "$xdg_config" \
    --context "$context_name" --namespace "$NAMESPACE" --cluster "$cluster_name" \
    --session "$session" --tmux-socket "$tmux_socket" --artifacts "$artifact_root" \
    --capture-helper "$capture_helper" --raw-capture-max-bytes "$raw_capture_max_bytes" \
    --process-state-dir "$state_root"
else
  ttl="${SURFSK8S_AGENTIC_TTL_SECONDS:-1800}"
  if [[ ! "$ttl" =~ ^[1-9][0-9]{0,4}$ ]] || (( ttl > 86400 )); then
    echo "SURFSK8S_AGENTIC_TTL_SECONDS must be 1..86400" >&2
    exit 2
  fi
  raw_log="$artifact_root/agentic-raw-terminal.log"
  capture_pipeline=$(printf '%q --output %q --max-bytes %q' "$capture_helper" "$raw_log" "$raw_capture_max_bytes")
  tmux -L "$tmux_socket" new-session -d -s "$session" -x 140 -y 36 \
    env TERM=xterm-256color XDG_CONFIG_HOME="$xdg_config" \
    "$binary" -kubeconfig "$kubeconfig" -context "$context_name" -namespace "$NAMESPACE"
  tmux -L "$tmux_socket" pipe-pane -t "$session" -o "$capture_pipeline"
  tmux_server_pid=$(tmux -L "$tmux_socket" display-message -p -t "$session" '#{pid}')
  tmux_pane_pid=$(tmux -L "$tmux_socket" display-message -p -t "$session" '#{pane_pid}')
  printf '%s %s\n' "$tmux_server_pid" "$(awk '{print $22}' "/proc/$tmux_server_pid/stat")" >"$state_root/tmux-server.identity"
  printf '%s %s\n' "$tmux_pane_pid" "$(awk '{print $22}' "/proc/$tmux_pane_pid/stat")" >"$state_root/tmux-pane.identity"
  printf 'Agentic E2E is live for at most %ss.\n' "$ttl"
  printf 'Cluster: %q\n' "$cluster_name"
  printf 'Kubeconfig: %q\n' "$kubeconfig"
  printf 'Context: %q\n' "$context_name"
  printf 'Namespace: %q\n' "$NAMESPACE"
  printf 'State root: %q\n' "$state_root"
  printf 'XDG_CONFIG_HOME: %q\n' "$xdg_config"
  printf 'Artifacts: %q\n' "$artifact_root"
  printf 'Raw capture cap: %s bytes\n' "$raw_capture_max_bytes"
  printf 'tmux socket/session: %q / %q\n' "$tmux_socket" "$session"
  printf 'kubectl: kubectl --kubeconfig %q --context %q --namespace %q get pods -o wide\n' "$kubeconfig" "$context_name" "$NAMESPACE"
  printf 'Inspect: tmux -L %q capture-pane -p -J -t %q -S -\n' "$tmux_socket" "$session"
  printf 'Send text: tmux -L %q send-keys -t %q -l %q\n' "$tmux_socket" "$session" pods
  printf 'Send semantic key: tmux -L %q send-keys -t %q Enter  # Escape, C-c, Up also work\n' "$tmux_socket" "$session"
  printf 'Attach (optional): tmux -L %q attach -t %q\n' "$tmux_socket" "$session"
  printf 'End UI: tmux -L %q send-keys -t %q q\n' "$tmux_socket" "$session"
  printf 'Full cleanup now: %s\n' "$cleanup_command"
  echo 'The wrapper uses the same helper on exit, signals, or TTL. Set SURFSK8S_E2E_KEEP_CLUSTER=1 before launch only for explicit preservation.'
  deadline=$((SECONDS + ttl))
  while tmux -L "$tmux_socket" has-session -t "$session" 2>/dev/null && (( SECONDS < deadline )); do
    sleep 1
  done
  if tmux -L "$tmux_socket" has-session -t "$session" 2>/dev/null; then
    echo "agentic TTL reached; stopping session" >&2
    tmux -L "$tmux_socket" kill-session -t "$session"
  fi
fi
