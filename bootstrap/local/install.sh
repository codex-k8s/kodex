#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
STEPS_DIR="${ROOT_DIR}/steps"
ENV_FILE="${1:-${ROOT_DIR}/bootstrap.env}"

# shellcheck disable=SC1091
source "${STEPS_DIR}/lib.sh"

require_root
ENV_FILE="$(cd "$(dirname "$ENV_FILE")" && pwd)/$(basename "$ENV_FILE")"
load_env_file "$ENV_FILE"
export BOOTSTRAP_ENV_FILE="$ENV_FILE"

steps=(
  "05_preflight.sh"
  "00_prepare_host.sh"
  "10_create_operator_user.sh"
  "20_install_k3s.sh"
  "25_configure_image_gc.sh"
  "30_install_network_stack.sh"
  "40_install_platform_dependencies.sh"
  "45_prepare_runtime_env.sh"
  "50_install_registry_and_builder.sh"
  "65_harden_network_firewall.sh"
  "70_finalize_and_report.sh"
)

for step in "${steps[@]}"; do
  log "Run step: $step"
  bash "${STEPS_DIR}/${step}"
done

log "Local bootstrap done"
