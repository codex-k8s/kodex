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
yq -e 'select(.apiVersion == "cert-manager.io/v1" and .kind == "Certificate" and
    .metadata.name == "staff-control-center-public") |
  .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
  .metadata.labels["kodex.dev/owner-intent"] == "true" and
  .spec.secretName == "staff-control-center-public-tls"' "$render" >/dev/null ||
  fail 'public TLS Certificate disagrees with the installer ownership contract'
[[ $(yq -N -r 'select(.kind == "StatefulSet") | .metadata.name' "$render" | sort -u | wc -l) -eq 3 ]] ||
  fail 'web-only stateful dependency count is invalid'
for workload in kodex-postgresql kodex-nats email-bridge-postgresql; do
  WORKLOAD_NAME="$workload" yq -e \
    'select(.kind == "StatefulSet" and .metadata.name == strenv(WORKLOAD_NAME))' "$render" >/dev/null ||
    fail "stateful dependency is absent: $workload"
done
yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[];
    .kind == "Deployment" and .metadata.name == "session-archive" and
    .metadata.namespace == "kodex-system" and
    any(.spec.template.spec.containers[];
      .name == "session-archive" and
      .image == "kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/session-archive@sha256:0000000000000000000000000000000000000000000000000000000000000000" and
      (.image as $sessionImage |
        any(.env[];
          .name == "SESSION_ARCHIVE_WORKER_IMAGE" and .value == $sessionImage))) and
    any(.spec.template.spec.containers[];
      .name == "internal-rpc-authority-issuer") and
    any(.spec.template.spec.containers[];
      .name == "platform-worker-grant-agent")) and
  any(.[];
    .kind == "NetworkPolicy" and
    .metadata.name == "session-archive-exact-paths" and
    any(.spec.egress[];
      any(.to[]?;
        .ipBlock.cidr == "__KODEX_KUBERNETES_API_SERVICE_CIDR__") and
      any(.ports[]?; .protocol == "TCP" and .port == 443)))
' >/dev/null || fail 'session archive release wiring is incomplete'
yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[];
    .kind == "Deployment" and .metadata.name == "control-plane" and
    any(.spec.template.spec.containers[];
      .name == "control-plane" and
      any(.env[]; .name == "CONTROL_PLANE_SECRET_BROKER_TARGET" and
        .value == "dns:///secret-broker.kodex-system.svc:8443") and
      any(.env[]; .name == "CONTROL_PLANE_SECRET_BROKER_TLS_SERVER_NAME" and
        .value == "secret-broker.kodex-system.svc.cluster.local") and
      any(.env[]; .name == "CONTROL_PLANE_PROVIDER_AUTHORITY_RESOLVER_TARGET" and
        .value == "dns:///127.0.0.1:8443") and
      any(.env[]; .name == "CONTROL_PLANE_PROVIDER_AUTHORITY_RESOLVER_TLS_SERVER_NAME" and
        .value == "control-plane.kodex-system.svc.cluster.local")) and
    any(.spec.template.spec.containers[];
      .name == "internal-rpc-authority-issuer" and
      .readinessProbe.httpGet.path == "/readyz" and
      any(.env[]; .name == "INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER" and
        .valueFrom.secretKeyRef.name == "internal-rpc-authority-control-plane-issuer-postgresql")) and
    any(.spec.template.spec.containers[];
      .name == "control-plane-platform-worker-grant-agent" and
      .readinessProbe.httpGet.path == "/readyz") and
    ([.spec.template.spec.volumes[]?.secret.secretName |
      select(. == "internal-rpc-authority-control-plane-issuer-key" or
        . == "internal-rpc-authority-control-plane-issuer-postgresql" or
        . == "internal-rpc-authority-control-plane-issuer-readback-credential" or
        . == "internal-rpc-authority-control-plane-issuer-readback-possession" or
        . == "internal-rpc-authority-control-plane-issuer-restore-credential" or
        . == "internal-rpc-authority-control-plane-issuer-restore-ack")] | unique | length) == 6) and
  any(.[];
    .kind == "Deployment" and .metadata.name == "secret-broker" and
    any(.spec.template.spec.containers[];
      .name == "secret-broker" and
      any(.env[]; .name == "INTERNAL_RPC_AUTHORITY_VERIFIER_SOCKET" and
        .value == "/run/kodex/internal-rpc-authority/verifier.sock")) and
    any(.spec.template.spec.containers[];
      .name == "internal-rpc-authority-verifier" and
      .readinessProbe.httpGet.path == "/readyz" and
      any(.env[]; .name == "INTERNAL_RPC_AUTHORITY_WORKLOAD_ID" and .value == "secret-broker") and
      any(.env[]; .name == "INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER" and
        .valueFrom.secretKeyRef.name == "internal-rpc-authority-secret-broker-verifier-postgresql")) and
    ([.spec.template.spec.volumes[]?.secret.secretName |
      select(. == "internal-rpc-authority-secret-broker-verifier-postgresql" or
        . == "internal-rpc-authority-secret-broker-verifier-readback-credential" or
        . == "internal-rpc-authority-secret-broker-verifier-readback-possession" or
        . == "internal-rpc-authority-secret-broker-verifier-restore-credential" or
        . == "internal-rpc-authority-secret-broker-verifier-restore-ack")] | unique | length) == 5)
' >/dev/null || fail 'provider credential authority sidecars and readiness are incomplete'
yq -o=json -I=0 '.' "$render" | jq -s -e '
  map(select(.kind != null)) as $resources |
  any($resources[];
    .kind == "ConfigMap" and .metadata.name == "runtime-controller-runtime" and
    .data.RUNTIME_CONTROLLER_SECRET_BROKER_TARGET ==
      "dns:///secret-broker.kodex-system.svc:8443" and
    .data.RUNTIME_CONTROLLER_SECRET_BROKER_TLS_SERVER_NAME ==
      "secret-broker.kodex-system.svc.cluster.local" and
    .data.RUNTIME_CONTROLLER_SECRET_BROKER_CA_FILE ==
      "/var/run/config/kodex/runtime-controller/control-plane/ca.pem") and
  any($resources[];
    .kind == "NetworkPolicy" and
    .metadata.name == "runtime-controller-exact-paths" and
    any(.spec.egress[];
      any(.to[]?.podSelector.matchLabels?;
        .["app.kubernetes.io/name"] == "secret-broker" and
        .["app.kubernetes.io/component"] == "secret-broker") and
      any(.ports[]?; .protocol == "TCP" and .port == 8443))) and
  any($resources[];
    .kind == "PrometheusRule" and .metadata.name == "runtime-controller" and
    any(.spec.groups[].rules[];
      .alert == "RuntimeWorkspaceCanaryFailed" and
      .annotations.runbook_url ==
        "https://github.com/codex-k8s/kodex/blob/main/docs/runbooks/runtime-controller.md"))
' >/dev/null || fail 'runtime controller credential projection render contract is incomplete'
yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[]; .kind == "NetworkPolicy" and
    .metadata.name == "control-plane-internal-rpc-authority-issuer-exact-paths" and
    any(.spec.egress[]; any(.to[]?.podSelector.matchLabels?;
      .["app.kubernetes.io/name"] == "kodex-postgresql") and
      any(.ports[]; .protocol == "TCP" and .port == 5432)) and
    any(.spec.egress[]; any(.to[]?.podSelector.matchLabels?;
      .["app.kubernetes.io/name"] == "internal-rpc-authority-readback-attestor") and
      any(.ports[]; .protocol == "TCP" and .port == 8443)) and
    any(.spec.egress[]; any(.to[]?.podSelector.matchLabels?;
      .["app.kubernetes.io/name"] == "internal-rpc-authority-restore-controller") and
      any(.ports[]; .protocol == "TCP" and .port == 8443))) and
  any(.[]; .kind == "NetworkPolicy" and .metadata.name == "control-plane-exact-runtime-paths" and
    any(.spec.egress[]; any(.to[]?.podSelector.matchLabels?;
      .["app.kubernetes.io/name"] == "secret-broker" and
      .["app.kubernetes.io/component"] == "secret-broker") and
      any(.ports[]; .protocol == "TCP" and .port == 8443))) and
  any(.[]; .kind == "NetworkPolicy" and .metadata.name == "secret-broker-exact-runtime-paths" and
    any(.spec.ingress[]; any(.from[]?.podSelector.matchLabels?;
      .["app.kubernetes.io/name"] == "control-plane") and
      any(.ports[]; .protocol == "TCP" and .port == 8443)) and
    any(.spec.ingress[]; any(.from[]?.podSelector.matchLabels?;
      .["app.kubernetes.io/name"] == "runtime-controller" and
      .["app.kubernetes.io/component"] == "internal-controller") and
      any(.ports[]; .protocol == "TCP" and .port == 8443)) and
    any(.spec.ingress[]; any(.from[]?.podSelector.matchLabels?;
      .["app.kubernetes.io/name"] == "stt-tts-service" and
      .["app.kubernetes.io/component"] == "internal-service") and
      any(.ports[]; .protocol == "TCP" and .port == 8443))) and
  any(.[]; .kind == "Service" and .metadata.name == "secret-broker" and
    any(.spec.ports[]; .name == "verify-metrics" and .port == 9092)) and
  any(.[]; .kind == "Role" and .metadata.name == "internal-rpc-authority-publisher" and
    (["internal-rpc-authority-control-plane-issuer-key",
      "internal-rpc-authority-control-plane-issuer-readback-credential",
      "internal-rpc-authority-control-plane-issuer-readback-possession",
      "internal-rpc-authority-control-plane-issuer-restore-credential",
      "internal-rpc-authority-control-plane-issuer-restore-ack",
      "internal-rpc-authority-secret-broker-verifier-readback-credential",
      "internal-rpc-authority-secret-broker-verifier-readback-possession",
      "internal-rpc-authority-secret-broker-verifier-restore-credential",
      "internal-rpc-authority-secret-broker-verifier-restore-ack"] -
      .rules[0].resourceNames | length) == 0 and
    (.rules[0].verbs | sort) == ["get", "update"]) and
  any(.[]; .kind == "Role" and .metadata.name == "secret-broker-runtime-secrets" and
    (.rules[0].resources | sort) == ["secrets"] and
    (.rules[0].verbs | sort) == ["create", "delete", "get", "list", "update"]) and
  any(.[]; .kind == "RoleBinding" and .metadata.name == "internal-rpc-authority-publisher" and
    (.rules | not) and .roleRef.kind == "Role" and
    .roleRef.name == "internal-rpc-authority-publisher")
' >/dev/null || fail 'provider credential TLS and exact network paths are incomplete'
yq -N -r '
  select(.kind == "ConfigMap" and
    .metadata.name == "internal-rpc-authority-publisher-target-registry") |
  .data["key-delivery-targets.yaml"]
' "$render" | yq -e '
  .source_revision == 7 and
  ([.targets[] | select(.workload_id == "control-plane" and
    .role == "AUTHORIZATION_ISSUER" and
    .database_identity.login_principal == "ira_control_plane_issuer_g1" and
    .auth_private_key.secret_name == "internal-rpc-authority-control-plane-issuer-key" and
    .readback.credential_secret_name == "internal-rpc-authority-control-plane-issuer-readback-credential" and
    .readback.possession_key_secret_name == "internal-rpc-authority-control-plane-issuer-readback-possession" and
    .restore_coordination.role_credential_secret_name == "internal-rpc-authority-control-plane-issuer-restore-credential" and
    .restore_coordination.ack_key_secret_name == "internal-rpc-authority-control-plane-issuer-restore-ack")] | length) == 1 and
  ([.targets[] | select(.workload_id == "secret-broker" and
    .role == "AUTHORIZATION_VERIFIER" and
    .database_identity.login_principal == "ira_secret_broker_verifier_g1" and
    .readback.credential_secret_name == "internal-rpc-authority-secret-broker-verifier-readback-credential" and
    .readback.possession_key_secret_name == "internal-rpc-authority-secret-broker-verifier-readback-possession" and
    .restore_coordination.role_credential_secret_name == "internal-rpc-authority-secret-broker-verifier-restore-credential" and
    .restore_coordination.ack_key_secret_name == "internal-rpc-authority-secret-broker-verifier-restore-ack")] | length) == 1
' >/dev/null || fail 'provider credential publisher delivery targets are incomplete'
yq -N -r '
  select(.kind == "ConfigMap" and
    .metadata.name == "internal-rpc-authority-publisher-target-registry") |
  .data["authority-policy.json"]
' "$render" | jq -e '
  .policy_revision == 53 and
  ([.policy.authority_proof_producers[] |
    select(.producer_id == "secret-broker.provider-credential-materializer" and
      .caller_workload_id == "control-plane" and
      .application_credential == "PLATFORM_WORKER_GRANT" and
      .authority_sources == ["DOMAIN_STATE"] and
      (.allowed_operation_ids | index("platform.provider-credentials.cleanup")) != null)] | length) == 1 and
  ([.policy.operation_bindings[] |
    select(.operation_id == "platform.provider-credentials.cleanup" and
      .permission == "platform.provider-credentials.cleanup" and
      .full_method == "/controlplane.v1.ProviderCredentialMaterializerService/CleanupProviderCredential" and
      .caller_workload_id == "control-plane" and
      .caller_spiffe_id == "spiffe://kodex.local/ns/kodex-system/sa/control-plane" and
      .target_workload_id == "secret-broker" and
      .target_spiffe_id == "spiffe://kodex.local/ns/kodex-system/sa/secret-broker" and
      .audience == "urn:kodex:internal-rpc:secret-broker" and
      .target_tls_server_name == "secret-broker.kodex-system.svc.cluster.local" and
      .authority_proof_producer_id == "secret-broker.provider-credential-materializer" and
      .authority_sources == ["DOMAIN_STATE"] and
      .project_required == false)] | length) == 1 and
  ([.policy.operation_bindings[] |
    select(.operation_id == "platform.runtime.credentials.materialize" and
      .caller_workload_id == "runtime-controller" and
      .target_workload_id == "secret-broker" and
      .project_required == true and
      .full_method == "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeRuntimeCredentials")] | length) == 1 and
  ([.policy.operation_bindings[] |
    select(.operation_id == "platform.runtime.credentials.system-assistant.materialize" and
      .caller_workload_id == "runtime-controller" and
      .target_workload_id == "secret-broker" and
      .project_required == false and
      .full_method == "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeSystemAssistantCredentials")] | length) == 1 and
  ([.policy.operation_bindings[] |
    select(.operation_id == "platform.stt.credential.project" and
      .caller_workload_id == "stt-tts-service" and
      .target_workload_id == "secret-broker" and
      .project_required == false and
      .full_method == "/stt.v1.TranscriptionCredentialProjectionService/ProjectTranscriptionCredential")] | length) == 1
' >/dev/null || fail 'secret broker protected operation profiles are incomplete'
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

yq -o=json -I=0 '.' "$render" | jq -s -e '
  map(select(.kind != null)) as $resources |
  any($resources[];
    .kind == "ConfigMap" and
    .metadata.name == "kodex-image-admission-policy" and
    .data.providerAppArmorProfile == "") and
  any($resources[];
    .kind == "ValidatingAdmissionPolicy" and
    .metadata.name == "runtime-revision-exact-configmap-projection" and
    .spec.failurePolicy == "Fail" and
    ([.spec.validations[].expression] | join(" ") | contains(
      "workspace-policy.json")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "provider-auth.sha256")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "size(object.data) == 9")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "runtime.kodex.dev/execution-binding-digest")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "runtime.kodex.dev/organization-hash"))) and
  any($resources[];
    .kind == "ValidatingAdmissionPolicyBinding" and
    .metadata.name == "runtime-revision-exact-configmap-projection" and
    .spec.policyName == "runtime-revision-exact-configmap-projection" and
    .spec.validationActions == ["Deny"]) and
  any($resources[];
    .kind == "ValidatingAdmissionPolicy" and
    .metadata.name == "runtime-execution-ticket-exact-projection" and
    .spec.failurePolicy == "Fail" and
    ([.spec.validations[].expression] | join(" ") | contains(
      "system:serviceaccount:kodex-system:runtime-controller")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "!has(object.stringData)")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "size(object.data) == 2")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "^environment-[a-f0-9]{16}$")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "runtime.kodex.dev/environment-digest"))) and
  any($resources[];
    .kind == "ValidatingAdmissionPolicyBinding" and
    .metadata.name == "runtime-execution-ticket-exact-projection" and
    .spec.policyName == "runtime-execution-ticket-exact-projection" and
    .spec.validationActions == ["Deny"]) and
  any($resources[];
    .kind == "ValidatingAdmissionPolicy" and
    .metadata.name == "runtime-execution-service-account" and
    .spec.failurePolicy == "Fail" and
    ([.spec.matchConditions[].expression] | join(" ") | contains(
      "system:serviceaccount:kodex-system:runtime-controller")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "runtime-sa-[a-f0-9]{16}"))) and
  any($resources[];
    .kind == "ValidatingAdmissionPolicy" and
    .metadata.name == "runtime-execution-network-policy" and
    .spec.failurePolicy == "Fail" and
    .spec.paramKind == {"apiVersion":"v1","kind":"ConfigMap"} and
    ([.spec.validations[].expression] | join(" ") | contains(
      "params.data['\''kubernetes-api-service-cidr'\'']"))) and
  any($resources[];
    .kind == "ValidatingAdmissionPolicyBinding" and
    .metadata.name == "runtime-execution-network-policy" and
    .spec.paramRef.name == "runtime-materialization-admission-parameters" and
    .spec.paramRef.namespace == "kodex-system" and
    .spec.paramRef.parameterNotFoundAction == "Deny") and
  any($resources[];
    .kind == "ValidatingAdmissionPolicy" and
    .metadata.name == "runtime-role-pod-exact-secret-projection" and
    .spec.failurePolicy == "Fail" and
    .spec.paramKind == {"apiVersion":"v1","kind":"ConfigMap"} and
    ([.spec.validations[].expression] | join(" ") | contains(
      "runtime-sa-[a-f0-9]{16}")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "item.valueFrom.secretKeyRef.name")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "runtime.kodex.dev/credential-projection-name")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "runtime-credentials-[a-f0-9]{40}")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "container.name != '\''provider-runtime'\''")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "runtime-projection-[a-f0-9]{16}")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "mount.name == '\''runtime-ticket'\''")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "mount.mountPath == '\''/workspace/input'\''")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "fsGroupChangePolicy == '\''OnRootMismatch'\''")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "quantity('\''1Gi'\'')")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "container.securityContext.allowPrivilegeEscalation == false")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "params.data['\''providerAppArmorProfile'\''] == '\'''\''")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "!has(variables.providerContainers[0].securityContext.appArmorProfile)")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "params.data['\''providerAppArmorProfile'\''] == '\''kodex-provider-runtime'\''")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "providerContainers[0].securityContext.appArmorProfile.localhostProfile")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "runtime-provider-credential-relay")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "params.data['\''nodeReadbackImage'\'']")) and
    ([.spec.validations[].expression] | join(" ") | contains(
      "compareTo(quantity('\''100m'\''))"))) and
  any($resources[];
    .kind == "ValidatingAdmissionPolicyBinding" and
    .metadata.name == "runtime-role-pod-exact-secret-projection" and
    .spec.policyName == "runtime-role-pod-exact-secret-projection" and
    .spec.paramRef.name == "kodex-image-admission-policy" and
    .spec.paramRef.namespace == "kodex-system" and
    .spec.paramRef.parameterNotFoundAction == "Deny" and
    .spec.validationActions == ["Deny"]) and
  any($resources[];
    .kind == "Role" and .metadata.name == "runtime-controller" and
    any(.rules[];
      .apiGroups == [""] and .resources == ["secrets"] and
      ((.verbs | sort) == (["create", "delete", "get"] | sort))) and
    any(.rules[];
      .apiGroups == [""] and .resources == ["configmaps"] and
      ((.verbs | sort) == (["create", "delete", "get", "list"] | sort)))) and
  any($resources[];
    .kind == "ServiceAccount" and .metadata.name == "agent-runner" and
    .automountServiceAccountToken == false)
' >/dev/null ||
  fail 'runtime Secret materialization admission boundary is incomplete'
if yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[];
    .kind == "Secret" and
    ((.data // {}) | keys | any(. == "runtime.json" or
      test("^environment-[a-f0-9]{16}$"))))
' >/dev/null; then
  fail 'release render embeds a runtime input or environment Secret projection'
fi

secret_references="$temporary_directory/secret-references"
secret_producers="$temporary_directory/secret-producers"
rg -Fq 'create secret generic kodex-external-s3' "$repository_root/install.sh" ||
  fail 'external object storage Secret does not have an installer producer'
{
  yq -N -r '.. | select(tag == "!!map" and has("secretName")) | .secretName' "$render"
  yq -N -r '.. | select(tag == "!!map" and has("secretKeyRef")) | .secretKeyRef.name' "$render"
  yq -N -r '.. | select(tag == "!!map" and has("secretRef")) | .secretRef.name' "$render"
  yq -N -r '
    select(.kind == "Deployment" or .kind == "StatefulSet" or
      .kind == "DaemonSet" or .kind == "Job" or .kind == "CronJob") |
    (.spec.template.spec.imagePullSecrets // [])[]?.name
  ' "$render"
  yq -N -r '
    select(.kind == "ServiceAccount") |
    (.imagePullSecrets // [])[]?.name
  ' "$render"
} | sed '/^null$/d;/^$/d' | sort -u >"$secret_references"
{
  jq -r '.secrets[].name' "$repository_root/tools/install/secret-projections.json"
  printf '%s\n' \
    internal-rpc-authority-bootstrap-roots \
    internal-rpc-authority-sentry \
    backup-controller-credentials \
    kodex-installation-ca \
    kodex-external-s3 \
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
yq -e '
  select(.kind == "Deployment" and .metadata.name == "backup-controller") |
  (.spec.strategy.type == "Recreate") and
  (.spec.template.spec.automountServiceAccountToken == false)
' "$render" >/dev/null || fail 'backup-controller release workload contract is incomplete'
yq -e '
  select(.kind == "Deployment" and .metadata.name == "backup-controller") |
  .spec.template.spec.containers[] |
  select(.name == "backup-controller") |
  .image | test("/backup-controller@sha256:[a-f0-9]{64}$")
' "$render" >/dev/null || fail 'backup-controller release image reference is invalid'
yq -e '
  select(.kind == "Deployment" and .metadata.name == "backup-controller") |
  .spec.template.spec.volumes[] |
  select(.name == "credentials") |
  .secret.secretName == "backup-controller-credentials"
' "$render" >/dev/null || fail 'backup-controller credential projection is invalid'
yq -e '
  select(.kind == "Deployment" and .metadata.name == "backup-controller") |
  .spec.template.spec.volumes[] |
  select(.name == "tls") |
  .configMap.name == "backup-controller-postgresql-ca"
' "$render" >/dev/null || fail 'backup-controller PostgreSQL CA projection is invalid'
jq -e '
  any(.images[];
    .component == "backup-controller" and
    .dockerfile == "services/jobs/backup-controller/Dockerfile")
' "$repository_root/tools/release/images.json" >/dev/null ||
  fail 'backup-controller image contract is absent'
if yq -e 'select(.kind == "ServiceAccount" and
  ((.imagePullSecrets // []) | length > 0))' "$render" >/dev/null 2>&1; then
  fail 'ServiceAccount bypasses the canonical node registry credential path'
fi

postgres_clients="$temporary_directory/postgres-clients"
postgres_allowed_clients="$temporary_directory/postgres-allowed-clients"
yq -o=json -I=0 '.' "$render" | jq -sr '
  .[] |
  (if .kind == "CronJob" then .spec.jobTemplate.spec.template
  elif (.kind == "Deployment" or .kind == "StatefulSet" or
    .kind == "DaemonSet" or .kind == "Job") then .spec.template
  else empty end) as $template |
  select(any($template.spec.containers[]?.env[]?;
    (.name // "") | test("POSTGRES.*DSN_FILE$"))) |
  $template.metadata.labels["app.kubernetes.io/name"]
' | sort -u >"$postgres_clients"
yq -o=json -I=0 '.' "$render" | jq -sr '
  .[] |
  select(.kind == "NetworkPolicy" and
    .spec.podSelector.matchLabels["app.kubernetes.io/name"] == "kodex-postgresql") |
  .spec.ingress[]? | select(any(.ports[]?; .port == 5432)) |
  .from[]?.podSelector |
  .matchLabels["app.kubernetes.io/name"] //
  (.matchExpressions[]? | select(.key == "app.kubernetes.io/name" and .operator == "In") | .values[])
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
yq -o=json -I=0 '.' "$render" | jq -sr '
  .[] | select(.kind == "NetworkPolicy" and
    .spec.podSelector.matchLabels["app.kubernetes.io/name"] == "internal-rpc-authority-readback-attestor") |
  .spec.ingress[]? | select(any(.ports[]?; .port == 8443)) |
  .from[]?.podSelector |
  .matchLabels["app.kubernetes.io/name"] //
  (.matchExpressions[]? | select(.key == "app.kubernetes.io/name" and .operator == "In") | .values[])
' | sort -u >"$attestor_ingress_clients"
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
  session-archive-exact-paths \
  internal-rpc-authority-publisher-exact-paths \
  internal-rpc-authority-restore-controller-exact-paths \
  internal-rpc-authority-restore-jobs-exact-paths \
  internal-rpc-authority-restore-pitr-telemetry; do
  [[ $(sed -n '/^api_client_policy_count=/,/^if /p' \
    "$repository_root/tools/release/render-web-only.sh" |
    rg -F ".metadata.name == \"$policy\"" | wc -l) -eq 2 ]] ||
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
