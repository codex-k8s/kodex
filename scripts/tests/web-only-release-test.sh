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
   registry:{push:"registry.example.test",node_pull:"registry.example.test:5001",repository_prefix:"mattercodex"},
   external_images:[{
     component:"admission-tools",
     pull_ref:"registry.example.test/tools/admission@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
     digest:"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}],
   role_image_input:{
     repository:"mattercodex/role-image-inputs",
     manifest_digest:"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
     payload_sha256:"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
     source_sha256:"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
     pull_ref:"registry.example.test:5001/mattercodex/role-image-inputs@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
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

role_environment_catalog=$(yq -r 'select(.kind == "ConfigMap" and .metadata.name == "mattercodex-role-environments") | .data."catalog.json"' "$render_file")
jq -e '
  .schemaVersion == 1 and (.environments | length) == 2 and
  .environments[0].key == "standard" and .environments[1].key == "documents" and
  (.environments[0].baseImageDigest | test("^sha256:[a-f0-9]{64}$")) and
  (.environments[1].baseImageDigest | test("^sha256:[a-f0-9]{64}$")) and
  (.context.contextRef | contains("mattercodex/role-image-inputs@sha256:"))
' <<<"$role_environment_catalog" >/dev/null || fail 'role environment catalog was not materialized'

yq -o=json 'select(.kind == "Job" and .metadata.name == "release-artifact-materializer")' "$render_file" |
  jq -e '
    .spec.template.spec.containers[0].env as $env |
    ($env[] | select(.name == "RELEASE_SOURCE_REGISTRY").value) == "registry.example.test" and
    ($env[] | select(.name == "AGENT_RUNNER_SOURCE_REF").value | startswith("registry.example.test/mattercodex/agent-runner@sha256:")) and
    ($env[] | select(.name == "ROLE_BASE_DOCUMENTS_SOURCE_REF").value | startswith("registry.example.test/mattercodex/role-base-documents@sha256:")) and
    ($env[] | select(.name == "ROLE_IMAGE_INPUT_SOURCE_REF").value | startswith("registry.example.test/mattercodex/role-image-inputs@sha256:"))
  ' >/dev/null || fail 'release artifact materializer was not pinned to the lock'

egress_policy=$(yq -r 'select(.kind == "ConfigMap" and (.metadata.name | test("^egress-gateway-policy-"))) | .data."policy.json"' "$render_file")
jq -e 'any(.spec.destinations[]; .hostname == "registry.example.test" and .port == 443)' \
  <<<"$egress_policy" >/dev/null || fail 'release registry was not added to bounded egress policy'
printf '%s' "$egress_policy" >"$temporary_directory/egress-policy.json"
actual_egress_digest=$(
  cd -- "$repository_root/services/external/egress-gateway"
  GOWORK=off go run ./cmd/policy-digest "$temporary_directory/egress-policy.json"
)
expected_egress_digest=$(yq -r '
  select(.kind == "Deployment" and .metadata.name == "egress-gateway") |
  .spec.template.spec.containers[] | select(.name == "egress-gateway") |
  .env[] | select(.name == "EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST").value
' "$render_file")
test "$actual_egress_digest" = "$expected_egress_digest" || fail 'egress policy digest expectation is inconsistent'

printf 'Web-only release tests passed\n'
