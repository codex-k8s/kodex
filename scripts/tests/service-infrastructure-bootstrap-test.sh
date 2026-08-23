#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Service infrastructure bootstrap test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
bootstrap="$repository_root/infra/service-infrastructure/bootstrap.sh"
vault_bootstrap="$repository_root/infra/service-infrastructure/vault-bootstrap.yaml"
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
grep -Fq '{ printf '\''%s\n'\'' "$root_token"; cat "$file_path"; }' "$vault_initializer" ||
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

printf 'Service infrastructure bootstrap checks completed\n'
