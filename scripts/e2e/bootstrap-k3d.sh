#!/usr/bin/env bash
set -euo pipefail

readonly K3D_VERSION="v5.8.3"
readonly K3D_LINUX_AMD64_SHA256="dbaa79a76ace7f4ca230a1ff41dc7d8a5036a8ad0309e9c54f9bf3836dbe853e"
readonly K3D_LINUX_ARM64_SHA256="0b8110f2229631af7402fb828259330985918b08fefd38b7f1b788a1c8687216"
readonly RELEASE_BASE="https://github.com/k3d-io/k3d/releases/download/${K3D_VERSION}"

if [[ -n "${K3D_BIN:-}" ]]; then
  [[ -x "$K3D_BIN" ]] || { echo "K3D_BIN is not executable: $K3D_BIN" >&2; exit 2; }
  "$K3D_BIN" version
  printf '%s\n' "$K3D_BIN"
  exit 0
fi

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64)
    artifact="k3d-linux-amd64"
    expected_sha256="$K3D_LINUX_AMD64_SHA256"
    ;;
  Linux-aarch64|Linux-arm64)
    artifact="k3d-linux-arm64"
    expected_sha256="$K3D_LINUX_ARM64_SHA256"
    ;;
  *)
    echo "unsupported k3d E2E platform: $(uname -s) $(uname -m); set K3D_BIN" >&2
    exit 2
    ;;
esac

cache_root="${SURFSK8S_E2E_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/surfsk8s-e2e}"
install_dir="$cache_root/k3d/$K3D_VERSION"
binary="$install_dir/k3d"
mkdir -p "$install_dir"
chmod 700 "$cache_root" "$cache_root/k3d" "$install_dir" 2>/dev/null || true

verify_binary() {
  [[ -f "$binary" ]] || return 1
  printf '%s  %s\n' "$expected_sha256" "$binary" | sha256sum --check --status
}

if ! verify_binary; then
  tmp_dir=$(mktemp -d "$install_dir/.download.XXXXXX")
  trap 'rm -rf "$tmp_dir"' EXIT

  download() {
    local url=$1 destination=$2
    if command -v curl >/dev/null 2>&1; then
      curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
        --connect-timeout 10 --max-time 120 --output "$destination" "$url"
    elif command -v wget >/dev/null 2>&1; then
      wget --https-only --timeout=120 --tries=2 --output-document="$destination" "$url"
    else
      echo "curl or wget is required to download pinned k3d" >&2
      return 1
    fi
  }

  download "$RELEASE_BASE/$artifact" "$tmp_dir/k3d"
  download "$RELEASE_BASE/checksums.txt" "$tmp_dir/checksums.txt"
  checksum_line=$(grep -E "^[0-9a-f]{64}  _dist/${artifact}$" "$tmp_dir/checksums.txt" || true)
  [[ "$checksum_line" == "$expected_sha256  _dist/$artifact" ]] || {
    echo "release checksum manifest did not contain the pinned $artifact checksum" >&2
    exit 1
  }
  printf '%s  %s\n' "$expected_sha256" "$tmp_dir/k3d" | sha256sum --check --status || {
    echo "downloaded k3d checksum mismatch" >&2
    exit 1
  }
  chmod 700 "$tmp_dir/k3d"
  mv -f "$tmp_dir/k3d" "$binary"
fi

verify_binary || { echo "cached k3d checksum mismatch: $binary" >&2; exit 1; }
version_output=$($binary version)
grep -Fq "k3d version $K3D_VERSION" <<<"$version_output" || {
  echo "downloaded k3d reported an unexpected version: $version_output" >&2
  exit 1
}
printf '%s\n' "$binary"
