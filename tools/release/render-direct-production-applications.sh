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
    with(select(.spec.template.spec.automountServiceAccountToken == null);
      .spec.template.spec.automountServiceAccountToken = false
    ) |
    .spec.template.spec.containers[] |= (
      .env = ((.env // []) |
        map(select((.name | test("^(OTEL_|SENTRY_)")) | not)) +
        [{"name": "OTEL_SDK_DISABLED", "value": "true"}]) |
      .volumeMounts = ((.volumeMounts // []) |
        map(select((.name | test("otel|sentry|observability")) | not)))
    ) |
    .spec.template.spec.initContainers[]? |= (
      .env = ((.env // []) |
        map(select((.name | test("^(OTEL_|SENTRY_)")) | not)) +
        [{"name": "OTEL_SDK_DISABLED", "value": "true"}]) |
      .volumeMounts = ((.volumeMounts // []) |
        map(select((.name | test("otel|sentry|observability")) | not)))
    ) |
    .spec.template.spec.volumes = ((.spec.template.spec.volumes // []) |
      map(select((.name | test("otel|sentry|observability")) | not))) |
    (.spec.template.spec.volumes[]? | select(.csi != null)) |= {
      "name": .name,
      "secret": {"secretName": .csi.volumeAttributes.secretProviderClass, "defaultMode": 288}
    }
  ) |
  with(select(.kind == "Deployment");
    .spec.replicas = 1 |
    (.spec.template.spec.containers[]?.env[]? | select(.name == "CONTROL_PLANE_NATS_REPLICAS").value) = "1"
  ) |
  with(select(.kind == "CronJob");
    .spec.jobTemplate.spec.template.metadata.labels."mattercodex.dev/profile" = "direct-production-single-node-prototype" |
    .spec.jobTemplate.spec.template.metadata.labels."mattercodex.dev/release-managed" = "true" |
    with(select(.spec.jobTemplate.spec.template.spec.automountServiceAccountToken == null);
      .spec.jobTemplate.spec.template.spec.automountServiceAccountToken = false
    ) |
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
    }
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
    "^mattercodex-image-registry-push\\.mattercodex-system\\.svc\\.cluster\\.local:5001/staging/role-images$";
    "matter-codex-registry.matter-kodex-prod.svc.cluster.local:5000/mattercodex/role-images-staging"
  )
' "$temporary_directory/filtered.yaml" >"$temporary_directory/normalized.yaml"

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
  if yq -r '.. | select(has("image")) | .image' "$output" | grep -E 'mattercodex/(control-plane|internal-rpc-authority|runtime-controller|interaction-gateway|integration-gateway|control-api-gateway|egress-gateway|automation-scheduler|role-image-builder|agent-runner)(:|@)' |
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
