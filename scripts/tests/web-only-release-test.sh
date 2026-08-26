#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Web-only release test failed: %s\n' "$*" >&2; exit 1; }
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
render="$temporary_directory/render.yaml"
kubectl kustomize "$repository_root/deploy/k8s/profiles/web-only" >"$render"

yq -o=json -I=0 '.' "$render" | jq -s -e '
  map(select(.kind != null)) as $resources |
  ($resources | length) > 0 and
  ($resources | group_by([.apiVersion,.kind,(.metadata.namespace // ""),.metadata.name]) |
    all(.[]; length == 1))
' >/dev/null || fail 'release render has duplicate resources'
if yq -e 'select(.kind == "SecretProviderClass" or
  (.apiVersion | test("secrets.hashicorp.com|vault.banzaicloud.com")))' "$render" >/dev/null 2>&1; then
  fail 'release render contains a retired secret provider resource'
fi
[[ $(yq -N -r 'select(.kind == "StatefulSet") | .metadata.name' "$render" | sort -u | wc -l) -eq 2 ]] ||
  fail 'web-only stateful dependency count is invalid'
for workload in kodex-postgresql kodex-nats; do
  WORKLOAD_NAME="$workload" yq -e \
    'select(.kind == "StatefulSet" and .metadata.name == strenv(WORKLOAD_NAME))' "$render" >/dev/null ||
    fail "stateful dependency is absent: $workload"
done
for job in kodex-postgresql-runtime-credentials internal-rpc-authority-migrate \
  control-plane-migrate control-plane-broker-bootstrap release-artifact-materializer; do
  JOB_NAME="$job" yq -e 'select(.kind == "Job" and .metadata.name == strenv(JOB_NAME))' \
    "$render" >/dev/null || fail "release Job is absent: $job"
done
yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[];
    .kind == "Job" and .metadata.name == "release-artifact-materializer" and
    any(.spec.template.spec.containers[0].env[];
      .name == "CONTROL_PLANE_SOURCE_REF" and
      (.value | contains("__KODEX_CONTROL_PLANE_SOURCE_REF__"))) and
    any(.spec.template.spec.containers[0].env[];
      .name == "CONTROL_PLANE_DIGEST" and
      .value == "sha256:0000000000000000000000000000000000000000000000000000000000000000") and
    any(.spec.template.spec.containers[0].env[];
      .name == "DOCKERFILE_SOURCE_REF" and
      (.value | contains("__KODEX_DOCKERFILE_SOURCE_REF__"))) and
    any(.spec.template.spec.containers[0].env[];
      .name == "DOCKERFILE_DIGEST" and
      .value == "sha256:0000000000000000000000000000000000000000000000000000000000000000"))
' >/dev/null || fail 'release bootstrap artifacts are absent from materialization'
grep -Fq 'select(.kind == "Deployment" and .metadata.name == "role-image-builder")' \
  "$repository_root/tools/install/deploy-platform.sh" ||
  fail 'role image builder is not applied after its release dependencies'

yq -e 'select(.kind == "ValidatingAdmissionPolicy" and
    .metadata.name == "internal-rpc-authority-restore-anchor-forward-only") |
  .spec.failurePolicy == "Fail" and
  ([.spec.matchConditions[] | select(.name == "exact-resource" and
    (.expression | contains("internal-rpc-authority-restore-evidence")))] | length == 1) and
  ([.spec.matchConditions[] | select(.name == "namespace-not-terminating" and
    (.expression | contains("namespaceObject != null")) and
    (.expression | contains("!has(namespaceObject.metadata.deletionTimestamp)")))] | length == 1) and
  ([.spec.validations[] | select((.expression | contains("request.operation")) and
    (.expression | contains("UPDATE"))) |
    select(.message == "restore evidence deletion is forbidden")] | length == 1)' \
  "$render" >/dev/null ||
  fail 'restore evidence policy does not preserve active protection and namespace teardown'

secret_references="$temporary_directory/secret-references"
secret_producers="$temporary_directory/secret-producers"
{
  yq -N -r '.. | select(tag == "!!map" and has("secretName")) | .secretName' "$render"
  yq -N -r '.. | select(tag == "!!map" and has("secretKeyRef")) | .secretKeyRef.name' "$render"
  yq -N -r '.. | select(tag == "!!map" and has("secretRef")) | .secretRef.name' "$render"
  yq -N -r '
    select(.kind == "Deployment" or .kind == "StatefulSet" or
      .kind == "DaemonSet" or .kind == "Job" or .kind == "CronJob") |
    (.spec.template.spec.imagePullSecrets // [])[]?.name
  ' "$render"
} | sed '/^null$/d;/^$/d' | sort -u >"$secret_references"
{
  jq -r '.secrets[].name' "$repository_root/tools/install/secret-projections.json"
  printf '%s\n' \
    internal-rpc-authority-bootstrap-roots \
    internal-rpc-authority-sentry \
    kodex-installation-ca \
    kodex-integration-credentials \
    kodex-nats-credentials \
    kodex-postgresql-bootstrap \
    kodex-postgresql-runtime-credentials \
    kodex-sentry \
    runtime-provider-openai-default-r1
  yq -N -r 'select(.kind == "Secret") | .metadata.name' "$render"
  yq -N -r 'select(.kind == "Certificate") | .spec.secretName' "$render"
} | sed '/^null$/d;/^$/d' | sort -u >"$secret_producers"
missing_secrets=$(comm -23 "$secret_references" "$secret_producers")
[[ -z "$missing_secrets" ]] ||
  fail "release references Kubernetes Secrets without a producer: ${missing_secrets//$'\n'/,}"

postgres_clients="$temporary_directory/postgres-clients"
postgres_allowed_clients="$temporary_directory/postgres-allowed-clients"
yq -o=json -I=0 '.' "$render" | jq -sr '
  .[] |
  if .kind == "CronJob" then .spec.jobTemplate.spec.template
  elif (.kind == "Deployment" or .kind == "StatefulSet" or
    .kind == "DaemonSet" or .kind == "Job") then .spec.template
  else empty end as $template |
  select(any($template.spec.containers[]?.env[]?;
    (.name // "") | test("POSTGRES.*DSN_FILE$"))) |
  $template.metadata.labels["app.kubernetes.io/name"]
' | sort -u >"$postgres_clients"
yq -o=json -I=0 '.' "$render" | jq -sr '
  .[] |
  select(.kind == "NetworkPolicy" and
    .metadata.name == "platform-postgresql-exact-clients") |
  .spec.ingress[].from[].podSelector.matchExpressions[] |
  select(.key == "app.kubernetes.io/name" and .operator == "In") |
  .values[]
' | sort -u >"$postgres_allowed_clients"
missing_postgres_clients=$(comm -23 "$postgres_clients" "$postgres_allowed_clients")
[[ -z "$missing_postgres_clients" ]] ||
  fail "PostgreSQL DSN consumers are denied by NetworkPolicy: ${missing_postgres_clients//$'\n'/,}"
grep -Fxq kodex-postgresql-runtime-credentials "$postgres_allowed_clients" ||
  fail 'PostgreSQL credential reconciler is denied by NetworkPolicy'

startup_readback_targets="$temporary_directory/startup-readback-targets"
attestor_ingress_clients="$temporary_directory/attestor-ingress-clients"
yq -N -r '
  select(.kind == "ConfigMap" and
    .metadata.name == "internal-rpc-authority-publisher-target-registry") |
  .data["key-delivery-targets.yaml"]
' "$render" | yq -N -r '
  .targets[] |
  select(.startup_readback_required == true) |
  .workload_id
' | sort -u >"$startup_readback_targets"
yq -N -r '
  select(.kind == "NetworkPolicy" and
    .metadata.name == "internal-rpc-authority-readback-attestor-exact-paths") |
  .spec.ingress[].from[].podSelector.matchExpressions[]? |
  select(.key == "app.kubernetes.io/name" and .operator == "In") |
  .values[]
' "$render" | sort -u >"$attestor_ingress_clients"
missing_readback_clients=$(comm -23 "$startup_readback_targets" "$attestor_ingress_clients")
[[ -z "$missing_readback_clients" ]] ||
  fail "startup readback targets are denied by attestor NetworkPolicy: ${missing_readback_clients//$'\n'/,}"

yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[];
    .kind == "CronJob" and
    .metadata.name == "internal-rpc-authority-restore-recovery" and
    any(.spec.jobTemplate.spec.template.spec.containers[];
      .name == "recovery" and
      any(.volumeMounts[];
        .name == "postgresql-ca" and
        .mountPath == "/var/run/config/kodex/internal-rpc-authority/postgresql" and
        .readOnly == true)) and
    any(.spec.jobTemplate.spec.template.spec.volumes[];
      .name == "postgresql-ca" and
      .configMap.name == "internal-rpc-authority-postgresql-ca" and
      any(.configMap.items[];
        .key == "ca.pem" and .path == "ca.pem")))
' >/dev/null || fail 'restore recovery PostgreSQL CA mount disagrees with its DSN'

for policy in internal-rpc-authority-restore-controller-exact-paths \
  internal-rpc-authority-restore-jobs-exact-paths \
  internal-rpc-authority-restore-pitr-telemetry; do
  yq -o=json -I=0 '.' "$render" | jq -s -e --arg policy "$policy" '
    any(.[];
      .kind == "NetworkPolicy" and .metadata.name == $policy and
      any(.spec.egress[];
        any(.to[]?; .ipBlock.cidr == "__KODEX_KUBERNETES_API_SERVICE_CIDR__") and
        any(.ports[]?; .protocol == "TCP" and .port == 443)))
  ' >/dev/null ||
    fail "restore workload is denied access to the Kubernetes API: $policy"
done
for policy in kodex-image-admission-controller-exact-paths \
  runtime-controller-exact-paths \
  internal-rpc-authority-publisher-exact-paths \
  internal-rpc-authority-restore-controller-exact-paths \
  internal-rpc-authority-restore-jobs-exact-paths \
  internal-rpc-authority-restore-pitr-telemetry; do
  [[ $(rg -F ".metadata.name == \"$policy\"" \
    "$repository_root/tools/release/render-web-only.sh" | wc -l) -eq 2 ]] ||
    fail "Kubernetes API endpoint render registry omits a client: $policy"
done
yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[];
    .kind == "Deployment" and .metadata.name == "role-image-builder" and
    any(.spec.template.spec.containers[]?.volumeMounts[]?;
      .name == "work" and .mountPath == "/work"))
' >/dev/null || fail 'role image builder workspace mount is not materializable'
yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[];
    .kind == "ConfigMap" and .metadata.name == "role-image-builder-runtime" and
    .data.ROLE_IMAGE_BUILDER_WORKSPACE_ROOT == "/work")
' >/dev/null || fail 'role image builder workspace configuration disagrees with its mount'

for script in "$repository_root/install.sh" "$repository_root/tools/install"/*.sh; do
  bash -n "$script"
done
if rg -qi 'vault|secrets-store\.csi|SecretProviderClass' \
  "$repository_root/deploy/k8s/profiles/web-only" \
  "$repository_root/tools/install/secret-projections.json"; then
  fail 'active release profile references retired secret delivery'
fi
printf 'Web-only release test completed\n'
