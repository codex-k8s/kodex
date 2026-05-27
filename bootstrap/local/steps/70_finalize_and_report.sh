#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck disable=SC1091
source "${ROOT_DIR}/lib.sh"
load_env_file "${BOOTSTRAP_ENV_FILE:?}"

KODEX_FIREWALL_ENABLED="${KODEX_FIREWALL_ENABLED:-true}"
KODEX_SSH_PORT="${KODEX_SSH_PORT:-22}"

log "Production namespace configured"
log "Internal registry endpoint configured (no auth, node loopback profile)"
if [ "$KODEX_FIREWALL_ENABLED" = "true" ] && command -v nft >/dev/null 2>&1; then
  log "Firewall policy active (public tcp ports: ${KODEX_SSH_PORT},80,443):"
  nft list table inet kodex_fw >/dev/null 2>&1 && echo "  nft table inet kodex_fw: present" || echo "  nft table inet kodex_fw: missing"
fi

log "Bootstrap finished. Recommended checks:"
log "  sudo cat /etc/rancher/k3s/registries.yaml"
log "  sudo cat /var/lib/rancher/k3s/agent/etc/kubelet.conf.d/10-kodex-image-gc.conf"
log "  sudo systemctl status kodex-image-prune.timer --no-pager"
log "  sudo nft list table inet kodex_fw"
log "  run bootstrap/host/smoke_registry_kaniko.sh with KODEX_SMOKE_ENV_FILE pointing to the generated env file"
log "  run bootstrap/host/smoke_backend_contour.sh after backend images are available in the internal registry"
