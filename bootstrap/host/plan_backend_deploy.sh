#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ENV_FILE="${KODEX_BACKEND_PLAN_ENV_FILE:-${PROJECT_ROOT}/bootstrap/host/config.env}"
RENDER_DIR="${KODEX_BACKEND_PLAN_RENDER_DIR:-}"
REQUIRE_KUBERNETES="false"
SKIP_LIVE_KUBERNETES="${KODEX_BACKEND_PLAN_SKIP_LIVE_KUBERNETES:-false}"

die() { echo "ERROR: $*" >&2; exit 1; }
require_cmd() { command -v "$1" >/dev/null 2>&1 || die "Missing required command: $1"; }

usage() {
  cat <<'USAGE'
Usage: bootstrap/host/plan_backend_deploy.sh [options]

Options:
  --env-file PATH           Bootstrap env file. Defaults to bootstrap/host/config.env.
  --render-dir PATH         Optional empty output directory for rendered manifests.
  --require-kubernetes      Fail when live Kubernetes foundation checks are unavailable.
  --skip-live-kubernetes    Skip live kubectl checks; render and inventory checks still run.
  -h, --help                Show this help.

The command is read-only: it does not apply manifests, run jobs, push images or
print env values.
USAGE
}

while (($# > 0)); do
  case "$1" in
    --env-file)
      ENV_FILE="${2:-}"
      shift 2
      ;;
    --render-dir)
      RENDER_DIR="${2:-}"
      shift 2
      ;;
    --require-kubernetes)
      REQUIRE_KUBERNETES="true"
      shift
      ;;
    --skip-live-kubernetes)
      SKIP_LIVE_KUBERNETES="true"
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

args=(
  --repo-root "$PROJECT_ROOT"
  --env-file "$ENV_FILE"
  --services-file "${PROJECT_ROOT}/services.yaml"
)
if [ -n "$RENDER_DIR" ]; then
  args+=(--render-dir "$RENDER_DIR")
fi
if [ "$REQUIRE_KUBERNETES" = "true" ]; then
  args+=(--require-kubernetes)
fi
if [ "$SKIP_LIVE_KUBERNETES" = "true" ]; then
  args+=(--skip-live-kubernetes)
fi

(cd "$PROJECT_ROOT" && go run ./cmd/bootstrap-deploy-plan "${args[@]}")
