#!/usr/bin/env bash
set -euo pipefail

# Скрипт применяется SRE только после merge/review и отдельного owner OK.
# VAULT_ADDR/VAULT_CACERT/VAULT_TOKEN задаются approved runtime окружением;
# значения не выводятся и не сохраняются в репозитории.
for command_name in vault; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Required command is unavailable: ${command_name}" >&2
    exit 1
  fi
done

: "${VAULT_ADDR:?VAULT_ADDR is required}"
: "${VAULT_CACERT:?VAULT_CACERT is required}"
: "${VAULT_TOKEN:?VAULT_TOKEN is required}"

auth_mount="${INTERACTION_GATEWAY_VAULT_AUTH_MOUNT:-kubernetes}"
policy_name="interaction-gateway-agent-bot-credential"
role_name="interaction-gateway-agent-bot-credential"

vault policy write "${policy_name}" - >/dev/null <<'HCL'
path "kv/data/mattercodex/interaction-gateway/agent-bot-identities/*" {
  capabilities = ["create", "read", "update"]
}
HCL

vault write "auth/${auth_mount}/role/${role_name}" \
  bound_service_account_names="interaction-gateway" \
  bound_service_account_namespaces="mattercodex-system" \
  audience="vault" \
  token_policies="${policy_name}" \
  token_no_default_policy="true" \
  token_type="service" \
  token_ttl="10m" \
  token_max_ttl="30m" >/dev/null

vault read "auth/${auth_mount}/role/${role_name}" >/dev/null
vault policy read "${policy_name}" >/dev/null
echo "Vault Agent bot credential policy and Kubernetes role are configured."
