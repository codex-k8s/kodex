#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PROJECT_ROOT="$(cd "${ROOT_DIR}/.." && pwd)"
ENV_FILE="${KODEX_BOOTSTRAP_CONFIG_FILE:-${ROOT_DIR}/host/config.env}"
ACTION="preflight"
MODE="${KODEX_BOOTSTRAP_MODE:-local}"
DRY_RUN="false"
SKIP_SSH="false"

log() { echo "[$(date -Is)] $*"; }
die() { echo "ERROR: $*" >&2; exit 1; }

usage() {
  cat <<'USAGE'
Usage: bootstrap/host/bootstrap_cluster.sh <preflight|install> [options]

Options:
  --mode local|remote       Run on this server or through TARGET_* SSH.
  --env-file PATH           Bootstrap env file. Defaults to bootstrap/host/config.env.
  --dry-run                 Run mode-specific preflight and print the install plan without install steps.
  --skip-ssh                Skip SSH reachability check in remote preflight.
  -h, --help                Show this help.

The script never prints env values. TARGET_*, domains, tokens, keys and emails are treated as sensitive.
USAGE
}

while (($# > 0)); do
  case "$1" in
    preflight|install)
      ACTION="$1"
      shift
      ;;
    --mode)
      MODE="${2:-}"
      shift 2
      ;;
    --env-file)
      ENV_FILE="${2:-}"
      shift 2
      ;;
    --dry-run)
      DRY_RUN="true"
      shift
      ;;
    --skip-ssh)
      SKIP_SSH="true"
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

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Missing required command: $1"
}

load_env() {
  [ -f "$ENV_FILE" ] || die "Env file not found"
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
}

check_local_files() {
  [ -f "${PROJECT_ROOT}/services.yaml" ] || die "services.yaml is missing"
  [ -f "${ROOT_DIR}/remote/bootstrap_production.sh" ] || die "remote bootstrap script is missing"
  [ -f "${ROOT_DIR}/remote/05_preflight.sh" ] || die "remote preflight script is missing"
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

prepare_env_copy() {
  local output="$1"
  cp "$ENV_FILE" "$output"
  if [ -n "${OPERATOR_SSH_PUBKEY_PATH:-}" ] && [ -f "$OPERATOR_SSH_PUBKEY_PATH" ]; then
    local pubkey escaped
    pubkey="$(cat "$OPERATOR_SSH_PUBKEY_PATH")"
    escaped="$(printf '%s' "$pubkey" | sed "s/'/'\\\\''/g")"
    printf "\nOPERATOR_SSH_PUBKEY='%s'\n" "$escaped" >> "$output"
  fi
}

print_plan() {
  log "Dry-run only: cluster installation is not executed"
  log "Plan: preflight -> prepare host -> operator user -> k3s -> image GC -> network prerequisites -> repo sync -> runtime env -> registry foundation -> firewall -> report"
  log "After install: run bootstrap/host/smoke_registry_kaniko.sh, then bootstrap/host/smoke_backend_contour.sh when backend images are available"
}

run_local_preflight() {
  KODEX_BOOTSTRAP_MODE="local" BOOTSTRAP_ENV_FILE="$ENV_FILE" bash "${ROOT_DIR}/remote/05_preflight.sh"
}

run_local_install() {
  require_cmd tar
  local tmp_dir repo_archive
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' RETURN
  repo_archive="${tmp_dir}/repo-src.tgz"
  log "Pack repository snapshot for local bootstrap"
  pack_repo "$repo_archive"
  if [ "${EUID}" -eq 0 ]; then
    KODEX_REMOTE_REPO_ARCHIVE="$repo_archive" KODEX_BOOTSTRAP_MODE="local" bash "${ROOT_DIR}/remote/bootstrap_production.sh" "$ENV_FILE"
  else
    require_cmd sudo
    sudo env KODEX_REMOTE_REPO_ARCHIVE="$repo_archive" KODEX_BOOTSTRAP_MODE="local" bash "${ROOT_DIR}/remote/bootstrap_production.sh" "$ENV_FILE"
  fi
}

remote_dir() {
  if [ -n "${KODEX_REMOTE_BOOTSTRAP_DIR:-}" ]; then
    printf '%s\n' "$KODEX_REMOTE_BOOTSTRAP_DIR"
  elif [ "${TARGET_ROOT_USER:-root}" = "root" ]; then
    printf '%s\n' "/root/kodex-bootstrap"
  else
    printf '%s\n' "/home/${TARGET_ROOT_USER}/kodex-bootstrap"
  fi
}

require_remote_env() {
  [ -n "${TARGET_HOST:-}" ] || die "TARGET_HOST is required for remote mode"
  [ -n "${TARGET_ROOT_USER:-root}" ] || die "TARGET_ROOT_USER is required for remote mode"
  [ -f "${TARGET_ROOT_SSH_KEY:-${HOME}/.ssh/id_rsa}" ] || die "TARGET_ROOT_SSH_KEY is not readable"
}

run_remote_preflight() {
  require_cmd ssh
  require_cmd scp
  require_remote_env
  if [ "$SKIP_SSH" = "true" ]; then
    log "Remote SSH check skipped"
    return
  fi
  local remote_bootstrap_dir remote_env tmp_dir env_copy
  local ssh_base_args scp_base_args
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' RETURN
  env_copy="${tmp_dir}/bootstrap.env"
  prepare_env_copy "$env_copy"
  remote_bootstrap_dir="$(remote_dir)"
  remote_env="${remote_bootstrap_dir}/bootstrap.env"
  ssh_base_args=(-i "${TARGET_ROOT_SSH_KEY:-${HOME}/.ssh/id_rsa}" -p "${TARGET_PORT:-22}")
  scp_base_args=(-i "${TARGET_ROOT_SSH_KEY:-${HOME}/.ssh/id_rsa}" -P "${TARGET_PORT:-22}")
  log "Copy preflight scripts to remote target"
  ssh "${ssh_base_args[@]}" "${TARGET_ROOT_USER:-root}@${TARGET_HOST}" "mkdir -p '${remote_bootstrap_dir}' && chmod 700 '${remote_bootstrap_dir}'"
  scp "${scp_base_args[@]}" -r "${ROOT_DIR}/remote" "${TARGET_ROOT_USER:-root}@${TARGET_HOST}:${remote_bootstrap_dir}/"
  scp "${scp_base_args[@]}" "$env_copy" "${TARGET_ROOT_USER:-root}@${TARGET_HOST}:${remote_env}"
  log "Run remote preflight"
  ssh "${ssh_base_args[@]}" "${TARGET_ROOT_USER:-root}@${TARGET_HOST}" \
    "KODEX_BOOTSTRAP_MODE='remote' BOOTSTRAP_ENV_FILE='${remote_env}' bash '${remote_bootstrap_dir}/remote/05_preflight.sh'"
}

run_remote_install() {
  require_cmd ssh
  require_cmd scp
  require_cmd tar
  require_remote_env
  local tmp_dir repo_archive env_copy remote_bootstrap_dir remote_env remote_repo_archive remote_run_prefix
  local ssh_base_args scp_base_args
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' RETURN
  repo_archive="${tmp_dir}/repo-src.tgz"
  env_copy="${tmp_dir}/bootstrap.env"
  prepare_env_copy "$env_copy"
  remote_bootstrap_dir="$(remote_dir)"
  remote_env="${remote_bootstrap_dir}/bootstrap.env"
  remote_repo_archive="${remote_bootstrap_dir}/repo-src.tgz"
  ssh_base_args=(-i "${TARGET_ROOT_SSH_KEY:-${HOME}/.ssh/id_rsa}" -p "${TARGET_PORT:-22}")
  scp_base_args=(-i "${TARGET_ROOT_SSH_KEY:-${HOME}/.ssh/id_rsa}" -P "${TARGET_PORT:-22}")
  remote_run_prefix=""
  if [ "${TARGET_ROOT_USER:-root}" != "root" ]; then
    remote_run_prefix="sudo "
  fi

  log "Pack repository snapshot for remote bootstrap"
  pack_repo "$repo_archive"
  log "Copy bootstrap bundle to remote target"
  ssh "${ssh_base_args[@]}" "${TARGET_ROOT_USER:-root}@${TARGET_HOST}" "mkdir -p '${remote_bootstrap_dir}' && chmod 700 '${remote_bootstrap_dir}'"
  scp "${scp_base_args[@]}" -r "${ROOT_DIR}/remote" "${TARGET_ROOT_USER:-root}@${TARGET_HOST}:${remote_bootstrap_dir}/"
  scp "${scp_base_args[@]}" "$env_copy" "${TARGET_ROOT_USER:-root}@${TARGET_HOST}:${remote_env}"
  scp "${scp_base_args[@]}" "$repo_archive" "${TARGET_ROOT_USER:-root}@${TARGET_HOST}:${remote_repo_archive}"

  log "Run remote bootstrap"
  ssh "${ssh_base_args[@]}" "${TARGET_ROOT_USER:-root}@${TARGET_HOST}" \
    "${remote_run_prefix}env KODEX_BOOTSTRAP_MODE='remote' KODEX_REMOTE_REPO_ARCHIVE='${remote_repo_archive}' bash '${remote_bootstrap_dir}/remote/bootstrap_production.sh' '${remote_env}'"
}

case "$MODE" in
  local|remote) ;;
  *) die "--mode must be local or remote";;
esac

check_local_files
load_env

if [ "$DRY_RUN" = "true" ]; then
  case "$MODE" in
    local) run_local_preflight ;;
    remote) run_remote_preflight ;;
  esac
  print_plan
  exit 0
fi

case "${ACTION}:${MODE}" in
  preflight:local) run_local_preflight ;;
  preflight:remote) run_remote_preflight ;;
  install:local) run_local_install ;;
  install:remote) run_remote_install ;;
  *) die "Unsupported action/mode: ${ACTION}/${MODE}" ;;
esac
