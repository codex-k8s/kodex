#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local Kubernetes API egress contract test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
renderer="$repository_root/tools/dev/render-local.sh"

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
