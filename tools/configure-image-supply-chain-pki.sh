#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 || ( $1 != staging && $1 != production ) ]]; then
  echo "usage: configure-image-supply-chain-pki.sh staging|production" >&2
  exit 64
fi

command -v vault >/dev/null 2>&1 || {
  echo "vault CLI is required" >&2
  exit 69
}
command -v jq >/dev/null 2>&1 || {
  echo "jq is required" >&2
  exit 69
}

: "${VAULT_ADDR:?VAULT_ADDR is required}"

configure_server_role() {
  local mount=$1
  local role=$2
  local service=$3
  vault write "${mount}/roles/${role}" \
    allowed_domains="${service},${service}.mattercodex-system.svc,${service}.mattercodex-system.svc.cluster.local" \
    allow_bare_domains=true \
    allow_subdomains=false \
    allow_glob_domains=false \
    enforce_hostnames=true \
    require_cn=true \
    server_flag=true \
    client_flag=false \
    key_type=rsa \
    key_bits=3072 \
    key_usage="DigitalSignature,KeyEncipherment" \
    ext_key_usage=ServerAuth \
    ttl=1h \
    max_ttl=2h >/dev/null
}

configure_client_role() {
  local role=$1
  local common_name=$2
  vault write "pki/roles/${role}" \
    allowed_domains="${common_name},${common_name}.mattercodex-system.svc" \
    allow_bare_domains=true \
    allow_subdomains=false \
    allow_glob_domains=false \
    enforce_hostnames=true \
    require_cn=true \
    server_flag=false \
    client_flag=true \
    key_type=rsa \
    key_bits=3072 \
    key_usage="DigitalSignature,KeyEncipherment" \
    ext_key_usage=ClientAuth \
    ttl=30m \
    max_ttl=1h >/dev/null
}

configure_server_role pki mattercodex-buildkit-server mattercodex-buildkit
configure_client_role mattercodex-buildkit-probe mattercodex-buildkit-probe
configure_client_role mattercodex-buildkit-client mattercodex-role-image-builder
configure_server_role pki-public mattercodex-image-registry-pull mattercodex-image-registry-pull
configure_server_role pki mattercodex-image-registry-push mattercodex-image-registry-push
configure_server_role pki mattercodex-image-registry-admin mattercodex-image-registry-admin
configure_server_role pki mattercodex-image-registry-promotion mattercodex-image-registry-promotion

write_issue_policy() {
  local policy=$1
  local mount=$2
  shift 2
  {
    for role in "$@"; do
      printf 'path "%s/issue/%s" { capabilities = ["update"] }\n' "$mount" "$role"
    done
  } | vault policy write "$policy" - >/dev/null
}

write_ca_policy() {
  local policy=$1
  local mount=$2
  printf 'path "%s/cert/ca" { capabilities = ["read"] }\n' "$mount" |
    vault policy write "$policy" - >/dev/null
}

write_issue_policy mattercodex-buildkit-pki-issue pki \
  mattercodex-buildkit-server mattercodex-buildkit-probe
write_ca_policy mattercodex-buildkit-pki-ca pki
write_issue_policy mattercodex-role-image-builder-pki-issue pki \
  mattercodex-buildkit-client
write_ca_policy mattercodex-role-image-builder-pki-ca pki
cat <<'HCL' | vault policy write mattercodex-image-registry-pull-pki - >/dev/null
path "pki-public/issue/mattercodex-image-registry-pull" {
  capabilities = ["update"]
}
path "kv/data/mattercodex/image-registry/pull" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write mattercodex-image-registry-push-pki - >/dev/null
path "pki/issue/mattercodex-image-registry-push" {
  capabilities = ["update"]
}
path "pki/cert/ca" {
  capabilities = ["read"]
}
path "kv/data/mattercodex/image-registry/push" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write mattercodex-image-registry-admin-pki - >/dev/null
path "pki/issue/mattercodex-image-registry-admin" {
  capabilities = ["update"]
}
path "kv/data/mattercodex/image-registry/admin" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write mattercodex-image-registry-promotion-pki - >/dev/null
path "pki/issue/mattercodex-image-registry-promotion" {
  capabilities = ["update"]
}
path "kv/data/mattercodex/image-registry/promotion" {
  capabilities = ["read"]
}
HCL

configure_kubernetes_role() {
  local role=$1
  local service_account=$2
  local policy=$3
  vault write "auth/kubernetes/role/${role}" \
    bound_service_account_names="$service_account" \
    bound_service_account_namespaces=mattercodex-system \
    token_policies="$policy" \
    token_ttl=30m \
    token_max_ttl=1h >/dev/null
}

vault write auth/kubernetes/role/mattercodex-buildkit \
  bound_service_account_names=mattercodex-buildkit \
  bound_service_account_namespaces=mattercodex-system \
  token_policies=mattercodex-buildkit-pki-issue,mattercodex-buildkit-pki-ca \
  token_ttl=30m token_max_ttl=1h >/dev/null
vault write auth/kubernetes/role/mattercodex-role-image-builder \
  bound_service_account_names=mattercodex-role-image-builder \
  bound_service_account_namespaces=mattercodex-system \
  token_policies=mattercodex-role-image-builder-pki-issue,mattercodex-role-image-builder-pki-ca \
  token_ttl=30m token_max_ttl=1h >/dev/null
configure_kubernetes_role mattercodex-image-registry-pull mattercodex-image-registry-pull mattercodex-image-registry-pull-pki
vault write auth/kubernetes/role/mattercodex-image-registry-push \
  bound_service_account_names=mattercodex-image-registry-push \
  bound_service_account_namespaces=mattercodex-system \
  token_policies=mattercodex-image-registry-push-pki \
  token_ttl=30m token_max_ttl=1h >/dev/null
configure_kubernetes_role mattercodex-image-registry-admin mattercodex-image-registry-admin mattercodex-image-registry-admin-pki
configure_kubernetes_role mattercodex-image-registry-promotion mattercodex-image-registry-promotion mattercodex-image-registry-promotion-pki

cat <<'HCL' | vault policy write mattercodex-image-admission - >/dev/null
path "kv/data/mattercodex/image-admission/signing" {
  capabilities = ["read"]
}
path "kv/data/mattercodex/image-admission/admission" {
  capabilities = ["read"]
}
HCL
configure_kubernetes_role mattercodex-image-admission mattercodex-image-admission mattercodex-image-admission

verify_server_role() {
  local mount=$1
  local role=$2
  local service=$3
  vault read -format=json "${mount}/roles/${role}" |
    jq -e --arg bare "$service" --arg fqdn "${service}.mattercodex-system.svc.cluster.local" '
      .data.server_flag == true and .data.client_flag == false and
      .data.allow_any_name == false and .data.allow_subdomains == false and
      .data.ttl == 3600 and .data.max_ttl == 7200 and
      (.data.allowed_domains | index($bare) != null) and
      (.data.allowed_domains | index($fqdn) != null) and
      (.data.ext_key_usage | index("ServerAuth") != null) and
      (.data.ext_key_usage | index("ClientAuth") == null)
    ' >/dev/null
}

verify_client_role() {
  local role=$1
  local common_name=$2
  vault read -format=json "pki/roles/${role}" |
    jq -e --arg common_name "$common_name" '
      .data.server_flag == false and .data.client_flag == true and
      .data.allow_any_name == false and .data.allow_subdomains == false and
      .data.ttl == 1800 and .data.max_ttl == 3600 and
      (.data.allowed_domains | index($common_name) != null) and
      (.data.ext_key_usage | index("ClientAuth") != null) and
      (.data.ext_key_usage | index("ServerAuth") == null)
    ' >/dev/null
}

verify_server_role pki mattercodex-buildkit-server mattercodex-buildkit
verify_client_role mattercodex-buildkit-probe mattercodex-buildkit-probe
verify_client_role mattercodex-buildkit-client mattercodex-role-image-builder
verify_server_role pki-public mattercodex-image-registry-pull mattercodex-image-registry-pull
verify_server_role pki mattercodex-image-registry-push mattercodex-image-registry-push
verify_server_role pki mattercodex-image-registry-admin mattercodex-image-registry-admin
verify_server_role pki mattercodex-image-registry-promotion mattercodex-image-registry-promotion

echo "image supply-chain PKI roles configured for $1"
