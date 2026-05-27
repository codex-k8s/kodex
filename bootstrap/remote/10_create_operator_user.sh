#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck disable=SC1091
source "${ROOT_DIR}/lib.sh"
load_env_file "${BOOTSTRAP_ENV_FILE:?}"

require_root

: "${OPERATOR_USER:?OPERATOR_USER is required}"
KODEX_BOOTSTRAP_MODE="${KODEX_BOOTSTRAP_MODE:-local}"
case "$KODEX_BOOTSTRAP_MODE" in
  local|remote) ;;
  *) die "KODEX_BOOTSTRAP_MODE must be local or remote";;
esac
if [ -z "${OPERATOR_SSH_PUBKEY:-}" ] && [ -n "${OPERATOR_SSH_PUBKEY_PATH:-}" ] && [ -f "${OPERATOR_SSH_PUBKEY_PATH}" ]; then
  OPERATOR_SSH_PUBKEY="$(cat "${OPERATOR_SSH_PUBKEY_PATH}")"
fi
if [ -z "${OPERATOR_SSH_PUBKEY:-}" ] && [ "$KODEX_BOOTSTRAP_MODE" = "remote" ]; then
  die "OPERATOR_SSH_PUBKEY is required for remote mode"
fi

if ! id -u "$OPERATOR_USER" >/dev/null 2>&1; then
  log "Create operator user: $OPERATOR_USER"
  adduser --disabled-password --gecos "" "$OPERATOR_USER"
fi

usermod -aG sudo "$OPERATOR_USER"

if [ -n "${OPERATOR_SSH_PUBKEY:-}" ]; then
  install -d -m 700 "/home/${OPERATOR_USER}/.ssh"
  printf '%s\n' "$OPERATOR_SSH_PUBKEY" > "/home/${OPERATOR_USER}/.ssh/authorized_keys"
  chmod 600 "/home/${OPERATOR_USER}/.ssh/authorized_keys"
  chown -R "${OPERATOR_USER}:${OPERATOR_USER}" "/home/${OPERATOR_USER}/.ssh"

  # Keep root key login but disallow root password login.
  if grep -qE '^#?PermitRootLogin' /etc/ssh/sshd_config; then
    sed -ri 's/^#?PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
  else
    echo 'PermitRootLogin prohibit-password' >> /etc/ssh/sshd_config
  fi
  systemctl reload ssh || systemctl reload sshd || true
else
  log "Operator SSH public key is not configured; skip authorized_keys provisioning in local mode"
fi
