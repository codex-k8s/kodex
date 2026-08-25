#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Management surfaces test failed: %s\n' "$*" >&2; exit 1; }
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
bootstrap="$repository_root/infra/management-surfaces/bootstrap.sh"
routes="$repository_root/infra/management-surfaces/routes.yaml"
values="$repository_root/infra/management-surfaces/oauth2-proxy-values.yaml"
lock="$repository_root/infra/management-surfaces/charts.lock.json"

bash -n "$bootstrap"
jq -e '
  .schemaVersion == 1 and (.charts | length) == 3 and
  ([.charts[].name] | sort) == ["headlamp","kube-prometheus-stack","oauth2-proxy"] and
  all(.charts[]; (.sha256 | test("^[a-f0-9]{64}$")))
' "$lock" >/dev/null || fail 'management chart lock is invalid'
for surface in control-center grafana headlamp; do
  rg -q "oauth2-$surface" "$bootstrap" || fail "OAuth2 surface is absent: $surface"
done
for ingress in kodex-grafana kodex-headlamp; do
  INGRESS_NAME="$ingress" yq -e \
    'select(.kind == "Ingress" and .metadata.name == strenv(INGRESS_NAME))' "$routes" >/dev/null ||
    fail "management Ingress is absent: $ingress"
done
kubectl kustomize "$repository_root/deploy/k8s/profiles/web-only" | yq -e \
  'select(.kind == "Ingress" and .metadata.name == "staff-control-center")' >/dev/null ||
  fail 'Control Center Ingress is absent from the platform release'
yq -e '.extraArgs."allowed-role" == "__KODEX_ALLOWED_ROLE__"' "$values" >/dev/null ||
  fail 'OAuth2 Proxy role gate is absent'
if rg -qi 'vault' "$repository_root/infra/management-surfaces"; then
  fail 'retired Vault management surface remains active'
fi
printf 'Management surfaces test completed\n'
