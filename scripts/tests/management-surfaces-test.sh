#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Management surfaces test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
identity_bootstrap="$repository_root/infra/identity/bootstrap.sh"
management_bootstrap="$repository_root/infra/management-surfaces/bootstrap.sh"
keycloak_bootstrap="$repository_root/tools/deploy/configure-keycloak.sh"
vault_bootstrap="$repository_root/tools/deploy/bootstrap-vault.sh"
temporary_directory=$(mktemp -d)
plaintext_test_directory=""
cleanup() {
  rm -rf -- "$temporary_directory"
  [[ -z "$plaintext_test_directory" ]] || rm -rf -- "$plaintext_test_directory"
}
trap cleanup EXIT

for command_name in jq kubectl yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

bash -n \
  "$identity_bootstrap" \
  "$management_bootstrap" \
  "$keycloak_bootstrap" \
  "$repository_root/tools/deploy/configure-vault-oidc.sh" \
  "$repository_root/tools/deploy/destroy-plaintext-identity-material.sh" \
  "$repository_root/tools/deploy/generate-identity-material.sh" \
  "$repository_root/tools/deploy/import-identity-material.sh" \
  "$repository_root/tools/deploy/materialize-identity-secrets.sh" \
  "$repository_root/tools/deploy/restore-vault-recovery-material.sh" \
  "$repository_root/tools/deploy/seal-vault-recovery-material.sh" \
  "$vault_bootstrap"

identity_render="$temporary_directory/identity.yaml"
kubectl kustomize "$repository_root/infra/identity" >"$identity_render"
OIDC_HOST=identity.example.test INGRESS_CLASS=public CLUSTER_ISSUER=public-production \
INGRESS_NAMESPACE=ingress-system INGRESS_POD_NAME=public-ingress yq -i '
  (.. | select(tag == "!!str")) |= (
    sub("__MATTERCODEX_OIDC_HOST__"; strenv(OIDC_HOST)) |
    sub("__MATTERCODEX_INGRESS_CLASS__"; strenv(INGRESS_CLASS)) |
    sub("__MATTERCODEX_CLUSTER_ISSUER__"; strenv(CLUSTER_ISSUER)) |
    sub("__MATTERCODEX_INGRESS_NAMESPACE__"; strenv(INGRESS_NAMESPACE)) |
    sub("__MATTERCODEX_INGRESS_POD_NAME__"; strenv(INGRESS_POD_NAME))
  )
' "$identity_render"
! rg -q '__MATTERCODEX_[A-Z0-9_]+__|sha256:0{64}' "$identity_render" ||
  fail 'identity render contains an unresolved placeholder'
yq -o=json -I=0 'select(.kind == "Deployment" and .metadata.name == "sso")' "$identity_render" |
  jq -e '
    any(.spec.template.spec.containers[];
      .name == "keycloak" and
      any(.env[]; .name == "KC_BOOTSTRAP_ADMIN_USERNAME" and
        .valueFrom.secretKeyRef.name == "keycloak-bootstrap") and
      any(.env[]; .name == "KC_HTTP_HOST" and .value == "127.0.0.1")) and
    all(.spec.template.spec.containers[] | select(.name == "keycloak").ports[]; .name != "http")
  ' >/dev/null || fail 'Keycloak bootstrap administrator is not secret-backed'
yq -o=json -I=0 'select(.kind == "StatefulSet" and .metadata.name == "keycloak-postgresql")' "$identity_render" |
  jq -e '
    any(.spec.template.spec.containers[];
      .name == "postgresql" and
      (.args | index("ssl=on")) != null and
      (.args | index("ssl_min_protocol_version=TLSv1.3")) != null and
      (.args | index("hba_file=/var/run/secrets/mattercodex/postgresql/pg_hba.conf")) != null and
      (.args | index("password_encryption=scram-sha-256")) != null) and
    any(.spec.template.spec.initContainers[];
      .name == "materialize-postgresql-tls" and
      (.args[0] | contains("hostnossl all all 0.0.0.0/0 reject")))
  ' >/dev/null || fail 'Keycloak PostgreSQL TLS boundary is incomplete'
yq -o=json -I=0 'select(.kind == "Ingress" and .metadata.name == "sso")' "$identity_render" |
  jq -e '
    .spec.rules[0].http.paths[0].backend.service.port.name == "https" and
    .metadata.annotations."traefik.ingress.kubernetes.io/service.serverstransport" ==
      "identity-sso-public@kubernetescrd"
  ' >/dev/null || fail 'Keycloak public ingress does not preserve backend TLS'

headlamp_values="$repository_root/infra/management-surfaces/headlamp-values.yaml"
yq -e '
  .config.unsafeUseServiceAccountToken == true and
  .clusterRoleBinding.create == true and
  .clusterRoleBinding.clusterRoleName == "cluster-admin" and
  .ingress.enabled == false
' "$headlamp_values" >/dev/null || fail 'Headlamp cluster-admin boundary is incomplete'

oauth_values="$repository_root/infra/management-surfaces/oauth2-proxy-values.yaml"
yq -e '
  .extraArgs.provider == "keycloak-oidc" and
  .extraArgs."code-challenge-method" == "S256" and
  .extraArgs."allowed-role" == "__MATTERCODEX_ALLOWED_ROLE__" and
  .extraArgs."pass-access-token" == "false" and
  .extraArgs."pass-authorization-header" == "false" and
  .ingress.path == "/oauth2"
' "$oauth_values" >/dev/null || fail 'OAuth2 Proxy boundary is incomplete'
if yq -e '.ingress.annotations | has("cert-manager.io/cluster-issuer")' "$oauth_values" >/dev/null 2>&1; then
  fail 'OAuth2 Proxy ingress attempts to own a duplicate public certificate'
fi
for tls_binding in \
  'control-center) tls_secret=staff-control-center-public-tls' \
  'grafana) tls_secret=mattercodex-grafana-public-tls' \
  'vault) tls_secret=mattercodex-vault-ui-public-tls' \
  'headlamp) tls_secret=mattercodex-headlamp-public-tls'; do
  grep -Fq "$tls_binding" "$management_bootstrap" ||
    fail "OAuth2 Proxy does not reuse the backend TLS Secret: $tls_binding"
done

routes="$repository_root/infra/management-surfaces/routes.yaml"
for ingress in mattercodex-grafana mattercodex-vault-ui mattercodex-headlamp; do
  yq -o=json -I=0 'select(.kind == "Ingress")' "$routes" |
    jq -s -e --arg ingress "$ingress" '
      any(.[];
        .metadata.name == $ingress and
        (.metadata.annotations."traefik.ingress.kubernetes.io/router.middlewares" |
          test("oauth2-.+-chain@kubernetescrd$")))
    ' >/dev/null || fail "management ingress bypasses OAuth2 Proxy: $ingress"
done
yq -o=json -I=0 'select(.kind == "NetworkPolicy")' "$routes" |
  jq -s -e '
    any(.[];
      .metadata.name == "headlamp-exact-paths" and
      .spec.policyTypes == ["Ingress", "Egress"])
  ' >/dev/null || fail 'Headlamp NetworkPolicy is absent'

grep -Fq '"headlamp|platform-admin|$headlamp_host|admin|$oidc_origin/realms/master"' \
  "$management_bootstrap" || fail 'Headlamp does not use the Keycloak master realm admin role'
grep -Fq 'reconcile_confidential_client mattercodex-headlamp-proxy "$headlamp_origin"' \
  "$keycloak_bootstrap" || fail 'Headlamp confidential client reconciliation is absent'
grep -Fq 'platform-admin oauth2-headlamp master' "$keycloak_bootstrap" ||
  fail 'Headlamp confidential client is outside the Keycloak master realm'
grep -Fq -- '--rolename admin' "$keycloak_bootstrap" ||
  fail 'Keycloak administrator role reconciliation is absent'
grep -Fq 'temporary Keycloak bootstrap administrator still exists' "$keycloak_bootstrap" ||
  fail 'temporary Keycloak bootstrap administrator deletion is not verified'
grep -Fq 'retire-initial-passwords' "$keycloak_bootstrap" ||
  fail 'Keycloak initial password retirement mode is absent'
grep -Fq 'vault operator init -format=json -key-shares=5 -key-threshold=3' "$vault_bootstrap" ||
  fail 'Vault does not use the approved Shamir 5/3 ceremony'

[[ -d /dev/shm && -w /dev/shm ]] || fail '/dev/shm is required for identity plaintext cleanup verification'
plaintext_test_directory=$(mktemp -d /dev/shm/mattercodex-identity-cleanup.XXXXXX)
input_directory="$plaintext_test_directory/inputs"
material_directory="$plaintext_test_directory/material"
mkdir -p "$input_directory" "$material_directory"
printf '%s' administrator >"$input_directory/admin-username"
printf '%s' test-administrator-password-12345 >"$input_directory/admin-initial-password"
printf '%s' owner >"$input_directory/owner-username"
printf '%s' owner@example.test >"$input_directory/owner-email"
printf '%s' test-owner-password-123456789 >"$input_directory/owner-initial-password"
"$repository_root/tools/deploy/generate-identity-material.sh" \
  --material-directory "$material_directory" \
  --admin-username-file "$input_directory/admin-username" \
  --admin-initial-password-file "$input_directory/admin-initial-password" \
  --owner-username-file "$input_directory/owner-username" \
  --owner-email-file "$input_directory/owner-email" \
  --owner-initial-password-file "$input_directory/owner-initial-password" >/dev/null
[[ "$(<"$material_directory/identity/admin-username")" != \
  "$(<"$material_directory/identity/bootstrap-admin-username")" ]] ||
  fail 'permanent and temporary Keycloak administrators are identical'
"$repository_root/tools/deploy/destroy-plaintext-identity-material.sh" \
  --material-directory "$material_directory" >/dev/null
[[ ! -e "$material_directory/identity" && ! -e "$material_directory/management" ]] ||
  fail 'identity plaintext cleanup did not remove generated material'

jq -e '
  .schemaVersion == 1 and (.charts | length) == 3 and
  ([.charts[].name] | sort) == ["headlamp", "kube-prometheus-stack", "oauth2-proxy"] and
  all(.charts[];
    (.sha256 | test("^[a-f0-9]{64}$")) and
    ((.sha256 | test("^0{64}$")) | not))
' "$repository_root/infra/management-surfaces/charts.lock.json" >/dev/null ||
  fail 'management chart lock is invalid'

yq -o=json -I=0 '.' "$repository_root/.github/workflows/prepare-installation-identity.yml" |
  jq -e '
    .jobs.prepare.environment == "production" and
    any(.jobs.prepare.steps[]; .uses == "actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803") and
    any(.jobs.prepare.steps[];
      .env.WORKFLOW_SHA == "${{ vars.MATTERCODEX_PRODUCTION_WORKFLOW_SHA }}") and
    any(.jobs.prepare.steps[]; .uses == "actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a")
  ' >/dev/null ||
  fail 'identity workflow is not pinned or owner-gated'
grep -Fq 'MATTERCODEX_KEYCLOAK_ADMIN_USERNAME' \
  "$repository_root/.github/workflows/prepare-installation-identity.yml" ||
  fail 'permanent Keycloak administrator variable is absent'
! grep -Fq 'MATTERCODEX_KEYCLOAK_BOOTSTRAP_ADMIN_' \
  "$repository_root/.github/workflows/prepare-installation-identity.yml" ||
  fail 'temporary Keycloak bootstrap identity is externally supplied'

if rg -q 'kubernetes\.io/metadata\.name: monitoring' "$repository_root/deploy/k8s"; then
  fail 'workload scrape policy still references the retired monitoring namespace'
fi
grep -Fq 'mattercodex-system-oauth2-control-center-chain@kubernetescrd' \
  "$repository_root/deploy/k8s/base/staff-control-center/ingress.yaml" ||
  fail 'Control Center ingress bypasses OAuth2 Proxy'

printf 'Management surfaces checks completed\n'
