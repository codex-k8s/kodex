#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 || ( $1 != staging && $1 != production ) ]]; then
  echo "usage: configure-image-supply-chain-pki.sh staging|production registry-pull-fqdn" >&2
  exit 64
fi
registry_pull_host=$2
[[ $registry_pull_host =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] &&
  [[ $registry_pull_host == *.* ]] &&
  [[ $registry_pull_host != *.svc ]] &&
  [[ $registry_pull_host != *.svc.cluster.local ]] || {
  echo "registry-pull-fqdn is invalid" >&2
  exit 64
}

command -v vault >/dev/null 2>&1 || {
  echo "vault CLI is required" >&2
  exit 69
}
command -v jq >/dev/null 2>&1 || {
  echo "jq is required" >&2
  exit 69
}

: "${VAULT_ADDR:?VAULT_ADDR is required}"

if ! vault secrets list -format=json | jq -e 'has("pki-buildkit-push/")' >/dev/null; then
  vault secrets enable -path=pki-buildkit-push pki >/dev/null
  vault secrets tune -max-lease-ttl=8760h pki-buildkit-push >/dev/null
  vault write -field=certificate pki-buildkit-push/root/generate/internal \
    common_name=mattercodex-buildkit-staging-push-root ttl=8760h key_type=rsa key_bits=4096 >/dev/null
fi
if ! vault secrets list -format=json | jq -e 'has("pki-node-pull/")' >/dev/null; then
  vault secrets enable -path=pki-node-pull pki >/dev/null
  vault secrets tune -max-lease-ttl=8760h pki-node-pull >/dev/null
  vault write -field=certificate pki-node-pull/root/generate/internal \
    common_name=mattercodex-node-pull-root ttl=8760h key_type=rsa key_bits=4096 >/dev/null
fi

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

configure_client_role_at() {
  local mount=$1
  local role=$2
  local common_name=$3
  vault write "${mount}/roles/${role}" \
    allowed_domains="${common_name}" allow_bare_domains=true allow_subdomains=false \
    allow_glob_domains=false enforce_hostnames=true require_cn=true \
    server_flag=false client_flag=true key_type=rsa key_bits=3072 \
    key_usage="DigitalSignature,KeyEncipherment" ext_key_usage=ClientAuth \
    ttl=30m max_ttl=1h >/dev/null
}

configure_server_role pki mattercodex-buildkit-server mattercodex-buildkit
configure_client_role mattercodex-buildkit-probe mattercodex-buildkit-probe
configure_client_role mattercodex-buildkit-client role-image-builder
configure_client_role mattercodex-buildkit-base-pull mattercodex-buildkit-base-pull
configure_client_role_at pki-buildkit-push mattercodex-buildkit-staging-push mattercodex-buildkit-staging-push
configure_client_role mattercodex-role-image-input-read role-image-builder-input-read
configure_client_role mattercodex-image-registry-pull-probe mattercodex-image-registry-pull-probe
configure_client_role mattercodex-image-registry-admin-probe mattercodex-image-registry-admin-probe
configure_client_role mattercodex-image-registry-promotion-probe mattercodex-image-registry-promotion-probe
configure_client_role mattercodex-image-scanner mattercodex-image-scanner
configure_client_role mattercodex-image-signer mattercodex-image-signer
configure_client_role image-admission image-admission
configure_client_role image-promotion image-promotion
configure_client_role mattercodex-registry-cleanup mattercodex-registry-cleanup
vault write pki-public/roles/mattercodex-image-registry-pull \
  allowed_domains="$registry_pull_host,mattercodex-image-registry,mattercodex-image-registry.mattercodex-system.svc,mattercodex-image-registry.mattercodex-system.svc.cluster.local" allow_bare_domains=true \
  allow_subdomains=false allow_glob_domains=false enforce_hostnames=true \
  require_cn=true server_flag=true client_flag=false key_type=rsa key_bits=3072 \
  key_usage="DigitalSignature,KeyEncipherment" ext_key_usage=ServerAuth \
  ttl=1h max_ttl=2h >/dev/null
configure_server_role pki-buildkit-push mattercodex-image-registry-push mattercodex-image-registry-push
configure_server_role pki mattercodex-image-registry-staging-read mattercodex-image-registry-staging-read
configure_server_role pki mattercodex-image-registry-admin mattercodex-image-registry-admin
configure_server_role pki mattercodex-image-registry-promotion mattercodex-image-registry-promotion
vault write pki-node-pull/roles/mattercodex-node-pull \
  allowed_domains=mattercodex-node-pull allow_bare_domains=false allow_subdomains=true \
  allow_glob_domains=false enforce_hostnames=true require_cn=true allow_ip_sans=true \
  server_flag=false client_flag=true key_type=rsa key_bits=3072 \
  key_usage="DigitalSignature,KeyEncipherment" ext_key_usage=ClientAuth ttl=30m max_ttl=30m >/dev/null

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
  mattercodex-buildkit-server mattercodex-buildkit-probe mattercodex-buildkit-base-pull
write_issue_policy mattercodex-buildkit-push-issue pki-buildkit-push mattercodex-buildkit-staging-push
write_ca_policy mattercodex-buildkit-push-ca pki-buildkit-push
write_ca_policy mattercodex-buildkit-pki-ca pki
cat <<'HCL' | vault policy write mattercodex-buildkit-pull-authority - >/dev/null
path "pki-public/cert/ca" {
  capabilities = ["read"]
}
path "kv/data/mattercodex/image-registry/buildkit-base-pull" {
  capabilities = ["read"]
}
HCL
write_issue_policy role-image-builder-pki-issue pki mattercodex-buildkit-client
write_ca_policy role-image-builder-pki-ca pki
write_issue_policy role-image-builder-input-read-pki-issue pki mattercodex-role-image-input-read
write_ca_policy role-image-builder-input-read-server-ca pki-public
cat <<'HCL' | vault policy write role-image-builder-runtime - >/dev/null
path "kv/data/mattercodex/role-image-builder/application-grant" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write role-image-builder-inputs - >/dev/null
path "kv/data/mattercodex/role-image-builder/input-read" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write role-image-builder-secret-resolver - >/dev/null
path "kv/data/mattercodex/role-image-builder/input-authority/*" {
  capabilities = ["read"]
}
path "auth/token/revoke-self" {
  capabilities = ["update"]
}
HCL
cat <<'HCL' | vault policy write role-image-builder-base-pull - >/dev/null
path "kv/data/mattercodex/image-registry/buildkit-base-pull" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write mattercodex-image-registry-pull-pki - >/dev/null
path "pki-public/issue/mattercodex-image-registry-pull" {
  capabilities = ["update"]
}
path "pki/issue/mattercodex-image-registry-pull-probe" {
  capabilities = ["update"]
}
path "pki/cert/ca" {
  capabilities = ["read"]
}
path "pki-node-pull/cert/ca" {
  capabilities = ["read"]
}
path "kv/data/mattercodex/image-registry/pull" {
  capabilities = ["read"]
}
path "pki-public/cert/ca" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write mattercodex-node-pull-bootstrap - >/dev/null
path "pki-node-pull/issue/mattercodex-node-pull" {
  capabilities = ["update"]
}
path "auth/token/revoke-self" {
  capabilities = ["update"]
}
HCL
cat <<'HCL' | vault policy write mattercodex-image-registry-push-pki - >/dev/null
path "pki-buildkit-push/issue/mattercodex-image-registry-push" {
  capabilities = ["update"]
}
path "pki-buildkit-push/cert/ca" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write mattercodex-image-registry-staging-read - >/dev/null
path "pki/issue/mattercodex-image-registry-staging-read" {
  capabilities = ["update"]
}
path "pki/cert/ca" {
  capabilities = ["read"]
}
path "kv/data/mattercodex/image-registry/staging-read" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write mattercodex-image-registry-admin-pki - >/dev/null
path "pki/issue/mattercodex-image-registry-admin" {
  capabilities = ["update"]
}
path "pki/issue/mattercodex-image-registry-admin-probe" {
  capabilities = ["update"]
}
path "pki/cert/ca" {
  capabilities = ["read"]
}
path "kv/data/mattercodex/image-registry/admin" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write mattercodex-image-registry-promotion-pki - >/dev/null
path "pki/issue/mattercodex-image-registry-promotion" {
  capabilities = ["update"]
}
path "pki/issue/mattercodex-image-registry-promotion-probe" {
  capabilities = ["update"]
}
path "pki/cert/ca" {
  capabilities = ["read"]
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
  token_policies=mattercodex-buildkit-pki-issue,mattercodex-buildkit-pki-ca,mattercodex-buildkit-pull-authority,mattercodex-buildkit-push-issue,mattercodex-buildkit-push-ca \
  token_ttl=30m token_max_ttl=1h >/dev/null
vault write auth/kubernetes/role/role-image-builder \
  bound_service_account_names=role-image-builder \
  bound_service_account_namespaces=mattercodex-system \
  token_policies=role-image-builder-pki-issue,role-image-builder-pki-ca,role-image-builder-runtime \
  token_ttl=30m token_max_ttl=1h >/dev/null
vault write auth/kubernetes/role/role-image-builder-input-read \
  bound_service_account_names=role-image-builder \
  bound_service_account_namespaces=mattercodex-system \
  token_policies=role-image-builder-input-read-pki-issue,role-image-builder-input-read-server-ca,role-image-builder-inputs \
  token_ttl=30m token_max_ttl=1h >/dev/null
vault write auth/kubernetes/role/role-image-builder-base-pull \
  bound_service_account_names=role-image-builder \
  bound_service_account_namespaces=mattercodex-system \
  token_policies=role-image-builder-base-pull \
  token_ttl=30m token_max_ttl=1h >/dev/null
vault write auth/kubernetes/role/role-image-builder-secret-resolver \
  bound_service_account_names=role-image-builder \
  bound_service_account_namespaces=mattercodex-system audience=vault \
  token_policies=role-image-builder-secret-resolver \
  token_ttl=5m token_max_ttl=5m >/dev/null
configure_kubernetes_role mattercodex-image-registry-pull mattercodex-image-registry-pull mattercodex-image-registry-pull-pki
vault write auth/kubernetes/role/mattercodex-image-registry-push \
  bound_service_account_names=mattercodex-image-registry-push \
  bound_service_account_namespaces=mattercodex-system \
  token_policies=mattercodex-image-registry-push-pki \
  token_ttl=30m token_max_ttl=1h >/dev/null
configure_kubernetes_role mattercodex-image-registry-staging-read mattercodex-image-registry-staging-read mattercodex-image-registry-staging-read
configure_kubernetes_role mattercodex-node-pull-bootstrap mattercodex-image-pull-readback mattercodex-node-pull-bootstrap
configure_kubernetes_role mattercodex-image-registry-admin mattercodex-image-registry-admin mattercodex-image-registry-admin-pki
configure_kubernetes_role mattercodex-image-registry-promotion mattercodex-image-registry-promotion mattercodex-image-registry-promotion-pki

configure_phase_policy() {
  local policy=$1
  local certificate_role=$2
  shift 2
  {
    printf 'path "pki/issue/%s" { capabilities = ["update"] }\n' "$certificate_role"
    printf 'path "pki/cert/ca" { capabilities = ["read"] }\n'
    for secret_path in "$@"; do
      printf 'path "kv/data/%s" { capabilities = ["read"] }\n' "$secret_path"
    done
  } | vault policy write "$policy" - >/dev/null
}

configure_phase_policy mattercodex-image-scanner mattercodex-image-scanner \
  mattercodex/image-registry/scanner
configure_phase_policy mattercodex-image-signer mattercodex-image-signer \
  mattercodex/image-registry/signer mattercodex/image-admission/signing
configure_phase_policy image-admission image-admission \
  mattercodex/image-registry/admission mattercodex/image-admission/signing \
  mattercodex/image-admission/application-grant
configure_phase_policy image-promotion image-promotion \
  mattercodex/image-registry/promotion-staging mattercodex/image-registry/promotion \
  mattercodex/image-promotion/application-grant
configure_phase_policy mattercodex-registry-cleanup mattercodex-registry-cleanup \
  mattercodex/image-registry/admin
configure_kubernetes_role mattercodex-image-scanner mattercodex-image-scanner mattercodex-image-scanner
configure_kubernetes_role mattercodex-image-signer mattercodex-image-signer mattercodex-image-signer
configure_kubernetes_role image-admission image-admission image-admission
configure_kubernetes_role image-promotion image-promotion image-promotion
configure_kubernetes_role mattercodex-registry-cleanup mattercodex-registry-cleanup mattercodex-registry-cleanup

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
verify_client_role mattercodex-buildkit-client role-image-builder
verify_client_role mattercodex-buildkit-base-pull mattercodex-buildkit-base-pull
verify_client_role mattercodex-buildkit-staging-push mattercodex-buildkit-staging-push
verify_client_role mattercodex-role-image-input-read role-image-builder-input-read
verify_client_role mattercodex-image-registry-pull-probe mattercodex-image-registry-pull-probe
verify_client_role mattercodex-image-registry-push-probe mattercodex-image-registry-push-probe
verify_client_role mattercodex-image-registry-admin-probe mattercodex-image-registry-admin-probe
verify_client_role mattercodex-image-registry-promotion-probe mattercodex-image-registry-promotion-probe
verify_client_role mattercodex-image-scanner mattercodex-image-scanner
verify_client_role mattercodex-image-signer mattercodex-image-signer
verify_client_role image-admission image-admission
verify_client_role image-promotion image-promotion
verify_client_role mattercodex-registry-cleanup mattercodex-registry-cleanup
vault read -format=json pki-public/roles/mattercodex-image-registry-pull |
  jq -e --arg fqdn "$registry_pull_host" '
    .data.server_flag == true and .data.client_flag == false and
    .data.allow_any_name == false and .data.allow_subdomains == false and
    .data.ttl == 3600 and .data.max_ttl == 7200 and
    (.data.allowed_domains | index($fqdn) != null) and
    (.data.allowed_domains | index("mattercodex-image-registry") != null) and
    (.data.allowed_domains | index("mattercodex-image-registry.mattercodex-system.svc") != null) and
    (.data.allowed_domains | index("mattercodex-image-registry.mattercodex-system.svc.cluster.local") != null) and
    (.data.ext_key_usage | index("ServerAuth") != null) and
    (.data.ext_key_usage | index("ClientAuth") == null)
  ' >/dev/null
verify_server_role pki mattercodex-image-registry-push mattercodex-image-registry-push
verify_server_role pki mattercodex-image-registry-admin mattercodex-image-registry-admin
verify_server_role pki mattercodex-image-registry-promotion mattercodex-image-registry-promotion

echo "image supply-chain PKI roles configured for $1 and exact pull host"
