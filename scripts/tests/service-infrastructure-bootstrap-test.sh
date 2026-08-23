#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Service infrastructure bootstrap test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
bootstrap="$repository_root/infra/service-infrastructure/bootstrap.sh"
vault_bootstrap="$repository_root/infra/service-infrastructure/vault-bootstrap.yaml"
vault_values="$repository_root/infra/service-infrastructure/vault-values.yaml"
vault_initializer="$repository_root/tools/deploy/bootstrap-vault.sh"
keycloak_bootstrap="$repository_root/tools/deploy/configure-keycloak.sh"

bash -n "$bootstrap" "$vault_initializer" "$keycloak_bootstrap"

for secret_bootstrap in "$vault_initializer" "$keycloak_bootstrap"; do
grep -Fq 'read_single_line_secret()' "$secret_bootstrap" ||
    fail "single-line secret validator is absent: ${secret_bootstrap#"$repository_root"/}"
  if rg -q '\{ cat "\$[^;]+"; printf '\''\\n'\''' "$secret_bootstrap"; then
    fail "secret bootstrap can insert an empty framing record: ${secret_bootstrap#"$repository_root"/}"
  fi
done
grep -Fq 'local material_file_path' "$vault_initializer" ||
  fail 'Vault owner material validation can mutate caller locals through Bash dynamic scope'
grep -Fq 'for material_file_path in "$root_token_file" "$unseal_key_file"; do' "$vault_initializer" ||
  fail 'Vault owner material validation does not use its private loop variable'
if grep -Fq 'for file_path in "$root_token_file" "$unseal_key_file"; do' "$vault_initializer"; then
  fail 'Vault owner material validation reuses the caller-owned seed file variable'
fi
[[ $(grep -Fc 'local path=$1 key=$2 seed_file_path=$3 root_token' "$vault_initializer") -eq 2 ]] ||
  fail 'Vault KV helpers do not own an isolated seed file variable'
grep -Fq '{ printf '\''%s\n'\'' "$root_token"; cat "$seed_file_path"; }' "$vault_initializer" ||
  fail 'Vault KV payload is not preserved byte-for-byte after token framing'
grep -Fq 'printf '\''%s\n%s\n'\'' "$database_password" "$role_password"' "$vault_initializer" ||
  fail 'PostgreSQL role password framing is not canonical'
grep -Fq 'printf '\''%s\n%s\n%s\n'\'' "$admin_client_id" "$admin_client_secret" "$owner_password"' \
  "$keycloak_bootstrap" || fail 'Keycloak owner credential framing is not canonical'

grep -Fq 'require_ready_deployment_by_selector cert-manager' "$bootstrap" ||
  fail 'trust-manager does not use the strict deployment readback'
grep -Fq 'require_ready_deployment_by_selector vault-secrets-operator-system' "$bootstrap" ||
  fail 'Vault Secrets Operator does not use the strict deployment readback'
grep -Fq 'app.kubernetes.io/component=controller-manager' "$bootstrap" ||
  fail 'Vault Secrets Operator selector does not identify the controller manager'
grep -Fq 'daemonset/mattercodex-secrets-store-csi-secrets-store-csi-driver --timeout=180s' "$bootstrap" ||
  fail 'Secrets Store CSI Driver rollout readback is absent'
grep -Fq 'require_vault_csi_provider' "$bootstrap" ||
  fail 'Vault CSI provider strict readback is absent'
[[ $(grep -Fc 'require_vault_csi_provider' "$bootstrap") -eq 3 ]] ||
  fail 'Vault CSI provider is not checked by apply and readback modes'

yq -e '.csi.enabled == true and .csi.agent.enabled == false' "$vault_values" >/dev/null ||
  fail 'Vault CSI provider or direct mode is disabled'
yq -e '
  (.csi.extraArgs | length) == 3 and
  .csi.extraArgs[0] == "--vault-addr=https://vault.mattercodex-system.svc:8200" and
  .csi.extraArgs[1] == "--vault-tls-ca-cert=/vault/tls/ca.crt" and
  .csi.extraArgs[2] == "--vault-tls-server-name=vault.mattercodex-system.svc.cluster.local"
' "$vault_values" >/dev/null || fail 'Vault CSI provider endpoint or TLS args are not exact'
yq -e '
  (.csi.volumeMounts | length) == 1 and
  .csi.volumeMounts[0].name == "vault-server-ca" and
  .csi.volumeMounts[0].mountPath == "/vault/tls" and
  .csi.volumeMounts[0].readOnly == true and
  (.csi.volumes | length) == 1 and
  .csi.volumes[0].name == "vault-server-ca" and
  .csi.volumes[0].secret.secretName == "mattercodex-vault-server-tls" and
  (.csi.volumes[0].secret.items | length) == 1 and
  .csi.volumes[0].secret.items[0].key == "ca.crt" and
  .csi.volumes[0].secret.items[0].path == "ca.crt"
' "$vault_values" >/dev/null || fail 'Vault CSI provider CA mount is not exact'
if rg -q -- '--vault-tls-skip-verify|vaultSkipTLSVerify' "$vault_values"; then
  fail 'Vault CSI provider values allow insecure TLS'
fi

while IFS= read -r manifest; do
  if yq -e 'select(.kind == "SecretProviderClass") | .spec.parameters | has("vaultAddress") or has("vaultTLSServerName") or has("vaultCACertPath") or has("vaultSkipTLSVerify")' \
    "$manifest" >/dev/null 2>&1; then
    fail "SecretProviderClass overrides provider-level Vault transport: ${manifest#"$repository_root"/}"
  fi
done < <(rg -l '^kind: SecretProviderClass$' "$repository_root/deploy/k8s" | sort)

for readiness_contract in \
  '.status.observedGeneration == .metadata.generation' \
  '(.status.updatedReplicas // 0) == .spec.replicas' \
  '(.status.readyReplicas // 0) == .spec.replicas' \
  '(.status.availableReplicas // 0) == .spec.replicas'; do
  grep -Fq "$readiness_contract" "$bootstrap" ||
    fail "deployment readiness contract is absent: $readiness_contract"
done

if grep -Fq 'mattercodex-vault-secrets-operator-vault-secrets-operator-controller-manager' "$bootstrap"; then
  fail 'readback still depends on the invalid duplicated release name'
fi

yq -e '
  select(.kind == "Certificate" and .metadata.name == "mattercodex-control-api-bootstrap") |
  .metadata.namespace == "mattercodex-system" and
  .spec.secretName == "mattercodex-control-api-bootstrap-tls"
' "$vault_bootstrap" >/dev/null ||
  fail 'control API bootstrap certificate is outside the MatterCodex namespace'

vault_endpoint='VAULT_ADDR=https://vault.mattercodex-system.svc.cluster.local:8200'
[[ $(grep -Fc "$vault_endpoint" "$vault_initializer") -eq 7 ]] ||
  fail 'Vault bootstrap does not consistently use the certificate-bound service DNS'
if rg -q 'VAULT_ADDR=https://(127\.0\.0\.1|localhost)' "$vault_initializer"; then
  fail 'Vault bootstrap uses a loopback hostname outside the certificate SAN contract'
fi
grep -Fq 'status_json=$(vault status -format=json) || status_code=$?' "$vault_initializer" ||
  fail 'Vault status exit code is not captured explicitly'
grep -Fq '0|2) printf "%s\n" "$status_json" ;;' "$vault_initializer" ||
  fail 'Vault bootstrap does not accept the documented sealed status code'
grep -Fq '*) exit "$status_code" ;;' "$vault_initializer" ||
  fail 'Vault bootstrap does not fail closed for unexpected status errors'
grep -Fq 'if ($value | type) == "boolean" then' "$vault_initializer" ||
  fail 'Vault status booleans are not type checked'
grep -Fq 'initialized=$(read_vault_boolean initialized <<<"$status")' "$vault_initializer" ||
  fail 'Vault initialized=false can still be interpreted as a jq execution error'
grep -Fq 'sealed=$(vault_status | read_vault_boolean sealed)' "$vault_initializer" ||
  fail 'Vault sealed=false can still be interpreted as a jq execution error'
if rg -q "jq -er '\.(initialized|sealed)'" "$vault_initializer"; then
  fail 'Vault bootstrap still binds boolean values to jq truthiness exit codes'
fi
grep -Fq 'vault write -format=json sys/unseal key=-' "$vault_initializer" ||
  fail 'Vault unseal does not use the stdin-backed API write'
if grep -Fq 'vault operator unseal' "$vault_initializer"; then
  fail 'Vault unseal still depends on an interactive operator command'
fi
grep -Fq 'exec vault kv put -mount=kv "$1" "$2"=-' "$vault_initializer" ||
  fail 'Vault KV put does not name the canonical mount'
grep -Fq 'exec vault kv patch -mount=kv "$1" "$2"=-' "$vault_initializer" ||
  fail 'Vault KV patch does not name the canonical mount'
[[ $(grep -Fc 'Vault KV seed path must be relative to the canonical mount' "$vault_initializer") -eq 2 ]] ||
  fail 'Vault KV seed helpers do not reject absolute or duplicated mount paths'
grep -Fq 'for delay in 1 2 3 5 8 13; do' "$vault_initializer" ||
  fail 'Vault PostgreSQL startup retry has no bounded backoff contract'
grep -Fq "rg -q 'error verifying connection:.*connection refused'" "$vault_initializer" ||
  fail 'Vault PostgreSQL startup retry is not limited to the proven transient error'
grep -Fq 'Vault database configuration failed with a non-transient error' "$vault_initializer" ||
  fail 'Vault PostgreSQL configuration does not fail closed for semantic errors'
grep -Fq 'Vault PostgreSQL connection did not become ready within the bounded retry budget' \
  "$vault_initializer" || fail 'Vault PostgreSQL configuration has no terminal retry error'
if grep -Fq 'verify_connection=false' "$vault_initializer"; then
  fail 'Vault PostgreSQL connection verification is disabled'
fi

grep -Fq 'local vault_path=$1 username=$2 password_file=$3 database=$4 postgresql_host=$5 ca_file=$6' \
  "$vault_initializer" || fail 'workload DSN helper does not require an exact PostgreSQL authority'
grep -Fq 'postgresql://%s:%s@%s:5432/%s?sslmode=verify-full&sslrootcert=%s' \
  "$vault_initializer" || fail 'workload DSN does not preserve verify-full with an exact authority'
control_plane_postgresql_host=control-plane-postgresql-rw.mattercodex-system.svc.cluster.local
internal_rpc_authority_postgresql_host=internal-rpc-authority-postgresql-rw.mattercodex-system.svc.cluster.local
grep -Fq "control_plane_postgresql_host=$control_plane_postgresql_host" "$vault_initializer" ||
  fail 'control-plane PostgreSQL authority is not canonical'
grep -Fq "internal_rpc_authority_postgresql_host=$internal_rpc_authority_postgresql_host" \
  "$vault_initializer" || fail 'internal-rpc-authority PostgreSQL authority is not canonical'
[[ $(grep -Fc '"$control_plane_postgresql_host"' "$vault_initializer") -eq 2 ]] ||
  fail 'control-plane migration and runtime DSN do not share the exact authority'
[[ $(grep -Fc '"$internal_rpc_authority_postgresql_host"' "$vault_initializer") -eq 2 ]] ||
  fail 'internal-rpc-authority migration and reconciler DSN do not share the exact authority'

printf 'Service infrastructure bootstrap checks completed\n'
