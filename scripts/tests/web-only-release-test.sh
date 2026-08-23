#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Web-only release test failed: %s\n' "$*" >&2
  exit 1
}

verify_csi_secret_mounts() {
  local render_file=$1
  local secret_provider_class_count=0

  while IFS=$'\t' read -r provider_class encoded_objects; do
    if [[ -z "$provider_class" && -z "$encoded_objects" ]]; then
      continue
    fi
    [[ -n "$provider_class" && -n "$encoded_objects" ]] ||
      fail 'rendered SecretProviderClass has no bounded objects'
    printf '%s' "$encoded_objects" | base64 -d | yq -o=json '.' |
      jq -e '(length > 0) and all(.[]; .filePermission == 292)' >/dev/null ||
      fail "SecretProviderClass does not use exact read-only mode 0444: $provider_class"
    ((secret_provider_class_count += 1))
  done < <(yq -N -r '
    select(.kind == "SecretProviderClass") |
    [.metadata.name, (.spec.parameters.objects | @base64)] | @tsv
  ' "$render_file")
  ((secret_provider_class_count > 0)) || fail 'release render has no SecretProviderClass objects'

  yq -o=json 'select(.spec.template.spec != null)' "$render_file" |
    jq -s -e '
      length > 0 and all(.[];
        . as $workload |
        ($workload.spec.template.spec.volumes // [] |
          map(select(.csi.driver == "secrets-store.csi.k8s.io")) |
          map(.name)) as $csi_volumes |
        all($csi_volumes[];
          . as $volume_name |
          (($workload.spec.template.spec.volumes[] |
            select(.name == $volume_name) | .csi.readOnly) == true) and
          ([
            (($workload.spec.template.spec.initContainers // []) +
             ($workload.spec.template.spec.containers // []))[] |
            (.volumeMounts // [])[] |
            select(.name == $volume_name) |
            .readOnly
          ] | length > 0 and all(.[]; . == true))
        )
      )
    ' >/dev/null || fail 'CSI secret volume or container mount is not read-only'
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
dockerfile_path_validator="$repository_root/tools/release/validate-image-dockerfile-path.sh"
fresh_deployer="$repository_root/tools/deploy/deploy-fresh-release.sh"
legacy_resetter="$repository_root/tools/deploy/reset-legacy-installation.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

bash -n "$dockerfile_path_validator" "$fresh_deployer" "$legacy_resetter"
if rg -q 'local name=\$1[^\n]*\$name' "$fresh_deployer"; then
  fail 'fresh deploy expands a local variable in the declaration that assigns it'
fi
grep -Fq 'ensure_restore_evidence_anchor()' "$fresh_deployer" ||
  fail 'fresh deploy does not implement create-once restore evidence materialization'
[[ $(grep -c '^[[:space:]]*ensure_restore_evidence_anchor$' "$fresh_deployer") -eq 3 ]] ||
  fail 'fresh deploy does not validate restore evidence in state, workloads and readback phases'
[[ $(grep -c 'metadata.name == "internal-rpc-authority-restore-evidence"' "$fresh_deployer") -eq 2 ]] ||
  fail 'fresh deploy does not exclude restore evidence from both generic apply phases'
if grep -Fq 'kubectl apply --server-side --field-manager=mattercodex-fresh-install -f "$render_file"' \
  "$fresh_deployer"; then
  fail 'fresh workload deploy can overwrite the forward-only restore evidence anchor'
fi
grep -Fq 'select(.kind != "Job" and' "$fresh_deployer" ||
  fail 'fresh workload deploy can mutate immutable completed Jobs through generic apply'
[[ $(grep -c '^[[:space:]]*apply_job ' "$fresh_deployer") -eq 4 ]] ||
  fail 'fresh deploy does not retain explicit lifecycle for all four release Jobs'
grep -Fq 'replace_immutable_resource_on_drift()' "$fresh_deployer" ||
  fail 'fresh deploy does not implement bounded immutable resource replacement'
[[ $(grep -c '^[[:space:]]*rotate_release_immutable_resources$' "$fresh_deployer") -eq 2 ]] ||
  fail 'fresh deploy does not reconcile immutable resources in state and workload phases'
for immutable_name in mattercodex-role-environments mattercodex-image-admission-policy; do
  grep -Fq "replace_immutable_resource_on_drift configmap ConfigMap $immutable_name" "$fresh_deployer" ||
    fail "fresh deploy omits immutable ConfigMap lifecycle: $immutable_name"
done
grep -Fq 'imageadmissionpolicyparameters.supplychain.mattercodex.dev' "$fresh_deployer" ||
  fail 'fresh deploy omits immutable image admission parameters lifecycle'
grep -Fq 'cmp -s "$desired_payload" "$live_payload"' "$fresh_deployer" ||
  fail 'fresh deploy replaces immutable resources without semantic drift comparison'
yq -o=json 'select(.kind == "ValidatingAdmissionPolicy" and
  .metadata.name == "internal-rpc-authority-restore-anchor-forward-only")' \
  "$repository_root/deploy/k8s/base/internal-rpc-authority-restore/evidence-admission.yaml" |
  jq -e '
    .spec.failurePolicy == "Fail" and
    .spec.matchConstraints.resourceRules[0].operations == ["UPDATE", "DELETE"] and
    any(.spec.validations[];
      .message == "only the independent PITR executor may update restore evidence" and
      (.expression | contains("system:serviceaccount:mattercodex-system:internal-rpc-authority-restore-pitr")))
  ' >/dev/null ||
  fail 'restore evidence forward-only admission boundary was weakened'
grep -Fq 'mattercodex.dev/profile in (direct-production-single-node-prototype,web-only,web-with-mattermost)' \
  "$legacy_resetter" || fail 'incompatible reset does not use the closed release profile selector'
for cluster_kind in \
  customresourcedefinitions.apiextensions.k8s.io \
  validatingadmissionpolicies.admissionregistration.k8s.io \
  validatingadmissionpolicybindings.admissionregistration.k8s.io \
  clusterroles.rbac.authorization.k8s.io \
  clusterrolebindings.rbac.authorization.k8s.io \
  bundles.trust.cert-manager.io; do
  grep -Fq "$cluster_kind" "$legacy_resetter" ||
    fail "incompatible reset omits a release-owned cluster kind: $cluster_kind"
done
grep -Fq 'verify_release_cluster_scope_absent' "$legacy_resetter" ||
  fail 'incompatible reset does not read back cluster-scope cleanup'
grep -Fq 'apply_filter custom-resource-definitions' "$fresh_deployer" ||
  fail 'fresh deploy does not establish custom resource definitions before parameters'
grep -Fq 'kubectl wait --for=condition=Established' "$fresh_deployer" ||
  fail 'fresh deploy does not wait for custom resource definition discovery'
cluster_cleanup_line=$(grep -n '^[[:space:]]*delete_release_cluster_scope$' "$legacy_resetter" |
  cut -d: -f1)
namespace_cleanup_line=$(grep -n 'kubectl delete namespace "$legacy_namespace"' "$legacy_resetter" |
  cut -d: -f1)
[[ -n "$cluster_cleanup_line" && -n "$namespace_cleanup_line" &&
  "$cluster_cleanup_line" -lt "$namespace_cleanup_line" ]] ||
  fail 'release admission policies are not removed before namespace deletion'
while IFS= read -r dockerfile; do
  "$dockerfile_path_validator" "$dockerfile"
  [[ -f "$repository_root/$dockerfile" ]] || {
    printf 'Release image Dockerfile is absent: %s\n' "$dockerfile" >&2
    exit 1
  }
done < <(jq -r '.images[].dockerfile' "$repository_root/tools/release/images.json")
for invalid_dockerfile in \
  services/jobs/agent-runner/../Dockerfile \
  services/jobs/agent-runner/dockerfile \
  services/jobs/agent-runner/.Dockerfile \
  infra/admission-tools/Dockerfile \
  services/unknown/example/Dockerfile; do
  if "$dockerfile_path_validator" "$invalid_dockerfile" >/dev/null 2>&1; then
    printf 'Release image Dockerfile validator accepted an unsafe path: %s\n' \
      "$invalid_dockerfile" >&2
    exit 1
  fi
done
lock_file="$temporary_directory/release-lock.json"
render_file="$temporary_directory/web-only.yaml"
source_sha=$(git -C "$repository_root" rev-parse HEAD)

setup_go_action=actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e
for workflow in build-release deploy-production; do
  SETUP_GO_ACTION="$setup_go_action" yq -e \
    '.jobs[] | .steps[] | select(.uses == strenv(SETUP_GO_ACTION)) | ((.with."go-version-file" == "go.mod") and (.with.cache == false))' \
    "$repository_root/.github/workflows/$workflow.yml" >/dev/null ||
    fail "exact Go toolchain step is absent: $workflow"
done
yq -e '
  .jobs.render.steps[] |
  select(.run == "tools/release/install-render-tools.sh")
' "$repository_root/.github/workflows/deploy-production.yml" >/dev/null ||
  fail 'exact render toolchain step is absent'
[[ -x "$repository_root/tools/release/install-render-tools.sh" ]] ||
  fail 'render tool installer is not executable'
for pinned_tool_contract in \
  'kubectl_version=v1.35.5' \
  'kubectl_sha256=90f75ea6ecc9ea5633262e1c0b83a40560003b30fc94a04cb099404fcef0c224' \
  'yq_version=v4.53.6' \
  'yq_sha256=c5f056448f973ae7d39b5401949648a78f2dc1947d6a8eb65be60d5c504b9385'; do
  grep -Fxq "$pinned_tool_contract" "$repository_root/tools/release/install-render-tools.sh" ||
    fail "render tool pin is absent: ${pinned_tool_contract%%=*}"
done

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
  --promoted-pull-host roles.example.test \
  --kubernetes-api-service-cidr 10.96.0.1/32 \
  --ingress-class public --cluster-issuer public-production \
  --ingress-namespace ingress-system --ingress-pod-name public-ingress \
  --oidc-namespace identity --oidc-pod-name sso \
  --oidc-pod-component identity-provider --oidc-target-port 8443 >/dev/null

if rg -n 'sha256:0{64}|__MATTERCODEX_[A-Z0-9_]+__|\.invalid|matter-kodex-prod|kodex\.works|runtime-provider-auth' "$render_file" >/dev/null; then
  fail 'render contains a forbidden deployment placeholder'
fi
if rg -ni 'bot-service|legacy-data-migration|mattermostMode' "$render_file" >/dev/null; then
  fail 'web-only render contains a retired interaction unit'
fi

verify_csi_secret_mounts "$render_file"

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
  select(.kind == "NetworkPolicy") |
  select((.spec | tostring) | test("interaction-gateway|[Mm]attermost"))
' "$render_file" >/dev/null 2>&1; then
  fail 'web-only NetworkPolicy grants the optional interaction adapter'
fi

if yq -e '
  select(
    .kind == "Namespace" or
    .kind == "CustomResourceDefinition" or
    .kind == "ClusterRole" or
    .kind == "ClusterRoleBinding" or
    .kind == "ValidatingAdmissionPolicy" or
    .kind == "ValidatingAdmissionPolicyBinding" or
    .kind == "ValidatingWebhookConfiguration" or
    .kind == "MutatingWebhookConfiguration" or
    .kind == "ClusterIssuer" or
    .kind == "Bundle"
  ) |
  select(.metadata.namespace != null)
' "$render_file" >/dev/null 2>&1; then
  fail 'web-only render assigns a namespace to a cluster-scoped resource'
fi
admission_command_expression=$(yq -N -r '
  select(.kind == "ValidatingAdmissionPolicy" and
    .metadata.name == "mattercodex-image-admission-controller-jobs") |
  .spec.validations[] |
  select(.message == "Image admission executable or container privileges differ from the owner contract.") |
  .expression
' "$render_file")
for command_contract in \
  'variables.main.command.size() == 3' \
  'variables.main.command[0] ==' \
  '/bin/sh' \
  'variables.main.command[1] ==' \
  '/opt/mattercodex/image-admission.sh' \
  'variables.main.command[2] == variables.phase'; do
  [[ "$admission_command_expression" == *"$command_contract"* ]] ||
    fail "image admission command CEL contract is absent: $command_contract"
done
[[ "$admission_command_expression" != *'variables.phase]'* ]] ||
  fail 'image admission command CEL contract still contains a heterogeneous list literal'

yq -o=json '
  select(.kind == "CustomResourceDefinition" and
    .metadata.name == "imageadmissionpolicyparameters.supplychain.mattercodex.dev")
' "$render_file" | jq -e '
  .spec.group == "supplychain.mattercodex.dev" and
  .spec.scope == "Namespaced" and
  .spec.versions[0].name == "v1alpha1" and
  .spec.versions[0].served == true and
  .spec.versions[0].storage == true and
  (.spec.versions[0].schema.openAPIV3Schema.properties.spec as $schema |
    ($schema.required | length) == 28 and
    ($schema.required | sort) == ($schema.properties | keys | sort) and
    all($schema.properties[]; .type == "string") and
    any($schema."x-kubernetes-validations"[]; .rule == "self == oldSelf"))
' >/dev/null || fail 'typed image admission policy parameter CRD is incomplete'

yq -o=json '
  select(.kind == "ValidatingAdmissionPolicy" and
    .metadata.name == "mattercodex-image-admission-controller-jobs")
' "$render_file" | jq -e '
  .spec.failurePolicy == "Fail" and
  .spec.paramKind.apiVersion == "supplychain.mattercodex.dev/v1alpha1" and
  .spec.paramKind.kind == "ImageAdmissionPolicyParameters" and
  all(.spec.validations[]; (.expression | contains("params.data.")) | not) and
  any(.spec.validations[]; .expression | contains("params.spec.policyRevision"))
' >/dev/null || fail 'image admission policy does not use typed fail-closed parameters'

yq -o=json '
  select(.kind == "ValidatingAdmissionPolicyBinding" and
    .metadata.name == "mattercodex-image-admission-controller-jobs")
' "$render_file" | jq -e '
  .spec.validationActions == ["Deny"] and
  .spec.paramRef.name == "mattercodex-image-admission-policy" and
  .spec.paramRef.namespace == "mattercodex-system" and
  .spec.paramRef.parameterNotFoundAction == "Deny"
' >/dev/null || fail 'typed image admission policy binding is not fail-closed'

for stateful_set in mattercodex-postgresql mattercodex-nats; do
  STATEFUL_SET="$stateful_set" yq -e '
    select(.kind == "StatefulSet" and .metadata.name == strenv(STATEFUL_SET)) |
    .spec.replicas == 1 and
    .spec.volumeClaimTemplates[0].spec.storageClassName == null and
    (.spec.volumeClaimTemplates[0].spec.resources.requests.storage | length > 0)
  ' "$render_file" >/dev/null ||
    fail "fresh stateful dependency is absent or binds an installation-specific storage class: $stateful_set"
done

yq -o=json 'select(.kind == "NetworkPolicy" and
  .metadata.name == "platform-postgresql-exact-clients")' "$render_file" |
  jq -e '
    .spec.policyTypes == ["Ingress", "Egress"] and
    .spec.egress == [] and
    (.spec.ingress | length) == 1 and
    (.spec.ingress[0].ports == [{"protocol":"TCP","port":5432}]) and
    (.spec.ingress[0].from | length) == 1 and
    (.spec.ingress[0].from[0].podSelector.matchExpressions | length) == 1 and
    (.spec.ingress[0].from[0].podSelector.matchExpressions[0] as $selector |
      $selector.key == "app.kubernetes.io/name" and
      $selector.operator == "In" and
      ($selector.values | sort) == ["control-plane", "internal-rpc-authority", "vault"])
  ' >/dev/null || fail 'PostgreSQL ingress does not expose the exact Vault database engine path'

yq -o=json -I=0 'select(.kind == "NetworkPolicy")' "$render_file" | jq -s -e '
  all(.[];
    ([.. | objects | select(has("app.kubernetes.io/name")) |
      .["app.kubernetes.io/name"]] |
      all(. != "internal-rpc-authority-postgresql" and . != "control-plane-postgresql"))) and
  all(.[] | select(any(.spec.egress[]?.ports[]?; .port == 5432));
    all(.spec.egress[] | select(any(.ports[]?; .port == 5432));
      all(.to[]; .podSelector.matchLabels["app.kubernetes.io/name"] == "mattercodex-postgresql")))
' >/dev/null || fail 'PostgreSQL NetworkPolicy points to a DNS alias instead of the actual workload label'

yq -o=json 'select(.kind == "NetworkPolicy" and
  .metadata.name == "internal-rpc-authority-postgresql-from-migrator")' "$render_file" |
  jq -e '
    .spec.podSelector.matchLabels == {"app.kubernetes.io/name":"mattercodex-postgresql"} and
    .spec.ingress == [{
      "from":[{"podSelector":{"matchLabels":{
        "app.kubernetes.io/name":"internal-rpc-authority",
        "app.kubernetes.io/component":"migrator"}}}],
      "ports":[{"port":5432,"protocol":"TCP"}]
    }]
  ' >/dev/null || fail 'PostgreSQL migration ingress does not select the actual workload and exact client'

yq -o=json 'select(.kind == "NetworkPolicy" and
  .metadata.name == "internal-rpc-authority-postgresql-from-runtime")' "$render_file" |
  jq -e '
    .spec.podSelector.matchLabels == {"app.kubernetes.io/name":"mattercodex-postgresql"} and
    any(.spec.ingress[0].from[];
      .podSelector.matchExpressions == [{
        "key":"mattercodex.dev/image-admission-phase",
        "operator":"In",
        "values":["claim","admit","promote"]
      }])
  ' >/dev/null || fail 'PostgreSQL runtime ingress omits the exact image admission clients'

yq -o=json 'select(.kind == "NetworkPolicy" and
  .metadata.name == "platform-vault-from-csi-provider")' "$render_file" |
  jq -e '
    .spec.podSelector.matchLabels == {"app.kubernetes.io/name":"vault"} and
    .spec.policyTypes == ["Ingress"] and
    (.spec.ingress | length) == 1 and
    .spec.ingress[0].ports == [{"protocol":"TCP","port":8200}] and
    .spec.ingress[0].from == [{"podSelector":{"matchLabels":{
      "app.kubernetes.io/name":"vault-csi-provider"}}}]
  ' >/dev/null || fail 'Vault ingress does not expose the exact CSI provider path'

for service in control-plane-postgresql-rw internal-rpc-authority-postgresql-rw nats; do
  SERVICE="$service" yq -e '
    select(.kind == "Service" and .metadata.name == strenv(SERVICE))
  ' "$render_file" >/dev/null || fail "fresh stateful service is absent: $service"
done

for certificate in mattercodex-postgresql mattercodex-nats control-plane-nats-client control-plane-nats-bootstrap-client control-api-gateway-nats-client; do
  CERTIFICATE="$certificate" yq -e '
    select(.kind == "Certificate" and .metadata.name == strenv(CERTIFICATE)) |
    .spec.issuerRef.name == "mattercodex-installation-ca"
  ' "$render_file" >/dev/null || fail "fresh TLS certificate contract is absent: $certificate"
done

for bundle in control-plane-postgresql-ca internal-rpc-authority-postgresql-ca mattercodex-nats-ca; do
  BUNDLE="$bundle" yq -e '
    select(.kind == "Bundle" and .metadata.name == strenv(BUNDLE)) |
    .metadata.namespace == null and
    .spec.sources[0].secret.name == "mattercodex-installation-ca" and
    .spec.target.configMap.key == "ca.pem"
  ' "$render_file" >/dev/null || fail "fresh CA bundle contract is absent or incorrectly namespaced: $bundle"
done

yq -o=json 'select(.kind == "StatefulSet" and .metadata.name == "mattercodex-postgresql")' "$render_file" |
  jq -e '
    .spec.template.spec.containers[] | select(.name == "postgresql") |
    .env[] | select(.name == "POSTGRES_PASSWORD_FILE") |
    .value == "/var/run/bootstrap/password"
  ' >/dev/null || fail 'PostgreSQL bootstrap password is not file-backed'
yq -o=json 'select(.kind == "StatefulSet" and .metadata.name == "mattercodex-nats")' "$render_file" |
  jq -e '
    .spec.template.spec.volumes[] | select(.name == "credentials") |
    .secret.secretName == "mattercodex-nats-credentials"
  ' >/dev/null || fail 'NATS operator/account material is not secret-backed'
yq -o=json 'select(.kind == "Deployment" and .metadata.name == "control-plane")' "$render_file" |
  jq -e '
    .spec.template.spec.containers[] | select(.name == "control-plane") |
    .env[] | select(.name == "CONTROL_PLANE_NATS_REPLICAS") |
    .value == "1"
  ' >/dev/null || fail 'control-plane stream replication does not match the shipped NATS topology'
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

yq -o=json 'select(.kind == "Job" and .metadata.name == "internal-rpc-authority-migrate")' "$render_file" |
  jq -e '
    .spec.template.spec.containers[] | select(.name == "migrate") |
    .command == ["/usr/local/bin/internal-rpc-authority-cli"] and
    .args == ["up"] and
    any(.env[]; .name == "INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE" and
      .value == "/var/run/secrets/mattercodex/internal-rpc-authority/postgres/dsn")
  ' >/dev/null || fail 'fresh internal RPC authority migration command is inconsistent with the CLI contract'

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

yq -o=json 'select(.kind == "Deployment" and .metadata.name == "runtime-controller")' "$render_file" |
  jq -e '
    .spec.template.spec.containers[] | select(.name == "runtime-controller") |
    any(.env[]; .name == "RUNTIME_CONTROLLER_DEFAULT_ROLE_IMAGE_REFERENCE" and
      .valueFrom.configMapKeyRef.name == "mattercodex-image-admission-policy" and
      .valueFrom.configMapKeyRef.key == "nodeReadbackImage")
  ' >/dev/null || fail 'runtime-controller does not accept the exact release default role image'

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
  .data.pullRegistryHost == "roles.example.test"
' "$render_file" >/dev/null || fail 'role image release policy was not materialized'

admission_policy_config_map_json=$(yq -o=json -I=0 '
  select(.kind == "ConfigMap" and .metadata.name == "mattercodex-image-admission-policy") |
  .data
' "$render_file" | jq -Sc .)
admission_policy_parameters_json=$(yq -o=json -I=0 '
  select(.apiVersion == "supplychain.mattercodex.dev/v1alpha1" and
    .kind == "ImageAdmissionPolicyParameters" and
    .metadata.name == "mattercodex-image-admission-policy") |
  .spec
' "$render_file" | jq -Sc .)
[[ -n "$admission_policy_parameters_json" &&
  "$admission_policy_parameters_json" == "$admission_policy_config_map_json" ]] ||
  fail 'typed image admission parameters drifted from the runtime ConfigMap projection'

yq -o=json '
  select(.kind == "Role" and .metadata.name == "image-admission-controller")
' "$render_file" | jq -e '
  any(.rules[];
    .apiGroups == ["supplychain.mattercodex.dev"] and
    .resources == ["imageadmissionpolicyparameters"] and
    .resourceNames == ["mattercodex-image-admission-policy"] and
    .verbs == ["get"])
' >/dev/null || fail 'image admission controller lacks exact typed parameter read access'

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
  --promoted-pull-host roles.example.test \
  --kubernetes-api-service-cidr 10.96.0.1/32 \
  --ingress-class public --cluster-issuer public-production \
  --ingress-namespace ingress-system --ingress-pod-name public-ingress \
  --oidc-namespace identity --oidc-pod-name sso \
  --oidc-pod-component identity-provider --oidc-target-port 8443 >/dev/null

verify_csi_secret_mounts "$mattermost_render"

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
