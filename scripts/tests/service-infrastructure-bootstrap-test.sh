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

bash -n "$bootstrap" "$vault_initializer"

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

printf 'Service infrastructure bootstrap checks completed\n'
