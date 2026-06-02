#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ENV_FILE="${KODEX_BACKEND_DEPLOY_ENV_FILE:-${PROJECT_ROOT}/bootstrap/host/config.env}"
RING="${KODEX_BACKEND_DEPLOY_RING:-first}"
SKIP_BUILD="false"
SKIP_HEALTH="false"

die() { echo "ERROR: $*" >&2; exit 1; }
require_cmd() { command -v "$1" >/dev/null 2>&1 || die "Missing required command: $1"; }

usage() {
  cat <<'USAGE'
Usage: bootstrap/host/deploy_backend_ring.sh [options]

Options:
  --env-file PATH  Bootstrap env file. Defaults to bootstrap/host/config.env.
  --ring NAME      Deploy ring: first, second, staff, mcp, web, web-public, or all. Defaults to first.
  --skip-build     Do not run Kaniko image build jobs.
  --skip-health    Do not run HTTP readiness checks after rollout.
  -h, --help       Show this help.

The command applies backend rings locally on the current Kubernetes server.
It does not print env values, secret values, domains or registry addresses.
USAGE
}

while (($# > 0)); do
  case "$1" in
    --env-file)
      ENV_FILE="${2:-}"
      shift 2
      ;;
    --ring)
      RING="${2:-}"
      shift 2
      ;;
    --skip-build)
      SKIP_BUILD="true"
      shift
      ;;
    --skip-health)
      SKIP_HEALTH="true"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "Unknown argument: $1"
      ;;
  esac
done

[ -f "$ENV_FILE" ] || die "Env file not found"
require_cmd go
require_cmd kubectl

args=(
  --repo-root "$PROJECT_ROOT"
  --env-file "$ENV_FILE"
  --services-file "${PROJECT_ROOT}/services.yaml"
  --ring "$RING"
)
if [ "$SKIP_BUILD" = "true" ]; then
  args+=(--skip-build)
fi
if [ "$SKIP_HEALTH" = "true" ]; then
  args+=(--skip-health)
fi

(cd "$PROJECT_ROOT" && go run ./cmd/bootstrap-backend-deploy "${args[@]}")
