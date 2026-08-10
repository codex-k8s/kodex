#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Application render failed: %s\n' "$*" >&2; exit 1; }
usage() { printf 'Usage: %s --lock <path> --output <path> --scope release|bootstrap|interfaces\n' "$0" >&2; }

lock_file=""
output=""
scope=""
while (($# > 0)); do
  case "$1" in
    --lock) lock_file="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    --scope) scope="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
[[ -n "$output" ]] || fail "output path is required"
case "$scope" in release|bootstrap|interfaces) ;; *) fail "scope must be release, bootstrap or interfaces" ;; esac
if [[ "$scope" == release ]]; then
  [[ -r "$lock_file" ]] || fail "release lock is not readable"
fi
for command_name in kubectl yq jq; do command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"; done

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
combined="$temporary_directory/combined.yaml"
: >"$combined"

components=(
  control-plane
  internal-rpc-authority
  runtime-controller
  interaction-gateway
  integration-gateway
  control-api-gateway
  staff-control-center
  automation-scheduler
  role-image-builder
  egress-gateway
)
for component in "${components[@]}"; do
  kubectl kustomize "$repository_root/deploy/k8s/overlays/production/$component" >>"$combined"
  printf '%s\n' '---' >>"$combined"
done

SCOPE="$scope" yq eval-all '
  select(.kind != null) |
  select(
    ((strenv(SCOPE) == "release" or strenv(SCOPE) == "interfaces") and
      (.kind == "ConfigMap" or .kind == "Service" or .kind == "Deployment" or
       .kind == "DaemonSet" or .kind == "Job" or .kind == "CronJob")) or
    (strenv(SCOPE) == "bootstrap" and
      (.kind == "ServiceAccount" or .kind == "Role" or .kind == "RoleBinding" or
       .kind == "ClusterRole" or .kind == "ClusterRoleBinding" or
       .kind == "ValidatingAdmissionPolicy" or .kind == "ValidatingAdmissionPolicyBinding" or
       .kind == "NetworkPolicy"))
  ) |
  select((.metadata.name | test("^(mattercodex-(buildkit|image-|registry-)|.*role-image-builder.*|.*-dashboard$)")) | not)
' "$combined" >"$temporary_directory/filtered.yaml"

# Direct-production prototype заменяет Vault CSI mounts на материализованные
# владельцем Kubernetes Secrets, сохраняя имена и пути файлов интерфейса.
yq eval-all '
  select(.kind != null) |
  with(select(.metadata.namespace != null); .metadata.namespace = "mattercodex-system") |
  .metadata.labels."mattercodex.dev/profile" = "direct-production-single-node-prototype" |
  .metadata.labels."mattercodex.dev/release-managed" = "true" |
  with(select(.kind == "Deployment" or .kind == "DaemonSet" or .kind == "Job");
    .spec.template.metadata.labels."mattercodex.dev/profile" = "direct-production-single-node-prototype" |
    .spec.template.metadata.labels."mattercodex.dev/release-managed" = "true" |
    .spec.template.spec.automountServiceAccountToken = false |
    .spec.template.spec.containers[] |= (
      .env = ((.env // []) |
        map(select((.name | test("^(OTEL_|SENTRY_)")) | not)) +
        [{"name": "OTEL_SDK_DISABLED", "value": "true"}]) |
      with(select(.name == "publisher" or .name == "reconciler" or
          .name == "internal-rpc-authority-issuer" or
          .name == "internal-rpc-authority-verifier");
        .env += [
          {"name":"INTERNAL_RPC_AUTHORITY_SECRET_BACKEND","value":"direct-production-kubernetes-file"},
          {"name":"INTERNAL_RPC_AUTHORITY_DEPLOYMENT_PROFILE","valueFrom":{"fieldRef":{"fieldPath":"metadata.labels['mattercodex.dev/profile']"}}}
        ]
      ) |
      .volumeMounts = ((.volumeMounts // []) |
        map(select((.name | test("otel|sentry|observability")) | not)))
    ) |
    .spec.template.spec.initContainers[]? |= (
      .env = ((.env // []) |
        map(select((.name | test("^(OTEL_|SENTRY_)")) | not)) +
        [{"name": "OTEL_SDK_DISABLED", "value": "true"}]) |
      with(select(.name == "internal-rpc-authority-issuer" or
          .name == "internal-rpc-authority-verifier");
        .env += [
          {"name":"INTERNAL_RPC_AUTHORITY_SECRET_BACKEND","value":"direct-production-kubernetes-file"},
          {"name":"INTERNAL_RPC_AUTHORITY_DEPLOYMENT_PROFILE","valueFrom":{"fieldRef":{"fieldPath":"metadata.labels['mattercodex.dev/profile']"}}}
        ]
      ) |
      .volumeMounts = ((.volumeMounts // []) |
        map(select((.name | test("otel|sentry|observability")) | not)))
    ) |
    .spec.template.spec.volumes = ((.spec.template.spec.volumes // []) |
      map(select((.name | test("otel|sentry|observability")) | not))) |
    (.spec.template.spec.volumes[]? | select(.csi != null)) |= {
      "name": .name,
      "secret": {"secretName": .csi.volumeAttributes.secretProviderClass, "defaultMode": 288}
    } |
    .spec.template.spec.volumes[]? |= (
      with(select(.secret != null and
          (.secret.secretName | test("^internal-rpc-authority-.*-(issuer|resolver)-key$")));
        .secret.items = [{"key":"private.jwk","path":"private.jwk"}]
      ) |
      with(select(.secret != null and
          .secret.secretName != "internal-rpc-authority-publisher-manifest-trust" and
          (.secret.secretName | test("^internal-rpc-authority-.*-manifest-trust$")));
        .secret.items = [{"key":"bundle.jws","path":"bundle.jws"}]
      ) |
      with(select(.secret != null and
          (.secret.secretName | test("^internal-rpc-authority-.*-(proof-trust|resolver-trust)$")));
        .secret.items = [{"key":"jwks.json","path":"jwks.json"}]
      )
    )
  ) |
  with(select(.kind == "Deployment");
    (.spec.template.spec.containers[]?.env[]? | select(.name == "CONTROL_PLANE_NATS_REPLICAS").value) = "1"
  ) |
  with(select(.kind == "CronJob");
    .spec.jobTemplate.spec.template.metadata.labels."mattercodex.dev/profile" = "direct-production-single-node-prototype" |
    .spec.jobTemplate.spec.template.metadata.labels."mattercodex.dev/release-managed" = "true" |
    .spec.jobTemplate.spec.template.spec.automountServiceAccountToken = false |
    .spec.jobTemplate.spec.template.spec.containers[] |= (
      .env = ((.env // []) |
        map(select((.name | test("^(OTEL_|SENTRY_)")) | not)) +
        [{"name": "OTEL_SDK_DISABLED", "value": "true"}]) |
      .volumeMounts = ((.volumeMounts // []) |
        map(select((.name | test("otel|sentry|observability")) | not)))
    ) |
    .spec.jobTemplate.spec.template.spec.initContainers[]? |= (
      .env = ((.env // []) |
        map(select((.name | test("^(OTEL_|SENTRY_)")) | not)) +
        [{"name": "OTEL_SDK_DISABLED", "value": "true"}]) |
      .volumeMounts = ((.volumeMounts // []) |
        map(select((.name | test("otel|sentry|observability")) | not)))
    ) |
    .spec.jobTemplate.spec.template.spec.volumes = ((.spec.jobTemplate.spec.template.spec.volumes // []) |
      map(select((.name | test("otel|sentry|observability")) | not))) |
    (.spec.jobTemplate.spec.template.spec.volumes[]? | select(.csi != null)) |= {
      "name": .name,
      "secret": {"secretName": .csi.volumeAttributes.secretProviderClass, "defaultMode": 288}
    } |
    .spec.jobTemplate.spec.template.spec.volumes[]? |= (
      with(select(.secret != null and
          (.secret.secretName | test("^internal-rpc-authority-.*-(issuer|resolver)-key$")));
        .secret.items = [{"key":"private.jwk","path":"private.jwk"}]
      ) |
      with(select(.secret != null and
          .secret.secretName != "internal-rpc-authority-publisher-manifest-trust" and
          (.secret.secretName | test("^internal-rpc-authority-.*-manifest-trust$")));
        .secret.items = [{"key":"bundle.jws","path":"bundle.jws"}]
      ) |
      with(select(.secret != null and
          (.secret.secretName | test("^internal-rpc-authority-.*-(proof-trust|resolver-trust)$")));
        .secret.items = [{"key":"jwks.json","path":"jwks.json"}]
      )
    )
  ) |
  with(select(.kind == "ConfigMap" and .data != null);
    .data = (.data | with_entries(select((.key | test("^(OTEL_|SENTRY_)")) | not)))
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^https://object-store\\.storage\\.svc\\.cluster\\.local$";
    "https://mattercodex-object-store.mattercodex-system.svc.cluster.local"
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^object-store\\.storage\\.svc\\.cluster\\.local$";
    "mattercodex-object-store.mattercodex-system.svc.cluster.local"
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^registry-pull\\.invalid/mattercodex/roles$";
    "localhost:5001/mattercodex/roles"
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^https://mattermost\\.mattermost\\.svc\\.cluster\\.local$";
    "https://mattercodex-legacy-mattermost-bridge.mattercodex-system.svc.cluster.local:8443"
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^mattermost\\.mattermost\\.svc\\.cluster\\.local$";
    "mattercodex-legacy-mattermost-bridge.mattercodex-system.svc.cluster.local"
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^https://matter-codex-bot-service\\.mattercodex-system\\.svc\\.cluster\\.local:8443$";
    "https://mattercodex-legacy-bot-service-bridge.mattercodex-system.svc.cluster.local:8443"
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^matter-codex-bot-service\\.mattercodex-system\\.svc\\.cluster\\.local$";
    "mattercodex-legacy-bot-service-bridge.mattercodex-system.svc.cluster.local"
  ) |
  with(select(.kind == "NetworkPolicy");
    .spec.egress[]? |= (
      with(select(.to[0].podSelector.matchLabels."app.kubernetes.io/name" == "matter-codex-bot-service");
        .to = [{"podSelector":{"matchLabels":{"app.kubernetes.io/name":"mattercodex-legacy-bot-service-bridge"}}}] |
        .ports = [{"protocol":"TCP","port":8443}]
      ) |
      with(select(.to[0].podSelector.matchLabels."app.kubernetes.io/name" == "mattermost");
        .to = [{"podSelector":{"matchLabels":{"app.kubernetes.io/name":"mattercodex-legacy-mattermost-bridge"}}}] |
        .ports = [{"protocol":"TCP","port":8443}]
      )
    )
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^mattercodex-image-registry-push\\.mattercodex-system\\.svc\\.cluster\\.local:5001/staging/role-images$";
    "matter-codex-registry.matter-kodex-prod.svc.cluster.local:5000/mattercodex/role-images-staging"
  )
' "$temporary_directory/filtered.yaml" >"$temporary_directory/normalized.yaml"

# File readers получают только Secret своей exact target. Список закрыт и
# повторяет утверждённый target registry publisher.
add_prototype_delivery_mount() {
  local workload_id=$1 container_name=$2 secret_name=$3 directory=$4 volume_name=$5
  WORKLOAD_ID="$workload_id" CONTAINER_NAME="$container_name" SECRET_NAME="$secret_name" \
    DIRECTORY="$directory" VOLUME_NAME="$volume_name" yq -i '
      with(select(.kind == "Deployment" or .kind == "DaemonSet" or .kind == "Job");
        with(select(([
          .spec.template.spec.containers[]? |
          select(.name == strenv(CONTAINER_NAME) and ([
            .env[]? |
            select(.name == "INTERNAL_RPC_AUTHORITY_WORKLOAD_ID" and .value == strenv(WORKLOAD_ID))
          ] | length) > 0)
        ] | length) > 0);
          .spec.template.spec.containers[] |=
            with(select(.name == strenv(CONTAINER_NAME));
              .volumeMounts = ((.volumeMounts // []) + [{
                "name":strenv(VOLUME_NAME),
                "mountPath":"/var/run/secrets/mattercodex/internal-rpc-authority/prototype-delivery/" + strenv(DIRECTORY),
                "readOnly":true
              }])
            ) |
          .spec.template.spec.volumes = ((.spec.template.spec.volumes // []) + [{
            "name":strenv(VOLUME_NAME),
            "secret":{"secretName":strenv(SECRET_NAME),"defaultMode":288}
          }])
        )
      )
    ' "$temporary_directory/normalized.yaml"
}

add_prototype_delivery_mount role-image-builder internal-rpc-authority-issuer internal-rpc-authority-role-image-builder-issuer-delivery primary prototype-delivery-issuer
add_prototype_delivery_mount image-admission internal-rpc-authority-issuer internal-rpc-authority-image-admission-issuer-delivery primary prototype-delivery-issuer
add_prototype_delivery_mount image-promotion internal-rpc-authority-issuer internal-rpc-authority-image-promotion-issuer-delivery primary prototype-delivery-issuer
add_prototype_delivery_mount automation-scheduler internal-rpc-authority-issuer internal-rpc-authority-automation-scheduler-issuer-delivery primary prototype-delivery-issuer
add_prototype_delivery_mount control-api-gateway internal-rpc-authority-issuer internal-rpc-authority-control-api-gateway-issuer-delivery primary prototype-delivery-issuer
add_prototype_delivery_mount integration-gateway internal-rpc-authority-issuer internal-rpc-authority-integration-gateway-issuer-delivery primary prototype-delivery-issuer
add_prototype_delivery_mount interaction-gateway internal-rpc-authority-issuer internal-rpc-authority-interaction-gateway-issuer-delivery primary prototype-delivery-issuer
add_prototype_delivery_mount interaction-gateway internal-rpc-authority-verifier internal-rpc-authority-interaction-gateway-verifier-delivery primary prototype-delivery-verifier
add_prototype_delivery_mount control-plane internal-rpc-authority-verifier internal-rpc-authority-control-plane-verifier-delivery primary prototype-delivery-verifier
add_prototype_delivery_mount control-plane internal-rpc-authority-verifier internal-rpc-authority-control-plane-resolver-delivery resolver prototype-delivery-resolver

conflicting_resources=$(yq -o=json '.' "$temporary_directory/normalized.yaml" | jq -rs '
  map(select(.kind != null)) |
  group_by([.apiVersion, .kind, (.metadata.namespace // ""), .metadata.name]) |
  map(select((map(tojson) | unique | length) > 1) | .[0] |
    [.apiVersion, .kind, (.metadata.namespace // ""), .metadata.name] | @tsv) |
  .[]
')
[[ -z "$conflicting_resources" ]] || fail "canonical overlays contain conflicting resource identities"
yq -o=json '.' "$temporary_directory/normalized.yaml" | jq -sc '
  map(select(.kind != null)) |
  unique_by([.apiVersion, .kind, (.metadata.namespace // ""), .metadata.name]) |
  .[]
' | yq -p=json -P >"$output"

if [[ "$scope" == release ]]; then
  while IFS=$'\t' read -r component pull_ref; do
    COMPONENT="$component" PULL_REF="$pull_ref" yq -i '
      (.. | select(tag == "!!str")) |= sub(
        "[A-Za-z0-9._:/-]+/" + strenv(COMPONENT) + "@sha256:[a-f0-9]{64}";
        strenv(PULL_REF)
      )
    ' "$output"
  done < <(jq -r '.images[] | [.component,.pull_ref] | @tsv' "$lock_file")

  agent_runner_ref=$(jq -er '.images[] | select(.component == "agent-runner") | .pull_ref' "$lock_file")
  agent_runner_digest=${agent_runner_ref##*@}
  lock_digest=$(sha256sum "$lock_file" | awk '{print $1}')
  AGENT_RUNNER_REF="$agent_runner_ref" AGENT_RUNNER_DIGEST="$agent_runner_digest" LOCK_DIGEST="$lock_digest" yq -i '
    with(select(.kind == "Deployment" and .metadata.name == "control-plane");
      .spec.template.metadata.annotations."mattercodex.dev/agent-runtime-image-digest" = strenv(AGENT_RUNNER_DIGEST)
    ) |
    (.. | select(tag == "!!str" and . == "sha256:0000000000000000000000000000000000000000000000000000000000000000")) = strenv(AGENT_RUNNER_DIGEST) |
    (.. | select(tag == "!!str" and . == "0000000000000000000000000000000000000000000000000000000000000000")) = strenv(LOCK_DIGEST)
  ' "$output"

  cat <<EOF >>"$output"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: mattercodex-image-admission-policy
  namespace: mattercodex-system
  labels:
    mattercodex.dev/profile: direct-production-single-node-prototype
    mattercodex.dev/release-managed: "true"
data:
  policyRevision: "1"
  policySHA256: "$lock_digest"
  promotedPullRepository: "localhost:5001/mattercodex/roles"
  roleImageInputRepository: "matter-codex-registry.matter-kodex-prod.svc.cluster.local:5000/mattercodex/role-image-inputs"
  trustedRoleBaseRepository: "localhost:5001/mattercodex/agent-runner"
  trustedRoleBaseDigest: "$agent_runner_digest"
  roleRuntimeContractRevision: "1"
  roleRuntimeContractSHA256: "$lock_digest"
EOF

  grep -Eq '^kind: Ingress$' "$output" && fail "application render contains Ingress"
  grep -Eq 'sha256:0{64}' "$output" && fail "application render contains a zero digest"
  if yq -r '.. | select(has("image")) | .image' "$output" | grep -E 'mattercodex/(control-plane|internal-rpc-authority|runtime-controller|interaction-gateway|integration-gateway|control-api-gateway|staff-control-center|egress-gateway|automation-scheduler|role-image-builder|agent-runner)(:|@)' |
    grep -Fvxf <(jq -r '.images[].pull_ref' "$lock_file") >/dev/null; then
    fail "application render contains an internal image outside the release lock"
  fi
fi

duplicate_resources=$(yq -o=json '.' "$output" | jq -rs '
  map(select(.kind != null)) |
  group_by([.apiVersion, .kind, (.metadata.namespace // ""), .metadata.name]) |
  map(select(length != 1) | .[0] | [.apiVersion, .kind, (.metadata.namespace // ""), .metadata.name] | @tsv) |
  .[]
')
[[ -z "$duplicate_resources" ]] || fail "application render contains duplicate resource identities"

printf 'Application %s render created: %s\n' "$scope" "$output"
