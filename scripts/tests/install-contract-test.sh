#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex install contract test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
cleanup() { rm -rf -- "$temporary_directory"; }
trap cleanup EXIT

export KODEX_INSTALL_MODE=existing-kubernetes
export KODEX_NAMESPACE=kodex-system
export KODEX_KUBECONFIG=/tmp/test-kubeconfig
export KODEX_KUBE_CONTEXT=kodex-test
export KODEX_RELEASE_REGISTRY_PASSWORD='test-registry-password-with-equals=='
export KODEX_CONTROL_TLS_RECOVERY_HOST=control-recovery.example.com
env_file="$temporary_directory/.kodex-env"
"$repository_root/tools/install/write-env-file.sh" --output "$env_file" >/dev/null
[[ "$(stat -c '%a' "$env_file")" == 600 ]] || fail '.kodex-env mode differs from 0600'

unset KODEX_INSTALL_MODE KODEX_NAMESPACE KODEX_KUBECONFIG KODEX_KUBE_CONTEXT
unset KODEX_RELEASE_REGISTRY_PASSWORD
unset KODEX_CONTROL_TLS_RECOVERY_HOST
# shellcheck source=../../tools/install/load-env.sh
source "$repository_root/tools/install/load-env.sh"
kodex_load_env "$env_file" || fail 'generated .kodex-env was not loaded'
[[ "$KODEX_INSTALL_MODE" == existing-kubernetes && "$KODEX_NAMESPACE" == kodex-system &&
  "$KODEX_RELEASE_REGISTRY_PASSWORD" == 'test-registry-password-with-equals==' &&
  "$KODEX_CONTROL_TLS_RECOVERY_HOST" == control-recovery.example.com ]] ||
  fail 'generated .kodex-env readback mismatch'

chmod 0644 "$env_file"
if kodex_load_env "$env_file" >/dev/null 2>&1; then
  fail 'over-permissive .kodex-env was accepted'
fi

for script in install.sh tools/install/bootstrap-cert-manager.sh \
  tools/install/configure-github.sh tools/install/configure-node-registry.sh \
  tools/install/deploy-platform.sh tools/install/generate-material.sh \
  tools/install/materialize-secrets.sh tools/install/prepare-host.sh \
  tools/install/release-platform.sh tools/install/reset-host.sh \
  tools/install/write-env-file.sh; do
  [[ -x "$repository_root/$script" ]] || fail "installer entrypoint is not executable: $script"
  bash -n "$repository_root/$script"
done
bash -n "$repository_root/tools/deploy/generate-identity-material.sh" \
  "$repository_root/tools/deploy/materialize-identity-secrets.sh"

rg -n 'Vault|SecretProviderClass|secrets-store\.csi' \
  "$repository_root/install.sh" "$repository_root/tools/install" \
  --glob '!deploy-platform.sh' >/dev/null &&
  fail 'retired secret backend remains in installer'

jq -e '
  .version == 1 and .namespace == "kodex-system" and (.secrets | length > 0) and
  ([.secrets[].name] | length == (unique | length)) and
  all(.secrets[]; (.items | type == "array" and length > 0) and
    ([.items[].key] | length == (unique | length)))
' "$repository_root/tools/install/secret-projections.json" >/dev/null ||
  fail 'secret projection registry contract is invalid'
rg -Fq '[.items[].key]' "$repository_root/tools/install/deploy-platform.sh" ||
  fail 'dynamic Secret readback does not use the projection item registry'
if rg -Fq 'gh variable get' "$repository_root/tools/install/configure-github.sh"; then
  fail 'GitHub variable readback relies on an unsupported gh subcommand'
fi
grep -Fq 'repos/$repository/actions/variables/$name' \
  "$repository_root/tools/install/configure-github.sh" ||
  fail 'GitHub variable readback does not use the repository REST endpoint'
grep -Fq 'while ((attempt <= 15))' "$repository_root/tools/install/configure-github.sh" ||
  fail 'GitHub variable readback does not have a bounded retry budget'
for recovery_contract in \
  'set_variable KODEX_CONTROL_TLS_RECOVERY_HOST' \
  'gh variable delete KODEX_CONTROL_TLS_RECOVERY_HOST' \
  '--control-tls-recovery-host "$CONTROL_TLS_RECOVERY_HOST"' \
  '.spec.dnsNames += [strenv(CONTROL_TLS_RECOVERY_HOST)]'; do
  rg -Fq -- "$recovery_contract" "$repository_root/tools/install/configure-github.sh" \
    "$repository_root/.github/workflows/deploy-production.yml" \
    "$repository_root/tools/release/render-web-only.sh" ||
    fail "Control Center TLS recovery contract is absent: $recovery_contract"
done
grep -Fq 'preflight-custom-resource-definitions' \
  "$repository_root/tools/install/deploy-platform.sh" ||
  fail 'platform preflight does not validate CustomResourceDefinitions separately'
grep -Fq '.apiVersion != \"$api_version\"' \
  "$repository_root/tools/install/deploy-platform.sh" ||
  fail 'platform preflight does not exclude exact API versions introduced by the render'
for preflight_contract in \
  'apply --server-side --dry-run=server' \
  '--field-manager=kodex-install' \
  '--public-tls-mode "$KODEX_PUBLIC_TLS_MODE"' \
  'KODEX_PUBLIC_TLS_MODE=deferred|enabled'; do
  rg -Fq -- "$preflight_contract" \
    "$repository_root/tools/install/deploy-platform.sh" \
    "$repository_root/tools/install/release-platform.sh" \
    "$repository_root/install.sh" "$repository_root/docs/runbooks/fresh-install.md" ||
    fail "platform release contract is absent: $preflight_contract"
done
rg -Fq 'reconcile_image_admission_policy_parameters' \
  "$repository_root/tools/install/deploy-platform.sh" ||
  fail 'immutable image admission parameters do not have a release reconciliation path'
grep -Fq 'KODEX_DISABLE_OBSERVABILITY=true' "$repository_root/.kodex-env.example" ||
  fail 'bundled install does not disable unavailable external telemetry exporters by default'
grep -Fq 'KODEX_DISABLE_OBSERVABILITY:-true' \
  "$repository_root/install.sh" "$repository_root/tools/install/configure-github.sh" ||
  fail 'installer and GitHub configuration disagree on the external telemetry default'
if rg -Fq 'select(.kind != "PodMonitor" and .kind != "ServiceMonitor" and .kind != "PrometheusRule")' \
  "$repository_root/tools/release/render-web-only.sh"; then
  fail 'external exporter disablement removes Prometheus discovery resources'
fi
for monitoring_kind in PodMonitor ServiceMonitor PrometheusRule; do
  MONITORING_KIND="$monitoring_kind" yq -e \
    'select(.kind == strenv(MONITORING_KIND))' \
    < <(kubectl kustomize "$repository_root/deploy/k8s/profiles/web-only") >/dev/null ||
    fail "web-only profile does not contain $monitoring_kind resources"
done
(
  runtime_directory="$temporary_directory/render-filter-runtime"
  render_file="$runtime_directory/release.yaml"
  temporary_directory="$runtime_directory/output"
  mkdir -p "$temporary_directory"
  printf '%s\n' 'apiVersion: v1' 'kind: Namespace' >"$render_file"
  yq() {
    command cat -- "$2"
  }
  # Execute the production helper under set -u instead of checking syntax only.
  source <(sed -n '/^render_filter() {$/,/^}$/p' \
    "$repository_root/tools/install/deploy-platform.sh")
  render_filter_output=$(render_filter known '.')
  [[ "$render_filter_output" == "$temporary_directory/known.yaml" ]] ||
    fail 'release render helper returned an unexpected path'
  cmp -s "$render_file" "$render_filter_output" ||
    fail 'release render helper did not materialize the filtered manifest'
)

for firewall_contract in \
  'systemctl disable --now nftables' \
  'nft delete table inet kodex_fw' \
  'ufw --force reset' \
  'ufw default deny routed' \
  'ufw route allow from "$pod_cidr"' \
  'ufw route allow proto tcp to "$pod_cidr" port 80' \
  'ufw route allow proto tcp to "$pod_cidr" port 443'; do
  rg -Fq "$firewall_contract" "$repository_root/tools/install/prepare-host.sh" ||
    fail "bare-metal firewall contract is absent: $firewall_contract"
done
rg -Fq 'legacy kodex_fw nftables policy remains active' \
  "$repository_root/tools/install/reset-host.sh" ||
  fail 'host reset does not reject the legacy nftables policy'
rg -Fq 'node_ready=true' "$repository_root/tools/install/prepare-host.sh" ||
  fail 'bare-metal installer does not wait for a ready Kubernetes node'
rg -Fq 'no ready Kubernetes node became available' \
  "$repository_root/tools/install/prepare-host.sh" ||
  fail 'bare-metal installer does not report a node readiness timeout'

identity_inputs="$temporary_directory/identity-inputs"
identity_material="$temporary_directory/identity-material"
mkdir -p "$identity_inputs" "$identity_material"
printf '%s' kodex-admin >"$identity_inputs/admin-username"
printf '%s' 'test-admin-initial-password' >"$identity_inputs/admin-password"
printf '%s' kodex-owner >"$identity_inputs/owner-username"
printf '%s' owner@example.com >"$identity_inputs/owner-email"
printf '%s' 'test-owner-initial-password' >"$identity_inputs/owner-password"
"$repository_root/tools/deploy/generate-identity-material.sh" \
  --material-directory "$identity_material" \
  --admin-username-file "$identity_inputs/admin-username" \
  --admin-initial-password-file "$identity_inputs/admin-password" \
  --owner-username-file "$identity_inputs/owner-username" \
  --owner-email-file "$identity_inputs/owner-email" \
  --owner-initial-password-file "$identity_inputs/owner-password" >/dev/null
for surface in control-center grafana headlamp; do
  cookie_secret="$identity_material/management/oauth2-$surface/cookie-secret"
  [[ "$(wc -c <"$cookie_secret")" -eq 32 ]] ||
    fail "generated OAuth2 Proxy cookie Secret length is invalid: $surface"
  [[ "$(stat -c '%a' "$cookie_secret")" == 600 ]] ||
    fail "generated OAuth2 Proxy cookie Secret mode is invalid: $surface"
done

printf 'Kodex install contract tests passed\n'
