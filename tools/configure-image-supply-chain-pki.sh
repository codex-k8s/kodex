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
    common_name=kodex-buildkit-staging-push-root ttl=8760h key_type=rsa key_bits=4096 >/dev/null
fi
if ! vault secrets list -format=json | jq -e 'has("pki-node-pull/")' >/dev/null; then
  vault secrets enable -path=pki-node-pull pki >/dev/null
  vault secrets tune -max-lease-ttl=8760h pki-node-pull >/dev/null
  vault write -field=certificate pki-node-pull/root/generate/internal \
    common_name=kodex-node-pull-root ttl=8760h key_type=rsa key_bits=4096 >/dev/null
fi

configure_server_role() {
  local mount=$1
  local role=$2
  local service=$3
  vault write "${mount}/roles/${role}" \
    allowed_domains="${service},${service}.kodex-system.svc,${service}.kodex-system.svc.cluster.local" \
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
    allowed_domains="${common_name},${common_name}.kodex-system.svc" \
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

configure_server_role pki kodex-buildkit-server kodex-buildkit
configure_client_role kodex-buildkit-probe kodex-buildkit-probe
configure_client_role kodex-buildkit-client role-image-builder
configure_client_role kodex-buildkit-base-pull kodex-buildkit-base-pull
configure_client_role_at pki-buildkit-push kodex-buildkit-staging-push kodex-buildkit-staging-push
configure_client_role kodex-role-image-input-read role-image-builder-input-read
configure_client_role kodex-image-registry-pull-probe kodex-image-registry-pull-probe
configure_client_role kodex-image-registry-admin-probe kodex-image-registry-admin-probe
configure_client_role kodex-image-registry-promotion-probe kodex-image-registry-promotion-probe
configure_client_role kodex-image-registry-evidence-probe kodex-image-registry-evidence-probe
configure_client_role kodex-image-scanner kodex-image-scanner
configure_client_role kodex-image-signer kodex-image-signer
configure_client_role image-admission image-admission
configure_client_role image-promotion image-promotion
configure_client_role kodex-registry-cleanup kodex-registry-cleanup
vault write pki-public/roles/kodex-image-registry-pull \
  allowed_domains="$registry_pull_host,kodex-image-registry,kodex-image-registry.kodex-system.svc,kodex-image-registry.kodex-system.svc.cluster.local" allow_bare_domains=true \
  allow_subdomains=false allow_glob_domains=false enforce_hostnames=true \
  require_cn=true server_flag=true client_flag=false key_type=rsa key_bits=3072 \
  key_usage="DigitalSignature,KeyEncipherment" ext_key_usage=ServerAuth \
  ttl=1h max_ttl=2h >/dev/null
configure_server_role pki-buildkit-push kodex-image-registry-push kodex-image-registry-push
configure_server_role pki kodex-image-registry-staging-read kodex-image-registry-staging-read
configure_server_role pki kodex-image-registry-evidence kodex-image-registry-evidence
configure_server_role pki kodex-image-registry-admin kodex-image-registry-admin
configure_server_role pki kodex-image-registry-promotion kodex-image-registry-promotion
vault write pki-node-pull/roles/kodex-node-pull \
  allowed_domains=kodex-node-pull allow_bare_domains=false allow_subdomains=true \
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

write_issue_policy kodex-buildkit-pki-issue pki \
  kodex-buildkit-server kodex-buildkit-probe kodex-buildkit-base-pull
write_issue_policy kodex-buildkit-push-issue pki-buildkit-push kodex-buildkit-staging-push
write_ca_policy kodex-buildkit-push-ca pki-buildkit-push
write_ca_policy kodex-buildkit-pki-ca pki
cat <<'HCL' | vault policy write kodex-buildkit-pull-authority - >/dev/null
path "pki-public/cert/ca" {
  capabilities = ["read"]
}
path "kv/data/kodex/image-registry/buildkit-base-pull" {
  capabilities = ["read"]
}
HCL
write_issue_policy role-image-builder-pki-issue pki kodex-buildkit-client
write_ca_policy role-image-builder-pki-ca pki
write_issue_policy role-image-builder-input-read-pki-issue pki kodex-role-image-input-read
write_ca_policy role-image-builder-input-read-server-ca pki-public
cat <<'HCL' | vault policy write role-image-builder-runtime - >/dev/null
path "kv/data/kodex/role-image-builder/application-grant" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write role-image-builder-inputs - >/dev/null
path "kv/data/kodex/role-image-builder/input-read" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write role-image-builder-base-pull - >/dev/null
path "kv/data/kodex/image-registry/buildkit-base-pull" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write kodex-image-registry-pull-pki - >/dev/null
path "pki-public/issue/kodex-image-registry-pull" {
  capabilities = ["update"]
}
path "pki/issue/kodex-image-registry-pull-probe" {
  capabilities = ["update"]
}
path "pki/cert/ca" {
  capabilities = ["read"]
}
path "pki-node-pull/cert/ca" {
  capabilities = ["read"]
}
path "kv/data/kodex/image-registry/pull" {
  capabilities = ["read"]
}
path "pki-public/cert/ca" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write kodex-node-pull-bootstrap - >/dev/null
path "pki-node-pull/issue/kodex-node-pull" {
  capabilities = ["update"]
}
path "auth/token/revoke-self" {
  capabilities = ["update"]
}
HCL
cat <<'HCL' | vault policy write kodex-image-registry-push-pki - >/dev/null
path "pki-buildkit-push/issue/kodex-image-registry-push" {
  capabilities = ["update"]
}
path "pki-buildkit-push/cert/ca" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write kodex-image-registry-staging-read - >/dev/null
path "pki/issue/kodex-image-registry-staging-read" {
  capabilities = ["update"]
}
path "pki/cert/ca" {
  capabilities = ["read"]
}
path "kv/data/kodex/image-registry/staging-read" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write kodex-image-registry-admin-pki - >/dev/null
path "pki/issue/kodex-image-registry-admin" {
  capabilities = ["update"]
}
path "pki/issue/kodex-image-registry-admin-probe" {
  capabilities = ["update"]
}
path "pki/cert/ca" {
  capabilities = ["read"]
}
path "kv/data/kodex/image-registry/admin" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write kodex-image-registry-evidence-pki - >/dev/null
path "pki/issue/kodex-image-registry-evidence" {
  capabilities = ["update"]
}
path "pki/issue/kodex-image-registry-evidence-probe" {
  capabilities = ["update"]
}
path "pki/cert/ca" {
  capabilities = ["read"]
}
path "kv/data/kodex/image-registry/evidence-probe" {
  capabilities = ["read"]
}
path "kv/data/kodex/image-registry/evidence-admission" {
  capabilities = ["read"]
}
path "kv/data/kodex/image-registry/evidence-promotion" {
  capabilities = ["read"]
}
HCL
cat <<'HCL' | vault policy write kodex-image-registry-promotion-pki - >/dev/null
path "pki/issue/kodex-image-registry-promotion" {
  capabilities = ["update"]
}
path "pki/issue/kodex-image-registry-promotion-probe" {
  capabilities = ["update"]
}
path "pki/cert/ca" {
  capabilities = ["read"]
}
path "kv/data/kodex/image-registry/promotion" {
  capabilities = ["read"]
}
HCL

configure_kubernetes_role() {
  local role=$1
  local service_account=$2
  local policy=$3
  vault write "auth/kubernetes/role/${role}" \
    bound_service_account_names="$service_account" \
    bound_service_account_namespaces=kodex-system \
    token_policies="$policy" \
    token_ttl=30m \
    token_max_ttl=1h >/dev/null
}

vault write auth/kubernetes/role/kodex-buildkit \
  bound_service_account_names=kodex-buildkit \
  bound_service_account_namespaces=kodex-system \
  token_policies=kodex-buildkit-pki-issue,kodex-buildkit-pki-ca,kodex-buildkit-pull-authority,kodex-buildkit-push-issue,kodex-buildkit-push-ca \
  token_ttl=30m token_max_ttl=1h >/dev/null
vault write auth/kubernetes/role/role-image-builder \
  bound_service_account_names=role-image-builder \
  bound_service_account_namespaces=kodex-system \
  token_policies=role-image-builder-pki-issue,role-image-builder-pki-ca,role-image-builder-runtime \
  token_ttl=30m token_max_ttl=1h >/dev/null
vault write auth/kubernetes/role/role-image-builder-input-read \
  bound_service_account_names=role-image-builder \
  bound_service_account_namespaces=kodex-system \
  token_policies=role-image-builder-input-read-pki-issue,role-image-builder-input-read-server-ca,role-image-builder-inputs \
  token_ttl=30m token_max_ttl=1h >/dev/null
vault write auth/kubernetes/role/role-image-builder-base-pull \
  bound_service_account_names=role-image-builder \
  bound_service_account_namespaces=kodex-system \
  token_policies=role-image-builder-base-pull \
  token_ttl=30m token_max_ttl=1h >/dev/null
configure_kubernetes_role kodex-image-registry-pull kodex-image-registry-pull kodex-image-registry-pull-pki
vault write auth/kubernetes/role/kodex-image-registry-push \
  bound_service_account_names=kodex-image-registry-push \
  bound_service_account_namespaces=kodex-system \
  token_policies=kodex-image-registry-push-pki \
  token_ttl=30m token_max_ttl=1h >/dev/null
configure_kubernetes_role kodex-image-registry-staging-read kodex-image-registry-staging-read kodex-image-registry-staging-read
configure_kubernetes_role kodex-image-registry-evidence kodex-image-registry-evidence kodex-image-registry-evidence-pki
configure_kubernetes_role kodex-node-pull-bootstrap kodex-image-pull-readback kodex-node-pull-bootstrap
configure_kubernetes_role kodex-image-registry-admin kodex-image-registry-admin kodex-image-registry-admin-pki
configure_kubernetes_role kodex-image-registry-promotion kodex-image-registry-promotion kodex-image-registry-promotion-pki

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

configure_phase_policy kodex-image-scanner kodex-image-scanner \
  kodex/image-registry/scanner
configure_phase_policy kodex-image-signer kodex-image-signer \
  kodex/image-registry/signer kodex/image-admission/signing
configure_phase_policy image-admission image-admission \
  kodex/image-registry/admission kodex/image-registry/evidence-admission kodex/image-admission/signing \
  kodex/image-admission/application-grant
configure_phase_policy image-promotion image-promotion \
  kodex/image-registry/promotion-staging kodex/image-registry/promotion kodex/image-registry/evidence-promotion \
  kodex/image-promotion/application-grant
configure_phase_policy kodex-registry-cleanup kodex-registry-cleanup \
  kodex/image-registry/admin
configure_kubernetes_role kodex-image-scanner kodex-image-scanner kodex-image-scanner
configure_kubernetes_role kodex-image-signer kodex-image-signer kodex-image-signer
configure_kubernetes_role image-admission image-admission image-admission
configure_kubernetes_role image-promotion image-promotion image-promotion
configure_kubernetes_role kodex-registry-cleanup kodex-registry-cleanup kodex-registry-cleanup

verify_server_role() {
  local mount=$1
  local role=$2
  local service=$3
  vault read -format=json "${mount}/roles/${role}" |
    jq -e --arg bare "$service" --arg fqdn "${service}.kodex-system.svc.cluster.local" '
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

verify_server_role pki kodex-buildkit-server kodex-buildkit
verify_client_role kodex-buildkit-probe kodex-buildkit-probe
verify_client_role kodex-buildkit-client role-image-builder
verify_client_role kodex-buildkit-base-pull kodex-buildkit-base-pull
verify_client_role kodex-buildkit-staging-push kodex-buildkit-staging-push
verify_client_role kodex-role-image-input-read role-image-builder-input-read
verify_client_role kodex-image-registry-pull-probe kodex-image-registry-pull-probe
verify_client_role kodex-image-registry-push-probe kodex-image-registry-push-probe
verify_client_role kodex-image-registry-admin-probe kodex-image-registry-admin-probe
verify_client_role kodex-image-registry-promotion-probe kodex-image-registry-promotion-probe
verify_client_role kodex-image-registry-evidence-probe kodex-image-registry-evidence-probe
verify_client_role kodex-image-scanner kodex-image-scanner
verify_client_role kodex-image-signer kodex-image-signer
verify_client_role image-admission image-admission
verify_client_role image-promotion image-promotion
verify_client_role kodex-registry-cleanup kodex-registry-cleanup
vault read -format=json pki-public/roles/kodex-image-registry-pull |
  jq -e --arg fqdn "$registry_pull_host" '
    .data.server_flag == true and .data.client_flag == false and
    .data.allow_any_name == false and .data.allow_subdomains == false and
    .data.ttl == 3600 and .data.max_ttl == 7200 and
    (.data.allowed_domains | index($fqdn) != null) and
    (.data.allowed_domains | index("kodex-image-registry") != null) and
    (.data.allowed_domains | index("kodex-image-registry.kodex-system.svc") != null) and
    (.data.allowed_domains | index("kodex-image-registry.kodex-system.svc.cluster.local") != null) and
    (.data.ext_key_usage | index("ServerAuth") != null) and
    (.data.ext_key_usage | index("ClientAuth") == null)
  ' >/dev/null
verify_server_role pki kodex-image-registry-push kodex-image-registry-push
verify_server_role pki kodex-image-registry-admin kodex-image-registry-admin
verify_server_role pki kodex-image-registry-promotion kodex-image-registry-promotion
verify_server_role pki kodex-image-registry-evidence kodex-image-registry-evidence

echo "image supply-chain PKI roles configured for $1 and exact pull host"
