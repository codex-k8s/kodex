#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck disable=SC1091
source "${ROOT_DIR}/lib.sh"
load_env_file "${BOOTSTRAP_ENV_FILE:?}"

KODEX_INGRESS_HOST_NETWORK="${KODEX_INGRESS_HOST_NETWORK:-true}"
KODEX_NETWORK_POLICY_BASELINE="${KODEX_NETWORK_POLICY_BASELINE:-true}"

validate_bool "KODEX_INGRESS_HOST_NETWORK" "$KODEX_INGRESS_HOST_NETWORK"
validate_bool "KODEX_NETWORK_POLICY_BASELINE" "$KODEX_NETWORK_POLICY_BASELINE"

log "Network prerequisites checked"
log "Ingress controller and cert-manager are not installed by this bootstrap slice"
