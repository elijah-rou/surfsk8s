#!/usr/bin/env bash
set -euo pipefail

root_dir=$(cd "$(dirname "$0")/.." && pwd)
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

context_name="${1:-}"
mode=""
kubeconfig_path=""
created_kind_cluster=""
live_context_source=""
apply_fixture="false"

is_context_alive() {
  local ctx=$1
  kubectl --context "$ctx" get ns --request-timeout=4s >/dev/null 2>&1
}

pick_local_context() {
  kubectl config get-contexts -o name 2>/dev/null | grep -E '^(kind-|minikube$|orbstack$|docker-desktop$|rancher-desktop$|k3d-)' || true
}

can_use_docker() {
  docker version --format '{{.Server.Version}}' >/dev/null 2>&1
}

create_kind_cluster() {
  local cluster_name="surfsk8s-smoke"
  if kind get clusters 2>/dev/null | grep -qx "$cluster_name"; then
    created_kind_cluster=""
  else
    kind create cluster --name "$cluster_name" --wait 60s >/dev/null
    created_kind_cluster="$cluster_name"
  fi
  context_name="kind-$cluster_name"
  mode="live"
  live_context_source="kind-created"
  apply_fixture="true"
}

write_fake_kubeconfig() {
  kubeconfig_path="$tmp_dir/kubeconfig"
  cat >"$kubeconfig_path" <<'EOF'
apiVersion: v1
kind: Config
current-context: dev
clusters:
- cluster:
    server: https://example.invalid
  name: dev-cluster
- cluster:
    server: https://example.invalid
  name: prod-cluster
contexts:
- context:
    cluster: dev-cluster
    user: dev-user
  name: dev
- context:
    cluster: prod-cluster
    user: prod-user
  name: prod
users:
- name: dev-user
  user:
    token: test
- name: prod-user
  user:
    token: test
EOF
  context_name="dev"
  mode="fake"
}

if [[ -n "$context_name" ]]; then
  if is_context_alive "$context_name"; then
    mode="live"
    live_context_source="explicit"
  else
    echo "context '$context_name' unreachable; falling back" >&2
    context_name=""
  fi
fi

if [[ -z "$context_name" ]]; then
  while IFS= read -r candidate; do
    [[ -z "$candidate" ]] && continue
    if is_context_alive "$candidate"; then
      context_name="$candidate"
      mode="live"
      live_context_source="existing"
      break
    fi
  done < <(pick_local_context)
fi

if [[ -z "$context_name" ]] && can_use_docker; then
  create_kind_cluster
fi

if [[ "$mode" == "live" ]]; then
  kubeconfig_path="${KUBECONFIG:-$HOME/.kube/config}"
  if [[ "$apply_fixture" == "true" ]]; then
    kubectl --context "$context_name" apply -f "$root_dir/scripts/smoke-fixture.yaml" >/dev/null
  fi
  cleanup_live() {
    if [[ "$apply_fixture" == "true" ]]; then
      kubectl --context "$context_name" delete namespace surfsk8s-smoke --ignore-not-found >/dev/null 2>&1 || true
    fi
    if [[ -n "$created_kind_cluster" ]]; then
      kind delete cluster --name "$created_kind_cluster" >/dev/null 2>&1 || true
    fi
  }
  trap 'cleanup_live; rm -rf "$tmp_dir"' EXIT
else
  write_fake_kubeconfig
fi

raw_output="$tmp_dir/raw.txt"
sanitized_output="$tmp_dir/sanitized.txt"
cd "$root_dir"
set +e
SURFSMOKE_MODE="$mode" \
SURFSMOKE_CONTEXT="$context_name" \
SURFSMOKE_FIXTURE="$apply_fixture" \
KUBECONFIG_PATH="$kubeconfig_path" \
ROOT_DIR="$root_dir" \
expect "$root_dir/scripts/smoke.expect" >"$raw_output" 2>&1
expect_rc=$?
set -e

python3 - <<'PY' "$raw_output" "$sanitized_output"
import re, sys
raw_path, out_path = sys.argv[1], sys.argv[2]
data = open(raw_path, 'rb').read().decode('utf-8', 'ignore')
data = re.sub(r'\x1b\[[0-9;?]*[ -/]*[@-~]', '', data)
data = data.replace('\r', '\n')
open(out_path, 'w').write(data)
print(data[-6000:])
PY

if [[ $expect_rc -ne 0 ]]; then
  echo "smoke: expect failed rc=$expect_rc" >&2
  exit $expect_rc
fi

if [[ "$mode" == "live" ]]; then
  grep -q "surfsk8s · resource catalog" "$sanitized_output"
  grep -q "surfsk8s · ns:all · pods" "$sanitized_output"
  grep -q "surfsk8s · ns:all · deployments" "$sanitized_output"
  grep -q "surfsk8s · ns:all · services" "$sanitized_output"
  grep -q "surfsk8s · ns:cluster · nodes" "$sanitized_output"
  grep -q "surfsk8s · CRDs" "$sanitized_output"
  grep -q "sort:" "$sanitized_output"
  grep -q "\*service\*serving.knative.dev\*" "$sanitized_output"
  grep -q "surfsk8s · ns:all · services · serving.knative.dev" "$sanitized_output"
  grep -q "LATESTCREATED" "$sanitized_output"
  grep -q "surfsk8s · commands" "$sanitized_output"
  if [[ "$apply_fixture" == "true" ]]; then
    grep -q "surfsk8s · select container" "$sanitized_output"
    grep -q "surfsk8s · select service port" "$sanitized_output"
    grep -q "surfsk8s · confirm action" "$sanitized_output"
    grep -q "local-port>" "$sanitized_output"
  fi
  echo "smoke: live context '$context_name' ($live_context_source, fixture=$apply_fixture) OK"
else
  grep -q "surfsk8s · select contexts" "$sanitized_output"
  echo "smoke: fake kubeconfig fallback OK"
fi
