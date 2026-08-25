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
env_file="$temporary_directory/.kodex-env"
"$repository_root/tools/install/write-env-file.sh" --output "$env_file" >/dev/null
[[ "$(stat -c '%a' "$env_file")" == 600 ]] || fail '.kodex-env mode differs from 0600'

unset KODEX_INSTALL_MODE KODEX_NAMESPACE KODEX_KUBECONFIG KODEX_KUBE_CONTEXT
unset KODEX_RELEASE_REGISTRY_PASSWORD
# shellcheck source=../../tools/install/load-env.sh
source "$repository_root/tools/install/load-env.sh"
kodex_load_env "$env_file" || fail 'generated .kodex-env was not loaded'
[[ "$KODEX_INSTALL_MODE" == existing-kubernetes && "$KODEX_NAMESPACE" == kodex-system &&
  "$KODEX_RELEASE_REGISTRY_PASSWORD" == 'test-registry-password-with-equals==' ]] ||
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

printf 'Kodex install contract tests passed\n'
