#!/usr/bin/env bash
set -euo pipefail

root_dir=$(cd "$(dirname "$0")/../.." && pwd)
usage() {
  printf 'usage: %s\n' "$0"
}

case "${1:-}" in
  "")
    [[ $# -eq 0 ]] || { usage >&2; exit 2; }
    ;;
  --help|-h)
    [[ $# -eq 1 ]] || { usage >&2; exit 2; }
    usage
    exit 0
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac

exec "$root_dir/scripts/e2e/run.sh" --agentic
