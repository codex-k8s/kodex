#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Web-only release test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
lock_file="$temporary_directory/release-lock.json"
render_file="$temporary_directory/web-only.yaml"
source_sha=$(git -C "$repository_root" rev-parse HEAD)

jq -n --arg source_sha "$source_sha" \
  --slurpfile manifest "$repository_root/tools/release/images.json" '
  {schema_version:2,profile:"web-only",source_sha:$source_sha,build_run_id:"local",
   registry:{push:"registry.example.test:5000",node_pull:"registry.example.test:5001",repository_prefix:"mattercodex"},
   external_images:[{
     component:"admission-tools",
     pull_ref:"registry.example.test/tools/admission@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
     digest:"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}],
   images:[$manifest[0].images[] | {
     component:.component,
     repository:("mattercodex/" + .component),
     digest:"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
     pull_ref:("registry.example.test:5001/mattercodex/" + .component + "@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}]}
' >"$lock_file"
lock_sha256=$(sha256sum "$lock_file" | awk '{print $1}')

"$repository_root/tools/release/validate-release-lock.sh" \
  --lock "$lock_file" --source-sha "$source_sha" --sha256 "$lock_sha256" >/dev/null
"$repository_root/tools/release/render-web-only.sh" \
  --lock "$lock_file" --lock-sha256 "$lock_sha256" --output "$render_file" \
  --public-host console.example.test --public-origin https://console.example.test \
  --oidc-issuer https://identity.example.test/realms/mattercodex \
  --oidc-jwks-url https://identity.example.test/realms/mattercodex/protocol/openid-connect/certs \
  --oidc-connect-address identity.example.test:443 \
  --oidc-tls-server-name identity.example.test >/dev/null

if rg -n 'sha256:0{64}|__MATTERCODEX_[A-Z0-9_]+__|\.invalid|matter-kodex-prod|kodex\.works' "$render_file" >/dev/null; then
  fail 'render contains a forbidden deployment placeholder'
fi
if rg -ni 'bot-service|legacy-data-migration|interaction-gateway|mattermost' "$render_file" >/dev/null; then
  fail 'web-only render contains a retired or optional interaction unit'
fi

for deployment in control-plane control-api-gateway runtime-controller integration-gateway automation-scheduler role-image-builder staff-control-center; do
  DEPLOYMENT="$deployment" yq -e 'select(.kind == "Deployment" and .metadata.name == strenv(DEPLOYMENT))' "$render_file" >/dev/null ||
    fail "required deployment is absent: $deployment"
done

yq -e '
  select(.kind == "ConfigMap" and .metadata.name == "mattercodex-image-admission-policy") |
  .data.policyRevision == "1" and
  (.data.policySHA256 | test("^[a-f0-9]{64}$")) and
  (.data.trustedRoleBaseDigest | test("^sha256:[a-f0-9]{64}$")) and
  (.data.roleRuntimeContractSHA256 | test("^[a-f0-9]{64}$")) and
  .data.pullRegistryHost == "registry.example.test:5001"
' "$render_file" >/dev/null || fail 'role image release policy was not materialized'

printf 'Web-only release tests passed\n'
