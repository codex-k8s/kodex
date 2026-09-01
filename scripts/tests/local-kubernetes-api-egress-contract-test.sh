#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local Kubernetes API egress contract test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
renderer="$repository_root/tools/dev/render-local.sh"
endpoint_configurator="$repository_root/tools/dev/configure-local-api-endpoint.sh"

[[ -x "$endpoint_configurator" ]] || fail 'stable local API endpoint configurator is not executable'
for contract in \
  'KODEX_DEV_KUBERNETES_API_ADDRESS:-10.254.254.1' \
  'interface=kodex-api0' \
  '."advertise-address" = strenv(KODEX_LOCAL_API_ADDRESS)' \
  'Before=k3s.service' \
  'unique) == [$address]' \
  'Kubernetes API is unreachable through the stable address with verified TLS'; do
  grep -Fq "$contract" "$endpoint_configurator" ||
    fail "stable local API endpoint contract is absent: $contract"
done
grep -Fq 'tools/dev/configure-local-api-endpoint.sh' "$repository_root/dev.sh" ||
  fail 'dev.sh does not configure and read back the stable local API endpoint'
grep -Fq 'systemctl disable --now kodex-local-api-address.service' \
  "$repository_root/tools/install/reset-host.sh" ||
  fail 'full host reset does not remove the stable local API endpoint service'

grep -Fq 'API_SERVICE_CIDR="$kubernetes_service_cidr"' "$renderer" ||
  fail 'Kubernetes service CIDR input is absent'
grep -Fq '.ipBlock.cidr) = strenv(API_ENDPOINT_CIDR)' "$renderer" ||
  fail 'exact local API endpoint substitution is absent'
grep -Fq '(strenv(API_ENDPOINT_PORT) | tonumber)' "$renderer" ||
  fail 'exact local API endpoint port substitution is absent'
grep -Fq '.data.oidcConnectAddress = "sso.identity.svc.cluster.local:443"' "$renderer" ||
  fail 'OIDC service connect address is absent'
grep -Fq 'select(.port == "__KODEX_OIDC_TARGET_PORT__").port) = 8443' "$renderer" ||
  fail 'OIDC endpoint target port is not allowed by the local NetworkPolicy'

printf 'Kodex local Kubernetes API egress contract test passed\n'
