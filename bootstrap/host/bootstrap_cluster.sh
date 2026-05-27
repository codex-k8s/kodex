#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PROJECT_ROOT="$(cd "${ROOT_DIR}/.." && pwd)"
ENV_FILE="${KODEX_BOOTSTRAP_CONFIG_FILE:-${ROOT_DIR}/host/config.env}"
ACTION="preflight"
DRY_RUN="false"

log() { echo "[$(date -Is)] $*"; }
die() { echo "ERROR: $*" >&2; exit 1; }

usage() {
  cat <<'USAGE'
Usage: bootstrap/host/bootstrap_cluster.sh <preflight|install> [options]

Options:
  --env-file PATH           Bootstrap env file. Defaults to bootstrap/host/config.env.
  --dry-run                 Run local preflight and print the install plan without install steps.
  -h, --help                Show this help.

This installer is local-on-server only. It never prints env values. Domains,
tokens, keys, emails and kubeconfig values are treated as sensitive.
USAGE
}

while (($# > 0)); do
  case "$1" in
    preflight|install)
      ACTION="$1"
      shift
      ;;
    --env-file)
      ENV_FILE="${2:-}"
      shift 2
      ;;
    --dry-run)
      DRY_RUN="true"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --mode)
      case "${2:-}" in
        local)
          shift 2
          ;;
        remote|"")
          die "Remote SSH bootstrap mode is not supported; run this script on the target server"
          ;;
        *)
          die "--mode only supports local bootstrap"
          ;;
      esac
      ;;
    --skip-ssh)
      die "Remote SSH bootstrap mode is not supported; run this script on the target server"
      ;;
    *)
      die "Unknown argument: $1"
      ;;
  esac
done

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Missing required command: $1"
}

abs_path() {
  local path="$1"
  local dir base
  dir="$(dirname "$path")"
  base="$(basename "$path")"
  printf '%s/%s\n' "$(cd "$dir" && pwd)" "$base"
}

load_env() {
  [ -f "$ENV_FILE" ] || die "Env file not found"
  ENV_FILE="$(abs_path "$ENV_FILE")"
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
}

check_local_files() {
  [ -f "${PROJECT_ROOT}/services.yaml" ] || die "services.yaml is missing"
  [ -f "${ROOT_DIR}/local/install.sh" ] || die "local install script is missing"
  [ -f "${ROOT_DIR}/local/steps/05_preflight.sh" ] || die "local preflight script is missing"
  [ -d "${PROJECT_ROOT}/deploy/base/bootstrap-foundation" ] || die "bootstrap foundation manifests are missing"
  [ -d "${PROJECT_ROOT}/deploy/base/bootstrap-builder-smoke" ] || die "bootstrap builder smoke manifests are missing"
}

pack_repo() {
  local output="$1"
  tar \
    --exclude='.git' \
    --exclude='.local' \
    --exclude='bootstrap/host/*.env' \
    --exclude='services/staff/web-console/node_modules' \
    --exclude='services/staff/web-console/dist' \
    -czf "$output" \
    -C "$PROJECT_ROOT" \
    .
}

print_plan() {
  log "Dry-run only: cluster installation is not executed"
  log "Plan: preflight -> prepare host -> operator user -> k3s -> image GC -> network prerequisites -> repo sync -> runtime env -> registry foundation -> firewall -> report"
  log "After install: run bootstrap/host/smoke_registry_kaniko.sh, then bootstrap/host/smoke_backend_contour.sh when backend images are available"
}

run_preflight() {
  KODEX_BOOTSTRAP_MODE="local" BOOTSTRAP_ENV_FILE="$ENV_FILE" bash "${ROOT_DIR}/local/steps/05_preflight.sh"
}

run_install() {
  require_cmd tar
  local tmp_dir repo_archive
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' RETURN
  repo_archive="${tmp_dir}/repo-src.tgz"
  log "Pack repository snapshot for local bootstrap"
  pack_repo "$repo_archive"
  if [ "${EUID}" -eq 0 ]; then
    KODEX_BOOTSTRAP_REPO_ARCHIVE="$repo_archive" KODEX_BOOTSTRAP_MODE="local" bash "${ROOT_DIR}/local/install.sh" "$ENV_FILE"
  else
    require_cmd sudo
    sudo env KODEX_BOOTSTRAP_REPO_ARCHIVE="$repo_archive" KODEX_BOOTSTRAP_MODE="local" bash "${ROOT_DIR}/local/install.sh" "$ENV_FILE"
  fi
}

check_local_files
load_env

if [ "$DRY_RUN" = "true" ]; then
  run_preflight
  print_plan
  exit 0
fi

case "$ACTION" in
  preflight) run_preflight ;;
  install) run_install ;;
  *) die "Unsupported action: ${ACTION}" ;;
esac
