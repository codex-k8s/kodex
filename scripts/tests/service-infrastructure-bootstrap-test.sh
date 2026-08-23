#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Service infrastructure bootstrap test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
bootstrap="$repository_root/infra/service-infrastructure/bootstrap.sh"
vault_bootstrap="$repository_root/infra/service-infrastructure/vault-bootstrap.yaml"

bash -n "$bootstrap"

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

printf 'Service infrastructure bootstrap checks completed\n'
