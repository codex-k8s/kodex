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
  --oidc-tls-server-name identity.example.test \
  --kubernetes-api-service-cidr 10.96.0.1/32 >/dev/null

if rg -n 'sha256:0{64}|__MATTERCODEX_[A-Z0-9_]+__|\.invalid|matter-kodex-prod|kodex\.works|runtime-provider-auth' "$render_file" >/dev/null; then
  fail 'render contains a forbidden deployment placeholder'
fi
if rg -ni 'bot-service|legacy-data-migration|mattermostMode' "$render_file" >/dev/null; then
  fail 'web-only render contains a retired interaction unit'
fi
if yq -e '
  select(
    (.kind == "Deployment" or .kind == "Service" or .kind == "ServiceAccount" or
     .kind == "PodDisruptionBudget" or .kind == "ServiceMonitor" or
     .kind == "SecretProviderClass") and
    (.metadata.name | test("(^|-)interaction-gateway($|-)|[Mm]attermost"))
  )
' "$render_file" >/dev/null 2>&1; then
  fail 'web-only render materializes the optional interaction adapter'
fi
if yq -e '
  select(.kind == "Deployment" and .metadata.name == "control-plane") |
  any(.spec.template.spec.containers[] | select(.name == "control-plane") | .env[]?;
    .name == "CONTROL_PLANE_INTERACTION_GRANT_TRUST_FILE")
' "$render_file" >/dev/null 2>&1; then
  fail 'web-only control-plane requires optional interaction grant trust'
fi

for deployment in control-plane control-api-gateway runtime-controller integration-gateway automation-scheduler role-image-builder image-admission-controller staff-control-center; do
  DEPLOYMENT="$deployment" yq -e 'select(.kind == "Deployment" and .metadata.name == strenv(DEPLOYMENT))' "$render_file" >/dev/null ||
    fail "required deployment is absent: $deployment"
  DEPLOYMENT="$deployment" yq -o=json 'select(.kind == "Deployment" and .metadata.name == strenv(DEPLOYMENT))' "$render_file" |
    jq -e --arg name "$deployment" '
      .spec.template.spec.containers[] | select(.name == $name) |
      .startupProbe.httpGet.path == "/healthz" and
      .readinessProbe.httpGet.path == "/readyz" and
      .livenessProbe.httpGet.path == "/healthz"
    ' >/dev/null || fail "application probes do not follow the local snapshot contract: $deployment"
done

invalid_probe=$(
  yq -r '
    select(.kind == "Deployment") |
    .metadata.name as $deployment |
    .spec.template.spec.containers[] |
    select(
      (.startupProbe.httpGet.path != null and .startupProbe.httpGet.path != "/healthz") or
      (.readinessProbe.httpGet.path != null and .readinessProbe.httpGet.path != "/readyz") or
      (.livenessProbe.httpGet.path != null and .livenessProbe.httpGet.path != "/healthz")
    ) |
    $deployment + "/" + .name
  ' "$render_file"
)
[[ -z "$invalid_probe" ]] || fail "render contains a probe outside the health/readiness contract: $invalid_probe"

yq -o=json 'select(.kind == "Job" and .metadata.name == "control-plane-migrate")' "$render_file" |
  jq -e '
    .spec.template.spec.containers[] | select(.name == "migrate") |
    .command == ["/usr/local/bin/control-plane-cli"] and
    .args == ["up"] and
    any(.env[]; .name == "CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE" and
      .value == "/var/run/secrets/mattercodex/control-plane/postgres-migration/dsn")
  ' >/dev/null || fail 'fresh control-plane migration command is inconsistent with the CLI contract'

yq -o=json 'select(.kind == "Deployment" and .metadata.name == "control-api-gateway")' "$render_file" |
  jq -e '
    .spec.template.spec.containers[] | select(.name == "control-api-gateway") |
    (.volumeMounts | map(.name)) as $mounts |
    all(["nats-client-tls", "nats-credential", "nats-ca"][]; . as $name | $mounts | index($name) != null)
  ' >/dev/null || fail 'control API realtime NATS materials are not mounted'

yq -o=json '
  select(.kind == "NetworkPolicy" and .metadata.name == "control-api-gateway-exact-runtime-paths")
' "$render_file" | jq -e '
  any(.spec.egress[];
    any(.to[]?; .podSelector.matchLabels."app.kubernetes.io/name" == "mattercodex-nats") and
    any(.ports[]?; .protocol == "TCP" and .port == 4222))
' >/dev/null || fail 'control API realtime NATS egress is absent'

yq -o=json 'select(.kind == "NetworkPolicy" and .metadata.name == "egress-gateway-exact-runtime-paths")' "$render_file" |
  jq -e '
    any(.spec.ingress[].from[]?;
      .podSelector.matchLabels."app.kubernetes.io/name" == "agent-runner" and
      .podSelector.matchLabels."app.kubernetes.io/component" == "role-runtime" and
      .podSelector.matchLabels."runtime.mattercodex.dev/managed" == "true") and
    (any(.spec.ingress[].from[]?;
      .podSelector.matchLabels."app.kubernetes.io/name" == "runtime-controller" and
      .podSelector.matchLabels."app.kubernetes.io/component" == "hot-runtime") | not)
  ' >/dev/null || fail 'managed role runtime provider path is absent from egress gateway ingress'

yq -o=json 'select(.kind == "Deployment" and .metadata.name == "control-plane")' "$render_file" |
  jq -e '
    (.spec.template.spec.containers[] | select(.name == "control-plane") | .env) as $env |
    def source($name): first($env[] | select(.name == $name) | .valueFrom.configMapKeyRef.name);
    source("CONTROL_PLANE_DEFAULT_PROVIDER_SECRET_NAME") == "runtime-provider-openai-default-metadata" and
    source("CONTROL_PLANE_DEFAULT_PROVIDER_SECRET_UID") == "runtime-provider-openai-default-metadata" and
    source("CONTROL_PLANE_DEFAULT_PROVIDER_SECRET_RESOURCE_VERSION") == "runtime-provider-openai-default-metadata" and
    source("CONTROL_PLANE_DEFAULT_PROVIDER_CREDENTIAL_SHA256") == "runtime-provider-openai-default-metadata"
  ' >/dev/null || fail 'control-plane provider credential metadata binding is incomplete'

test -f "$repository_root/contracts/runtime-controller/v4/agent-runner-input.schema.json" ||
  fail 'runtime input v4 schema is absent'
test ! -e "$repository_root/contracts/runtime-controller/v3" ||
  fail 'retired runtime input v3 contract remains'
jq -e '
  .properties.schema.const == "mattercodex.agent-runner-input.v4" and
  (.required | index("provider_account_ref") != null) and
  (.required | index("provider_credential_revision_ref") != null) and
  (.required | index("provider_credential_sha256") != null)
' "$repository_root/contracts/runtime-controller/v4/agent-runner-input.schema.json" >/dev/null ||
  fail 'runtime input v4 provider affinity contract is incomplete'

api_policy_matches=$(yq -e '
  select(.kind == "NetworkPolicy" and
    (.metadata.name == "mattercodex-image-admission-controller-exact-paths" or
     .metadata.name == "runtime-controller-exact-paths")) |
  .spec.egress[] | select(.to[].ipBlock.cidr == "10.96.0.1/32") |
  ((.ports | length) == 1 and .ports[0].protocol == "TCP" and .ports[0].port == 443)
' "$render_file" | grep -c '^true$')
if [[ $api_policy_matches -ne 2 ]]; then
  fail 'Kubernetes API Service egress is not bound to the rendered host CIDR'
fi
if go run "$repository_root/tools/release/validate-host-cidr.go" 10.96.0.0/24 >/dev/null 2>&1; then
  fail 'non-host Kubernetes API CIDR was accepted'
fi

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

mattermost_lock="$temporary_directory/web-with-mattermost-lock.json"
mattermost_render="$temporary_directory/web-with-mattermost.yaml"
jq '.profile = "web-with-mattermost"' "$lock_file" >"$mattermost_lock"
mattermost_lock_sha256=$(sha256sum "$mattermost_lock" | awk '{print $1}')
"$repository_root/tools/release/validate-release-lock.sh" \
  --lock "$mattermost_lock" --source-sha "$source_sha" --sha256 "$mattermost_lock_sha256" \
  --profile web-with-mattermost >/dev/null
"$repository_root/tools/release/render-web-only.sh" \
  --lock "$mattermost_lock" --lock-sha256 "$mattermost_lock_sha256" --output "$mattermost_render" \
  --profile web-with-mattermost \
  --mattermost-host chat.example.test \
  --public-host console.example.test --public-origin https://console.example.test \
  --oidc-issuer https://identity.example.test/realms/mattercodex \
  --oidc-jwks-url https://identity.example.test/realms/mattercodex/protocol/openid-connect/certs \
  --oidc-connect-address identity.example.test:443 \
  --oidc-tls-server-name identity.example.test \
  --kubernetes-api-service-cidr 10.96.0.1/32 >/dev/null

yq -o=json 'select(.kind == "Deployment" and .metadata.name == "interaction-gateway")' "$mattermost_render" |
  jq -e '
    .spec.template.spec.containers as $containers |
    any($containers[]; .name == "interaction-gateway" and
      .startupProbe.httpGet.path == "/healthz" and
      .readinessProbe.httpGet.path == "/readyz" and
      .livenessProbe.httpGet.path == "/healthz") and
    any($containers[]; .name == "internal-rpc-authority-issuer") and
    any($containers[]; .name == "platform-worker-grant-agent")
  ' >/dev/null || fail 'Mattermost profile does not materialize the optional adapter boundary'
yq -e '
  select(.kind == "ConfigMap" and .metadata.name == "interaction-gateway-runtime") |
  .data.INTERACTION_GATEWAY_ALLOWED_HOSTS == "chat.example.test"
' "$mattermost_render" >/dev/null || fail 'Mattermost profile lost its installation-level host allowlist'
if rg -ni 'bot-service|legacy-data-migration|interaction-gateway-postgresql|mattermostMode' "$mattermost_render" >/dev/null; then
  fail 'Mattermost profile contains a retired core dependency'
fi
yq -o=json 'select(.kind == "NetworkPolicy" and .metadata.name == "egress-gateway-exact-runtime-paths")' "$mattermost_render" |
  jq -e '
    any(.spec.ingress[].from[]?;
      .podSelector.matchLabels."app.kubernetes.io/name" == "interaction-gateway" and
      .podSelector.matchLabels."app.kubernetes.io/component" == "interaction-adapter")
  ' >/dev/null || fail 'Mattermost adapter is not an exact egress-gateway client'
mattermost_egress_policy=$(yq -r 'select(.kind == "ConfigMap" and (.metadata.name | test("^egress-gateway-policy-"))) | .data."policy.json"' "$mattermost_render")
jq -e 'any(.spec.destinations[]; .hostname == "chat.example.test" and .port == 443)' \
  <<<"$mattermost_egress_policy" >/dev/null || fail 'Mattermost host is absent from bounded egress policy'

printf 'Web-only and optional Mattermost release tests passed\n'
