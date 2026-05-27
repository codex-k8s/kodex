#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck disable=SC1091
source "${ROOT_DIR}/lib.sh"
load_env_file "${BOOTSTRAP_ENV_FILE:?}"

log "Run bootstrap preflight"

if [ -r /etc/os-release ]; then
  # shellcheck disable=SC1091
  source /etc/os-release
  case "${ID:-}" in
    ubuntu|debian) log "OS family check passed";;
    *) die "Unsupported OS family for this bootstrap slice";;
  esac
else
  die "/etc/os-release is not readable"
fi

if [ "${EUID}" -eq 0 ]; then
  log "Privilege check passed: root context"
elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then
  log "Privilege check passed: passwordless sudo available"
else
  die "Root or passwordless sudo is required for install mode"
fi

require_cmd awk
require_cmd sed
require_cmd sort
require_cmd tar
require_cmd openssl

KODEX_PRODUCTION_NAMESPACE="${KODEX_PRODUCTION_NAMESPACE:-kodex-prod}"
KODEX_INTERNAL_REGISTRY_PORT="${KODEX_INTERNAL_REGISTRY_PORT:-5000}"
KODEX_INTERNAL_REGISTRY_HOST="${KODEX_INTERNAL_REGISTRY_HOST:-127.0.0.1:${KODEX_INTERNAL_REGISTRY_PORT}}"
KODEX_KANIKO_TIMEOUT="${KODEX_KANIKO_TIMEOUT:-1800s}"
KODEX_FIREWALL_ENABLED="${KODEX_FIREWALL_ENABLED:-true}"
KODEX_INGRESS_HOST_NETWORK="${KODEX_INGRESS_HOST_NETWORK:-true}"
KODEX_BOOTSTRAP_SKIP_DNS_CHECK="${KODEX_BOOTSTRAP_SKIP_DNS_CHECK:-false}"

validate_integer "KODEX_INTERNAL_REGISTRY_PORT" "$KODEX_INTERNAL_REGISTRY_PORT"
validate_bool "KODEX_FIREWALL_ENABLED" "$KODEX_FIREWALL_ENABLED"
validate_bool "KODEX_INGRESS_HOST_NETWORK" "$KODEX_INGRESS_HOST_NETWORK"
validate_bool "KODEX_BOOTSTRAP_SKIP_DNS_CHECK" "$KODEX_BOOTSTRAP_SKIP_DNS_CHECK"

[ -n "${KODEX_PRODUCTION_NAMESPACE}" ] || die "KODEX_PRODUCTION_NAMESPACE is required"
[ -n "${KODEX_INTERNAL_REGISTRY_HOST}" ] || die "KODEX_INTERNAL_REGISTRY_HOST is required"
[ -n "${OPERATOR_USER:-}" ] || die "OPERATOR_USER is required"
if [ -z "${OPERATOR_SSH_PUBKEY:-}" ] && [ -n "${OPERATOR_SSH_PUBKEY_PATH:-}" ] && [ -f "${OPERATOR_SSH_PUBKEY_PATH}" ]; then
  OPERATOR_SSH_PUBKEY="$(cat "${OPERATOR_SSH_PUBKEY_PATH}")"
fi
[ -n "${OPERATOR_SSH_PUBKEY:-}" ] || die "OPERATOR_SSH_PUBKEY is required"

if [ -n "${KODEX_PRODUCTION_DOMAIN:-}" ] && [ "$KODEX_BOOTSTRAP_SKIP_DNS_CHECK" != "true" ]; then
  ensure_domain_resolves "$KODEX_PRODUCTION_DOMAIN"
else
  log "DNS check skipped or production domain is not configured"
fi

if command -v k3s >/dev/null 2>&1; then
  log "k3s binary present"
else
  log "k3s binary is not present yet; install step will provision it"
fi

if command -v kubectl >/dev/null 2>&1; then
  log "kubectl binary present"
else
  log "kubectl binary is not present yet; k3s install provides kubectl"
fi

if [ -s /etc/rancher/k3s/k3s.yaml ] && command -v kubectl >/dev/null 2>&1; then
  if kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml get --raw=/readyz >/dev/null 2>&1; then
    log "Kubernetes API readiness check passed"
  else
    log "Kubernetes API readiness check did not pass; install/check step will handle it"
  fi
fi

REPO_DIR="$(repo_dir)"
if [ -d "${REPO_DIR}/deploy/base/bootstrap-foundation" ]; then
  [ -f "${REPO_DIR}/deploy/base/bootstrap-foundation/kustomization.yaml.tpl" ] || die "bootstrap foundation kustomization is missing"
  [ -f "${REPO_DIR}/deploy/base/bootstrap-builder-smoke/kustomization.yaml.tpl" ] || die "bootstrap builder smoke kustomization is missing"
  log "Registry and Kaniko manifest checks passed"
else
  log "Repository snapshot is not installed yet; manifest checks are deferred"
fi

log "Bootstrap preflight passed"
