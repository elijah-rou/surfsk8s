#!/usr/bin/env bash
set -uo pipefail

config=${1:-}
if [[ -n "$config" && "$(basename "$config")" == "ownership.env" && "$(basename "$(dirname "$config")")" == surfsk8s-e2e.* && ! -e "$(dirname "$config")" ]]; then
  exit 0
fi
[[ -n "$config" && -f "$config" ]] || { echo "usage: $0 /path/to/ownership.env" >&2; exit 2; }
tmux_socket=""
tmux_socket_path=""
session=""
k3d_bin=""
cluster_name=""
kubeconfig=""
state_root=""
state_parent=""
docker_network=""
docker_images_volume=""
# The wrapper creates this mode-0600 file inside its private state directory.
# shellcheck source=/dev/null
source "$config"

required=(tmux_socket tmux_socket_path session k3d_bin cluster_name kubeconfig state_root state_parent docker_network docker_images_volume)
for name in "${required[@]}"; do
  [[ -n "${!name:-}" ]] || { echo "cleanup ownership field is empty: $name" >&2; exit 2; }
done
[[ "$cluster_name" =~ ^surfsk8s-e2e-[a-z0-9]([a-z0-9-]{0,16}[a-z0-9])?$ ]] || { echo "refusing unsafe cluster name: $cluster_name" >&2; exit 2; }
[[ "$tmux_socket" =~ ^surfsk8s-e2e-[A-Za-z0-9_-]{1,48}$ ]] || { echo "refusing unsafe tmux socket: $tmux_socket" >&2; exit 2; }
[[ "$session" =~ ^surfsk8s-e2e-[A-Za-z0-9_-]{1,48}$ ]] || { echo "refusing unsafe tmux session: $session" >&2; exit 2; }
[[ "$(dirname "$state_root")" == "$state_parent" && "$(basename "$state_root")" == surfsk8s-e2e.* ]] || { echo "refusing unsafe state path: $state_root" >&2; exit 2; }
[[ "$config" == "$state_root/ownership.env" && "$kubeconfig" == "$state_root/kubeconfig" ]] || { echo "refusing ownership paths outside state root" >&2; exit 2; }
[[ "$(basename "$tmux_socket_path")" == "$tmux_socket" ]] || { echo "refusing unsafe tmux socket path: $tmux_socket_path" >&2; exit 2; }
[[ "$docker_network" == "k3d-$cluster_name" ]] || { echo "refusing unexpected Docker network: $docker_network" >&2; exit 2; }
[[ "$docker_images_volume" == "k3d-$cluster_name-images" ]] || { echo "refusing unexpected Docker volume: $docker_images_volume" >&2; exit 2; }
[[ -x "$k3d_bin" ]] || { echo "owned k3d binary is unavailable: $k3d_bin" >&2; exit 1; }

lock_timeout=${SURFSK8S_CLEANUP_LOCK_TIMEOUT_SECONDS:-20}
if [[ ! "$lock_timeout" =~ ^[1-9][0-9]?$ ]] || (( lock_timeout > 60 )); then
  echo "SURFSK8S_CLEANUP_LOCK_TIMEOUT_SECONDS must be 1..60" >&2
  exit 2
fi
[[ -e "$state_root" ]] || exit 0
exec 9>"$state_root/.cleanup.lock" || {
  [[ ! -e "$state_root" ]] && exit 0
  echo "cannot open cleanup lock in $state_root" >&2
  exit 1
}
if ! flock -w "$lock_timeout" 9; then
  printf 'cleanup is already running; retry cleanup: %q %q\n' "$0" "$config" >&2
  exit 1
fi
[[ -e "$state_root" ]] || exit 0

failure=0
fail() {
  echo "$1" >&2
  failure=1
}
retry_message() {
  printf 'owned cleanup incomplete; recovery state retained. Retry cleanup: %q %q\n' "$0" "$config" >&2
}
process_start_time() {
  local pid=$1
  local stat
  local -a fields
  IFS= read -r stat <"/proc/$pid/stat" || return 1
  stat=${stat##*) }
  read -r -a fields <<<"$stat"
  (( ${#fields[@]} >= 20 )) || return 1
  [[ "${fields[19]}" =~ ^[1-9][0-9]*$ ]] || return 1
  printf '%s\n' "${fields[19]}"
}

# Stop only the dedicated server and session. The exact socket path is removed
# only after the dedicated server has been asked to exit.
timeout 10s tmux -L "$tmux_socket" kill-session -t "$session" >/dev/null 2>&1 || true
timeout 10s tmux -L "$tmux_socket" kill-server >/dev/null 2>&1 || true
rm -f -- "$tmux_socket_path"
if timeout 5s tmux -L "$tmux_socket" has-session -t "$session" >/dev/null 2>&1; then
  fail "owned tmux session still exists: $tmux_socket/$session"
fi
[[ ! -e "$tmux_socket_path" ]] || fail "owned tmux socket still exists: $tmux_socket_path"

for identity in "$state_root/tmux-server.identity" "$state_root/tmux-pane.identity"; do
  [[ -f "$identity" ]] || continue
  read -r pid start_time <"$identity" || { fail "invalid owned process identity: $identity"; continue; }
  if [[ ! "$pid" =~ ^[1-9][0-9]*$ || ! "$start_time" =~ ^[1-9][0-9]*$ ]]; then
    fail "invalid owned process identity: $identity"
    continue
  fi
  if [[ -r "/proc/$pid/stat" ]]; then
    if ! current_start_time=$(process_start_time "$pid"); then
      fail "cannot verify owned process identity: pid=$pid identity=$identity"
    elif [[ "$current_start_time" == "$start_time" ]]; then
      fail "owned process still running: pid=$pid identity=$identity"
    fi
  fi
done

cluster_list="$state_root/k3d-clusters.after.txt"
if ! timeout 20s "$k3d_bin" cluster list --no-headers >"$cluster_list" 2>"$state_root/k3d-list.stderr"; then
  fail "cannot determine whether owned k3d cluster exists: $cluster_name"
elif awk '{print $1}' "$cluster_list" | grep -Fxq "$cluster_name"; then
  if ! timeout 90s env KUBECONFIG="$kubeconfig" "$k3d_bin" cluster delete "$cluster_name"; then
    fail "failed to delete owned k3d cluster: $cluster_name"
  fi
fi

# A successful delete is not proof. Probe k3d again and independently verify
# every exact Docker name this run can own before removing recovery state.
if (( failure == 0 )); then
  if ! timeout 20s "$k3d_bin" cluster list --no-headers >"$cluster_list" 2>"$state_root/k3d-list.stderr"; then
    fail "post-cleanup k3d cluster probe failed: $cluster_name"
  elif awk '{print $1}' "$cluster_list" | grep -Fxq "$cluster_name"; then
    fail "owned k3d cluster still exists: $cluster_name"
  fi
fi

container_list="$state_root/docker-containers.after.txt"
network_list="$state_root/docker-networks.after.txt"
volume_list="$state_root/docker-volumes.after.txt"
if ! timeout 20s docker ps -a --filter "name=k3d-$cluster_name-" --format '{{.Names}}' >"$container_list"; then
  fail "cannot verify owned Docker containers for $cluster_name"
elif grep -Eq "^k3d-${cluster_name}-(server-[0-9]+|agent-[0-9]+|serverlb|tools)$" "$container_list"; then
  fail "owned Docker containers remain for $cluster_name"
fi
if ! timeout 20s docker network ls --filter "name=^${docker_network}$" --format '{{.Name}}' >"$network_list"; then
  fail "cannot verify owned Docker network $docker_network"
elif grep -Fxq "$docker_network" "$network_list"; then
  fail "owned Docker network remains: $docker_network"
fi
if ! timeout 20s docker volume ls --filter "name=^${docker_images_volume}$" --format '{{.Name}}' >"$volume_list"; then
  fail "cannot verify owned Docker volume $docker_images_volume"
elif grep -Fxq "$docker_images_volume" "$volume_list"; then
  fail "owned Docker volume remains: $docker_images_volume"
fi

if (( failure != 0 )); then
  retry_message
  exit 1
fi

rm -rf -- "$state_root"
if [[ -e "$state_root" ]]; then
  echo "failed to remove owned state root: $state_root" >&2
  retry_message
  exit 1
fi
exit 0
