#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local render failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --source-root <path> --cache-root <path> --output <path>" \
    '  --public-host <dns> --oidc-host <dns> --kubernetes-service-cidr <cidr>' \
    '  [--ingress-class <name>] [--cluster-issuer <name>]' \
    '  --kubernetes-endpoint-cidr <cidr> --kubernetes-endpoint-port <port>' \
    '  --runner-image <repository@sha256:digest>' \
    '  --session-archive-image <repository@sha256:digest>' \
    '  --backup-controller-image <repository@sha256:digest>' \
    '  --promoted-pull-host <dns>' \
    '  --role-image-builder-image <repository@sha256:digest>' \
    '  --image-admission-image <repository@sha256:digest>' \
    '  --image-admission-tools-image <repository@sha256:digest>' \
    '  --authority-image <repository@sha256:digest>' \
    '  --authority-source-revision <positive integer>' \
    '  --role-image-input-manifest-digest <sha256:digest>' \
    '  --role-image-input-payload-sha256 <sha256>' \
    '  --role-image-input-source-sha256 <sha256>' >&2
}

source_root=""
cache_root=""
output=""
public_host=""
oidc_host=""
ingress_class=traefik
cluster_issuer=kodex-local
kubernetes_service_cidr=""
kubernetes_endpoint_cidr=""
kubernetes_endpoint_port=""
runner_image=""
session_archive_image=""
backup_controller_image=""
promoted_pull_host=""
role_image_builder_image=""
image_admission_image=""
image_admission_tools_image=""
authority_image=""
authority_source_revision=""
role_image_input_manifest_digest=""
role_image_input_payload_sha256=""
role_image_input_source_sha256=""
while (($# > 0)); do
  case "$1" in
    --source-root) source_root=${2:-}; shift 2 ;;
    --cache-root) cache_root=${2:-}; shift 2 ;;
    --output) output=${2:-}; shift 2 ;;
    --public-host) public_host=${2:-}; shift 2 ;;
    --oidc-host) oidc_host=${2:-}; shift 2 ;;
    --ingress-class) ingress_class=${2:-}; shift 2 ;;
    --cluster-issuer) cluster_issuer=${2:-}; shift 2 ;;
    --kubernetes-service-cidr) kubernetes_service_cidr=${2:-}; shift 2 ;;
    --kubernetes-endpoint-cidr) kubernetes_endpoint_cidr=${2:-}; shift 2 ;;
    --kubernetes-endpoint-port) kubernetes_endpoint_port=${2:-}; shift 2 ;;
    --runner-image) runner_image=${2:-}; shift 2 ;;
    --session-archive-image) session_archive_image=${2:-}; shift 2 ;;
    --backup-controller-image) backup_controller_image=${2:-}; shift 2 ;;
    --promoted-pull-host) promoted_pull_host=${2:-}; shift 2 ;;
    --role-image-builder-image) role_image_builder_image=${2:-}; shift 2 ;;
    --image-admission-image) image_admission_image=${2:-}; shift 2 ;;
    --image-admission-tools-image) image_admission_tools_image=${2:-}; shift 2 ;;
    --authority-image) authority_image=${2:-}; shift 2 ;;
    --authority-source-revision) authority_source_revision=${2:-}; shift 2 ;;
    --role-image-input-manifest-digest) role_image_input_manifest_digest=${2:-}; shift 2 ;;
    --role-image-input-payload-sha256) role_image_input_payload_sha256=${2:-}; shift 2 ;;
    --role-image-input-source-sha256) role_image_input_source_sha256=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$source_root" == /* && -d "$source_root/.git" || -f "$source_root/.git" ]] ||
  fail 'source root must be an exact Git worktree path'
[[ "$cache_root" == /* && "$cache_root" != / ]] || fail 'cache root is invalid'
[[ "$output" == /* ]] || fail 'output must be an absolute path'
for host in "$public_host" "$oidc_host"; do
  [[ "$host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$host" == *.* ]] ||
    fail 'local host is invalid'
done
[[ "$public_host" != "$oidc_host" ]] || fail 'public and OIDC hosts must differ'
[[ "$ingress_class" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
  fail 'development ingress class is invalid'
[[ "$cluster_issuer" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
  fail 'development cluster issuer is invalid'
[[ "$kubernetes_service_cidr" =~ /32$ && "$kubernetes_endpoint_cidr" =~ /32$ ]] ||
  fail 'Kubernetes API CIDRs are invalid'
[[ "$kubernetes_endpoint_port" =~ ^[1-9][0-9]{0,4}$ ]] || fail 'Kubernetes API port is invalid'
[[ "$runner_image" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] ||
  fail 'local runner image must use an exact manifest digest'
[[ "$session_archive_image" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] ||
  fail 'local session archive image must use an exact manifest digest'
[[ "$backup_controller_image" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] ||
  fail 'local backup-controller image must use an exact manifest digest'
[[ "$promoted_pull_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ &&
  "$promoted_pull_host" == *.* ]] || fail 'local promoted pull host is invalid'
for exact_image in "$role_image_builder_image" "$image_admission_image" \
  "$image_admission_tools_image" "$authority_image"; do
  [[ "$exact_image" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] ||
    fail 'local supply-chain image must use an exact manifest digest'
done
[[ "$authority_source_revision" =~ ^[1-9][0-9]*$ &&
  "$authority_source_revision" -le 9007199254740991 ]] ||
  fail 'local authority source revision is invalid'
[[ "$role_image_input_manifest_digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
  fail 'local role image input manifest digest is invalid'
for plain_digest in "$role_image_input_payload_sha256" "$role_image_input_source_sha256"; do
  [[ "$plain_digest" =~ ^[a-f0-9]{64}$ ]] || fail 'local role image input digest is invalid'
done
for command_name in git go jq kubectl sha256sum yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
[[ "$repository_root" == "$source_root" ]] || fail 'source root must match the current worktree'
lock_file="$repository_root/tools/dev/components.lock.json"
runtime_contract_file="$repository_root/contracts/runtime-controller/v6/agent-runner-input.schema.json"
runtime_contract_digest=$(jq -cS . "$runtime_contract_file" | sha256sum | awk '{print $1}')
[[ "$runtime_contract_digest" =~ ^[a-f0-9]{64}$ &&
  "$runtime_contract_digest" != 0000000000000000000000000000000000000000000000000000000000000000 ]] ||
  fail 'local role runtime contract digest is invalid'
seaweedfs_image=$(jq -er '
  .images[] | select(.name == "seaweedfs" and .version == "4.41") | .reference
' "$lock_file") || fail 'SeaweedFS image lock is absent'
[[ "$seaweedfs_image" =~ ^docker\.io/chrislusf/seaweedfs@sha256:[a-f0-9]{64}$ ]] ||
  fail 'SeaweedFS image lock is invalid'
aws_cli_image=$(jq -er '
  .images[] | select(.name == "aws-cli" and .version == "2.36.34") | .reference
' "$lock_file") || fail 'AWS CLI image lock is absent'
[[ "$aws_cli_image" =~ ^docker\.io/amazon/aws-cli@sha256:[a-f0-9]{64}$ ]] ||
  fail 'AWS CLI image lock is invalid'
air_module=$(jq -er '.tools.air.module' "$lock_file") || fail 'Air module lock is absent'
air_version=$(jq -er '.tools.air.version' "$lock_file") || fail 'Air version lock is absent'
[[ "$air_module" == "github.com/air-verse/air" && "$air_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] ||
  fail 'Air tool lock is invalid'
# These directories are mounted directly as hostPath volumes. k3s may remap
# container root to an unprivileged host UID, so the local-only shared caches
# must be writable independently of the private state directory permissions.
go_module_cache="$cache_root/go-mod-v2"
go_sumdb_cache="$cache_root/go-sumdb"
go_prime_cache="$cache_root/go-build/host-prime"
install -d -m 0777 \
  "$go_module_cache/cache/download/sumdb/sum.golang.org" \
  "$go_sumdb_cache/sum.golang.org" \
  "$go_prime_cache" \
  "$cache_root/go-build" \
  "$cache_root/go-tools" \
  "$cache_root/node-modules"
chmod 0777 \
  "$go_module_cache" \
  "$go_module_cache/cache" \
  "$go_module_cache/cache/download" \
  "$go_module_cache/cache/download/sumdb" \
  "$go_module_cache/cache/download/sumdb/sum.golang.org" \
  "$go_sumdb_cache" \
  "$go_sumdb_cache/sum.golang.org" \
  "$go_prime_cache" \
  "$cache_root/go-build" \
  "$cache_root/go-tools" \
  "$cache_root/node-modules"
install -d -m 0777 "$source_root/services/staff/control-center/node_modules"
chmod 0777 "$source_root/services/staff/control-center/node_modules"

# Local NetworkPolicy intentionally blocks arbitrary Internet access from pods.
# Prime every module used by hot-reload workloads on the host so migrations and
# service restarts are deterministic and do not weaken that boundary.
go_modules=(
  services/internal/control-plane
  services/internal/secret-broker
  services/internal/internal-rpc-authority
  services/internal/runtime-controller
  services/external/control-api-gateway
  services/external/egress-gateway
  services/external/integration-gateway
  services/jobs/automation-scheduler
  services/jobs/artifact-retention
  services/jobs/session-archive
)
for module in "${go_modules[@]}"; do
  [[ -f "$source_root/$module/go.mod" ]] || fail "Go module is absent: $module"
  GOMODCACHE="$go_module_cache" GOCACHE="$go_prime_cache" GOWORK=off GOTOOLCHAIN=local \
    go -C "$source_root/$module" mod download || fail "Go module cache prime failed: $module"
done
air_binary="$cache_root/go-tools/air"
air_contract_file="$cache_root/go-tools/air.contract"
air_contract="$air_module@$air_version|CGO_ENABLED=0|$(go env GOOS)/$(go env GOARCH)"
current_air_contract=$(cat "$air_contract_file" 2>/dev/null || true)
if [[ ! -x "$air_binary" || "$current_air_contract" != "$air_contract" ]]; then
  rm -f -- "$air_binary" "$air_contract_file"
  CGO_ENABLED=0 GOBIN="$cache_root/go-tools" GOMODCACHE="$go_module_cache" \
    GOCACHE="$go_prime_cache" GOWORK=off GOTOOLCHAIN=local \
    go install "$air_module@$air_version" || fail 'Air installation failed'
  [[ -x "$air_binary" ]] || fail 'Air installation did not produce an executable'
  printf '%s\n' "$air_contract" >"$air_contract_file"
fi
for owned_cache in "$go_module_cache" "$go_sumdb_cache" "$go_prime_cache"; do
  find "$owned_cache" -user "$(id -u)" -exec chmod a+rwX {} +
done

temporary_directory=$(mktemp -d)
render="$temporary_directory/local.yaml"
{
  kubectl kustomize "$repository_root/deploy/k8s/profiles/web-only"
  printf '\n---\n'
  kubectl kustomize "$repository_root/deploy/k8s/base/local-object-storage"
  printf '\n---\n'
  kubectl kustomize "$repository_root/deploy/k8s/overlays/local/session-archive"
  printf '\n---\n'
  kubectl kustomize "$repository_root/deploy/k8s/overlays/local/integration-synthetic"
} >"$render"

source_revision=$(git -C "$source_root" rev-parse HEAD)
source_digest=$(printf '%s' "$source_revision" | sha256sum | awk '{print $1}')
oidc_issuer="https://$oidc_host/realms/kodex"
oidc_jwks_url="$oidc_issuer/protocol/openid-connect/certs"
public_origin="https://$public_host"
oidc_origin="https://$oidc_host"

PUBLIC_HOST="$public_host" PUBLIC_ORIGIN="$public_origin" \
OIDC_ISSUER="$oidc_issuer" OIDC_JWKS_URL="$oidc_jwks_url" \
OIDC_HOST="$oidc_host" OIDC_ORIGIN="$oidc_origin" \
INGRESS_CLASS="$ingress_class" CLUSTER_ISSUER="$cluster_issuer" \
KUBERNETES_SERVICE_CIDR="$kubernetes_service_cidr" \
SOURCE_REVISION="$source_revision" SOURCE_DIGEST="$source_digest" \
SEAWEEDFS_IMAGE="$seaweedfs_image" AWS_CLI_IMAGE="$aws_cli_image" \
PROMOTED_PULL_HOST="$promoted_pull_host" yq -i '
  (.. | select(tag == "!!str")) |= (
    sub("__KODEX_PUBLIC_HOST__"; strenv(PUBLIC_HOST)) |
    sub("__KODEX_PUBLIC_ORIGIN__"; strenv(PUBLIC_ORIGIN)) |
    sub("__KODEX_OIDC_ISSUER__"; strenv(OIDC_ISSUER)) |
    sub("__KODEX_OIDC_JWKS_URL__"; strenv(OIDC_JWKS_URL)) |
    sub("__KODEX_OIDC_CONNECT_ADDRESS__"; "sso.identity.svc.cluster.local:443") |
    sub("__KODEX_OIDC_TLS_SERVER_NAME__"; strenv(OIDC_HOST)) |
    sub("__KODEX_OIDC_ORIGIN__"; strenv(OIDC_ORIGIN)) |
    sub("__KODEX_INGRESS_CLASS__"; strenv(INGRESS_CLASS)) |
    sub("__KODEX_CLUSTER_ISSUER__"; strenv(CLUSTER_ISSUER)) |
    sub("__KODEX_INGRESS_NAMESPACE__"; "kube-system") |
    sub("__KODEX_INGRESS_POD_NAME__"; "traefik") |
    sub("__KODEX_OIDC_NAMESPACE__"; "identity") |
    sub("__KODEX_OIDC_POD_NAME__"; "sso") |
    sub("__KODEX_OIDC_POD_COMPONENT__"; "identity-provider") |
    sub("__KODEX_KUBERNETES_API_SERVICE_CIDR__"; strenv(KUBERNETES_SERVICE_CIDR)) |
    sub("__KODEX_SEAWEEDFS_IMAGE__"; strenv(SEAWEEDFS_IMAGE)) |
    sub("__KODEX_AWS_CLI_IMAGE__"; strenv(AWS_CLI_IMAGE)) |
    sub("registry-pull\\.invalid"; strenv(PROMOTED_PULL_HOST))
  ) |
  with(select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "Job");
    .spec.template.metadata.labels."kodex.dev/environment" = "staging" |
    .spec.template.metadata.labels."kodex.dev/local-profile" = "hot-reload" |
    (.spec.template.spec.containers[] | select(.startupProbe != null) |
      .startupProbe.failureThreshold) = 180 |
    (.spec.template.spec.containers[] | select(.startupProbe != null) |
      .startupProbe.periodSeconds) = 2
  ) |
  with(select(.metadata.labels != null);
    .metadata.labels."kodex.dev/environment" = "staging" |
    .metadata.labels."kodex.dev/local-profile" = "hot-reload"
  )
' "$render"

# Локальный профиль запускает полный RoleImage supply-chain. Наблюдаемость и
# retention CronJob остаются за пределами hot-reload контура, но registry,
# BuildKit, admission/promotion и builder должны быть реально достижимы.
yq -i '
  select(
    (.kind != "NetworkPolicy" or
      (.metadata.name | test("^(artifact-retention|backup-controller|control-plane|seaweedfs|integration-gateway|integration-synthetic|session-archive|kodex-image|kodex-buildkit|role-image-builder|image-admission)"))) and
    .kind != "PodDisruptionBudget" and
    .kind != "ServiceMonitor" and
    .kind != "PodMonitor" and
    .kind != "PrometheusRule" and
    .kind != "CronJob" and
    (.kind != "PersistentVolumeClaim" or
      .metadata.name == "kodex-image-registry-staging" or
      .metadata.name == "kodex-image-registry-promoted" or
      .metadata.name == "kodex-image-registry-evidence") and
    (.kind != "IngressRouteTCP" or .metadata.name == "kodex-image-registry-pull") and
    (.kind != "Deployment" or .metadata.name == "control-plane" or
      .metadata.name == "secret-broker" or
      .metadata.name == "control-api-gateway" or .metadata.name == "egress-gateway" or
      .metadata.name == "runtime-controller" or .metadata.name == "integration-gateway" or
      .metadata.name == "integration-synthetic" or
      .metadata.name == "backup-controller" or
      .metadata.name == "automation-scheduler" or .metadata.name == "artifact-retention" or
      .metadata.name == "staff-control-center" or
      .metadata.name == "session-archive" or
      .metadata.name == "internal-rpc-authority-publisher" or
      .metadata.name == "internal-rpc-authority-readback-attestor" or
      .metadata.name == "internal-rpc-authority-restore-controller" or
      .metadata.name == "kodex-image-registry-pull" or
      .metadata.name == "kodex-image-registry-push" or
      .metadata.name == "kodex-image-registry-promotion" or
      .metadata.name == "kodex-image-registry-staging-read" or
      .metadata.name == "kodex-image-registry-evidence" or
      .metadata.name == "kodex-buildkit" or
      .metadata.name == "image-admission-controller" or
      .metadata.name == "role-image-builder") and
    (.kind != "Job" or .metadata.name == "control-plane-migrate" or
      .metadata.name == "control-plane-broker-bootstrap" or
      .metadata.name == "internal-rpc-authority-migrate" or
      .metadata.name == "kodex-postgresql-runtime-credentials" or
      .metadata.name == "seaweedfs-bucket-bootstrap")
  )
' "$render"

runner_digest=${runner_image#*@}
runtime_runner_image="$promoted_pull_host/kodex/agent-runner@$runner_digest"
admission_tools_digest=${image_admission_tools_image#*@}
admission_tools_sha256=${image_admission_tools_image#*@sha256:}
frontend_sha256=$("$source_root/tools/dev/resolve-local-dockerfile-frontend.sh" \
  --source-root "$source_root" --format digest)
[[ "$frontend_sha256" =~ ^[a-f0-9]{64}$ ]] || fail 'Dockerfile frontend digest is invalid'
ROLE_IMAGE_BUILDER_IMAGE="$role_image_builder_image" \
IMAGE_ADMISSION_IMAGE="$image_admission_image" \
IMAGE_ADMISSION_TOOLS_IMAGE="$image_admission_tools_image" \
AUTHORITY_IMAGE="$authority_image" \
ADMISSION_TOOLS_DIGEST="$admission_tools_digest" \
ADMISSION_TOOLS_SHA256="$admission_tools_sha256" \
PROMOTED_PULL_HOST="$promoted_pull_host" \
RUNNER_DIGEST="$runner_digest" \
SOURCE_REVISION="$source_revision" SOURCE_DIGEST="$source_digest" \
RUNTIME_CONTRACT_DIGEST="$runtime_contract_digest" \
FRONTEND_SHA256="$frontend_sha256" \
ROLE_INPUT_MANIFEST_DIGEST="$role_image_input_manifest_digest" \
ROLE_INPUT_PAYLOAD_SHA256="$role_image_input_payload_sha256" \
ROLE_INPUT_SOURCE_SHA256="$role_image_input_source_sha256" yq -i '
  with(select(.kind == "PersistentVolumeClaim" and
      (.metadata.name == "kodex-image-registry-staging" or
       .metadata.name == "kodex-image-registry-promoted" or
       .metadata.name == "kodex-image-registry-evidence"));
    .spec.resources.requests.storage = "10Gi"
  ) |
  with(select(.kind == "Deployment" and
      (.metadata.name | test("^(kodex-image-registry-|kodex-buildkit$|role-image-builder$)")));
    .spec.replicas = 1
  ) |
  with(select(.kind == "Deployment");
    with((.spec.template.spec.initContainers[]?, .spec.template.spec.containers[]?) |
        select(.image | test("/image-admission-tools@sha256:"));
      .image = strenv(IMAGE_ADMISSION_TOOLS_IMAGE) |
      .imagePullPolicy = "IfNotPresent"
    ) |
    with((.spec.template.spec.initContainers[]?, .spec.template.spec.containers[]?) |
        select(.image | test("/image-admission@sha256:"));
      .image = strenv(IMAGE_ADMISSION_IMAGE) |
      .imagePullPolicy = "IfNotPresent"
    ) |
    with((.spec.template.spec.initContainers[]?, .spec.template.spec.containers[]?) |
        select(.image | test("/internal-rpc-authority@sha256:"));
      .image = strenv(AUTHORITY_IMAGE) |
      .imagePullPolicy = "IfNotPresent"
    ) |
    with(.spec.template.spec.containers[]? | select(.name == "role-image-builder");
      .image = strenv(ROLE_IMAGE_BUILDER_IMAGE) |
      .imagePullPolicy = "IfNotPresent"
    )
  ) |
  with(select(.kind == "ConfigMap" and .metadata.name == "kodex-image-admission-policy");
    .immutable = true |
    .metadata.annotations."kodex.dev/admission-tools-sha256" = strenv(ADMISSION_TOOLS_DIGEST) |
    .data.orchestrationRevision = strenv(SOURCE_REVISION) |
    .data.toolsImage = strenv(IMAGE_ADMISSION_TOOLS_IMAGE) |
    .data.admissionImage = strenv(IMAGE_ADMISSION_IMAGE) |
    .data.authorityImage = strenv(AUTHORITY_IMAGE) |
    .data.promotedPullRepository = (strenv(PROMOTED_PULL_HOST) + "/kodex/roles") |
    .data.pullRegistryHost = strenv(PROMOTED_PULL_HOST) |
    .data.pullCredentialGeneration = "1" |
    .data.nodeReadbackImage = (strenv(PROMOTED_PULL_HOST) + "/kodex/agent-runner@" + strenv(RUNNER_DIGEST)) |
    .data.roleImageInputRepository = "kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/role-image-inputs" |
    .data.policyRevision = "1" |
    .data.policySHA256 = "0000000000000000000000000000000000000000000000000000000000000000" |
    .data.trustedRoleBaseRepository = "kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/agent-runner" |
    .data.trustedRoleBaseDigest = strenv(RUNNER_DIGEST) |
    .data.frontendSHA256 = strenv(FRONTEND_SHA256) |
    .data.toolchainSHA256 = strenv(ADMISSION_TOOLS_SHA256) |
    .data.roleRuntimeContractRevision = "1" |
    .data.roleRuntimeContractSHA256 = strenv(RUNTIME_CONTRACT_DIGEST)
  ) |
  with(select(.kind == "ConfigMap" and .metadata.name == "role-image-builder-runtime");
    .data.ROLE_IMAGE_BUILDER_EXPECTED_TOOLCHAIN_SHA256 = strenv(ADMISSION_TOOLS_SHA256)
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "kodex-buildkit");
    .spec.template.metadata.annotations."kodex.dev/release-revision" = strenv(SOURCE_REVISION) |
    .spec.template.metadata.annotations."kodex.dev/trusted-role-base-repository" =
      "kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/agent-runner" |
    .spec.template.metadata.annotations."kodex.dev/trusted-role-base-digest" = strenv(RUNNER_DIGEST) |
    .spec.template.metadata.annotations."kodex.dev/frontend-sha256" = strenv(FRONTEND_SHA256) |
    with(.spec.template.spec.containers[] | select(.name == "buildkitd");
      .resources.requests.cpu = "8" |
      .resources.requests.memory = "8Gi" |
      .resources.limits.cpu = "24" |
      .resources.limits.memory = "64Gi"
    )
  ) |
  with(select(.kind == "Job" and .metadata.name == "seaweedfs-bucket-bootstrap");
    with(.spec.template.spec.containers[] | select(.name == "bootstrap");
      .resources.requests.cpu = "500m" |
      .resources.requests.memory = "256Mi" |
      .resources.limits.cpu = "2" |
      .resources.limits.memory = "512Mi"
    )
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "role-image-builder");
    .spec.template.metadata.annotations."kodex.dev/release-revision" = strenv(SOURCE_REVISION) |
    .spec.template.metadata.annotations."kodex.dev/trusted-role-base-repository" =
      "kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/agent-runner" |
    .spec.template.metadata.annotations."kodex.dev/trusted-role-base-digest" = strenv(RUNNER_DIGEST) |
    .spec.template.metadata.annotations."kodex.dev/frontend-sha256" = strenv(FRONTEND_SHA256)
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "kodex-image-registry-pull");
    .spec.template.metadata.annotations."kodex.dev/pull-credential-generation" = "1" |
    (.spec.template.spec.containers[] | select(.name == "certificate-guard") |
      .env[] | select(.name == "READBACK_IMAGE").value) =
        (strenv(PROMOTED_PULL_HOST) + "/kodex/control-plane@" + strenv(RUNNER_DIGEST))
  ) |
  with(select(.kind == "ConfigMap" and .metadata.name == "kodex-role-environments");
    .immutable = false |
    .data."catalog.json" |= (
      from_json |
      .context.sourceRevision = strenv(SOURCE_REVISION) |
      .context.sourceSha256 = strenv(ROLE_INPUT_SOURCE_SHA256) |
      .context.contextSha256 = strenv(ROLE_INPUT_PAYLOAD_SHA256) |
      .context.contextRef = ("oci://kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/role-image-inputs@" + strenv(ROLE_INPUT_MANIFEST_DIGEST)) |
      (.environments[] | select(.key == "standard") | .baseImageDigest) = strenv(RUNNER_DIGEST) |
      (.environments[] | select(.key == "documents") | .available) = false |
      (.environments[] | select(.key == "documents") | .unavailableMessageKey) =
        "role-environments.documents.local-unavailable" |
      to_json
    )
  )
' "$render"

admission_policy_payload=$(yq -o=json -I=0 '
  select(.kind == "ConfigMap" and .metadata.name == "kodex-image-admission-policy") |
  .data | del(.orchestrationRevision, .policySHA256)
' "$render" | jq -cS .)
admission_policy_digest=$(printf '%s\n' "$admission_policy_payload" | sha256sum | awk '{print $1}')
[[ "$admission_policy_digest" =~ ^[a-f0-9]{64}$ &&
  "$admission_policy_digest" != 0000000000000000000000000000000000000000000000000000000000000000 ]] ||
  fail 'local image admission policy digest is invalid'
POLICY_SHA256="$admission_policy_digest" yq -i '
  with(select(.kind == "ConfigMap" and .metadata.name == "kodex-image-admission-policy");
    .data.policySHA256 = strenv(POLICY_SHA256)
  )
' "$render"

admission_policy_json=$(yq -o=json -I=0 '
  select(.kind == "ConfigMap" and .metadata.name == "kodex-image-admission-policy") |
  .data
' "$render")
[[ -n "$admission_policy_json" && "$admission_policy_json" != null ]] ||
  fail 'local image admission policy projection is absent'
ADMISSION_POLICY_JSON="$admission_policy_json" yq -i '
  (strenv(ADMISSION_POLICY_JSON) | from_json) as $policy |
  with(select(.apiVersion == "supplychain.kodex.dev/v1alpha1" and
      .kind == "ImageAdmissionPolicyParameters" and
      .metadata.name == "kodex-image-admission-policy");
    .spec = $policy
  ) |
  # A local upgrade replaces an immutable ConfigMap under the same name.
  # Materialize its non-secret values into every Pod template so kubelet and
  # admission cannot observe different revisions through independent caches.
  with(select(.kind == "Deployment" or .kind == "StatefulSet" or
      .kind == "Job");
    .spec.template.metadata.annotations."kodex.dev/runtime-admission-policy-sha256" =
      $policy.policySHA256 |
    ((.spec.template.spec.initContainers[]?,
      .spec.template.spec.containers[]?) | .env[]? |
      select(.valueFrom.configMapKeyRef.name ==
        "kodex-image-admission-policy")) |= (
        (.valueFrom.configMapKeyRef.key) as $key |
        .value = $policy[$key] |
        del(.valueFrom)
      )
  )
' "$render"

BACKUP_CONTROLLER_IMAGE="$backup_controller_image" yq -i '
  with(select(.kind == "Deployment" and .metadata.name == "backup-controller");
    (.spec.template.spec.containers[] | select(.name == "backup-controller")) |= (
      .image = strenv(BACKUP_CONTROLLER_IMAGE) |
      .imagePullPolicy = "IfNotPresent"
    ) |
    (.spec.template.spec.containers[] | select(.name == "backup-controller") |
      .env[] | select(.name == "BACKUP_CONTROLLER_RELEASE_REVISION").value) =
        (strenv(BACKUP_CONTROLLER_IMAGE) | split("@")[1])
  )
' "$render"

API_SERVICE_CIDR="$kubernetes_service_cidr" API_ENDPOINT_CIDR="$kubernetes_endpoint_cidr" \
API_ENDPOINT_PORT="$kubernetes_endpoint_port" RUNTIME_RUNNER_IMAGE="$runtime_runner_image" \
OIDC_HOST="$oidc_host" yq -i '
  with(select(.kind == "ConfigMap" and .metadata.name == "kodex-platform-endpoints");
    .data.oidcConnectAddress = "sso.identity.svc.cluster.local:443" |
    .data.oidcTlsServerName = strenv(OIDC_HOST)
  ) |
  with(select(.kind == "NetworkPolicy" and .metadata.name == "control-plane-exact-runtime-paths");
    (.spec.egress[].ports[] | select(.port == "__KODEX_OIDC_TARGET_PORT__").port) = 8443
  ) |
  with(select(.kind == "NetworkPolicy");
    (.spec.egress[]? |
      select(.to[]?.ipBlock.cidr == strenv(API_SERVICE_CIDR))) |= (
        (.to[] | select(.ipBlock.cidr == strenv(API_SERVICE_CIDR)) |
          .ipBlock.cidr) = strenv(API_ENDPOINT_CIDR) |
        (.ports[] | select(.port == 443) | .port) =
          (strenv(API_ENDPOINT_PORT) | tonumber)
      )
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "control-api-gateway");
    (.spec.template.spec.containers[] | select(.name == "control-api-gateway") |
      .env[] | select(.name == "CONTROL_API_GATEWAY_RATE_LIMIT").value) = "1200" |
    (.spec.template.spec.containers[] | select(.name == "control-api-gateway") |
      .env[] | select(.name == "CONTROL_API_GATEWAY_PER_SUBJECT_WEBSOCKET_CONCURRENCY").value) = "16"
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "control-plane");
    (.spec.template.spec.containers[] | select(.name == "control-plane") |
      .env[] | select(.name == "CONTROL_PLANE_OBJECT_STORAGE_ALLOW_INSECURE_LOCAL").value) = "true"
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "artifact-retention");
    (.spec.template.spec.containers[] | select(.name == "artifact-retention") |
      .env[] | select(.name == "ARTIFACT_RETENTION_OBJECT_STORAGE_ALLOW_INSECURE_LOCAL").value) = "true"
  ) |
  with(select(.kind == "ConfigMap" and .metadata.name == "session-archive-runtime");
    .data.SESSION_ARCHIVE_OBJECT_STORAGE_ALLOW_INSECURE_LOCAL = "true"
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "secret-broker");
    .spec.template.spec.initContainers = (
      ((.spec.template.spec.initContainers // []) |
        map(select(.name != "codex-cli-install"))) +
      [{
        "name":"codex-cli-install",
        "image":strenv(RUNTIME_RUNNER_IMAGE),
        "imagePullPolicy":"IfNotPresent",
        "command":["/bin/sh","-ec"],
        "args":["binary=/usr/local/bin/codex; test -x \"$binary\"; \"$binary\" --version >/dev/null; temporary=/codex/.codex.tmp; rm -f \"$temporary\"; cp \"$binary\" \"$temporary\"; chmod 0555 \"$temporary\"; mv -f \"$temporary\" /codex/codex"],
        "resources":{"requests":{"cpu":"10m","memory":"64Mi"},"limits":{"cpu":"100m","memory":"256Mi"}},
        "securityContext":{"runAsNonRoot":true,"runAsUser":10001,"runAsGroup":29000,"allowPrivilegeEscalation":false,"readOnlyRootFilesystem":true,"capabilities":{"drop":["ALL"]}},
        "volumeMounts":[{"name":"codex-cli","mountPath":"/codex"}]
      }]
    ) |
    (.spec.template.spec.containers[] | select(.name == "secret-broker")) |= (
      .env = (((.env // []) |
        map(select(.name != "SECRET_BROKER_CODEX_BINARY"))) +
        [{"name":"SECRET_BROKER_CODEX_BINARY","value":"/codex/codex"}]) |
      .volumeMounts = (((.volumeMounts // []) |
        map(select(.name != "codex-cli"))) +
        [{"name":"codex-cli","mountPath":"/codex","readOnly":true}])
    ) |
    .spec.template.spec.volumes = (
      ((.spec.template.spec.volumes // []) |
        map(select(.name != "codex-cli"))) +
      [{"name":"codex-cli","emptyDir":{"sizeLimit":"384Mi"}}]
    )
  ) |
  with(select(.kind == "StatefulSet" and .metadata.name == "kodex-postgresql");
    (.spec.template.spec.containers[] | select(.name == "postgresql") | .args) += [
      "-c", "fsync=off",
      "-c", "synchronous_commit=off",
      "-c", "full_page_writes=off"
    ]
  ) |
  with(select(.kind == "Deployment");
    (.spec.template.spec.containers[] |
      select(.name == "internal-rpc-authority-issuer" or
        .name == "internal-rpc-authority-verifier") | .env) |=
      (((. // []) |
        map(select(.name != "INTERNAL_RPC_AUTHORITY_READINESS_TIMEOUT"))) +
        [{"name":"INTERNAL_RPC_AUTHORITY_READINESS_TIMEOUT","value":"5s"}])
  )
' "$render"

add_development_volumes() {
  local kind=$1 workload=$2
  KIND="$kind" WORKLOAD="$workload" SOURCE_ROOT="$source_root" CACHE_ROOT="$cache_root" yq -i '
    with(select(.kind == strenv(KIND) and .metadata.name == strenv(WORKLOAD));
      .spec.template.spec.volumes = (
        ((.spec.template.spec.volumes // []) |
          map(select(.name != "dev-source" and .name != "dev-go-mod" and .name != "dev-go-sumdb" and
            .name != "dev-go-build" and .name != "dev-go-tools"))) +
        [
          {"name":"dev-source","hostPath":{"path":strenv(SOURCE_ROOT),"type":"Directory"}},
          {"name":"dev-go-mod","hostPath":{"path":(strenv(CACHE_ROOT) + "/go-mod-v2"),"type":"Directory"}},
          {"name":"dev-go-sumdb","hostPath":{"path":(strenv(CACHE_ROOT) + "/go-sumdb"),"type":"Directory"}},
          {"name":"dev-go-build","hostPath":{"path":(strenv(CACHE_ROOT) + "/go-build"),"type":"Directory"}},
          {"name":"dev-go-tools","hostPath":{"path":(strenv(CACHE_ROOT) + "/go-tools"),"type":"Directory"}}
        ]
      ) |
      (.spec.template.spec.volumes[] | select(.name == "tmp").emptyDir) = {}
    )
  ' "$render"
}

patch_go_container() {
  local kind=$1 workload=$2 container=$3 module=$4 package=$5
  shift 5
  local command_args
  command_args=$(printf '%s\n' "$@" | jq -Rsc 'split("\n") | map(select(length > 0))')
  add_development_volumes "$kind" "$workload"
  KIND="$kind" WORKLOAD="$workload" CONTAINER="$container" MODULE="$module" PACKAGE="$package" \
  COMMAND_ARGS="$command_args" CACHE_KEY="$workload-$container" \
  GO_IMAGE='docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83' \
  yq -i '
    with(select(.kind == strenv(KIND) and .metadata.name == strenv(WORKLOAD));
      .spec.replicas = 1 |
      (.spec.template.spec.containers[] | select(.name == strenv(CONTAINER))) |= (
        .image = strenv(GO_IMAGE) |
        .imagePullPolicy = "IfNotPresent" |
        .command = ["/workspace/tools/dev/run-go-hot-reload.sh"] |
        .args = ([strenv(MODULE),strenv(PACKAGE),strenv(CONTAINER)] +
          (strenv(COMMAND_ARGS) | from_json)) |
        .workingDir = ("/workspace/" + strenv(MODULE)) |
        .resources = {"requests":{"cpu":"50m","memory":"128Mi"}} |
        .securityContext.readOnlyRootFilesystem = false |
        .volumeMounts = (((.volumeMounts // []) |
          map(select(.name != "dev-source" and .name != "dev-go-mod" and .name != "dev-go-sumdb" and
            .name != "dev-go-build" and .name != "dev-go-tools"))) +
          [
            {"name":"dev-source","mountPath":"/workspace","readOnly":true},
            {"name":"dev-go-mod","mountPath":"/go/pkg/mod"},
            {"name":"dev-go-sumdb","mountPath":"/go/pkg/sumdb"},
            {"name":"dev-go-build","mountPath":"/go/build-cache"},
            {"name":"dev-go-tools","mountPath":"/go/tools"}
          ]) |
        .env = (((.env // []) | map(select(.name != "GOMODCACHE" and .name != "GOCACHE" and
          .name != "GOWORK" and .name != "GOTOOLCHAIN" and .name != "GOTMPDIR" and .name != "HOME" and
          .name != "KODEX_DEV_AIR_VERSION"))) +
          [
            {"name":"GOMODCACHE","value":"/go/pkg/mod"},
            {"name":"GOCACHE","value":("/go/build-cache/" + strenv(CACHE_KEY))},
            {"name":"GOWORK","value":"off"},
            {"name":"GOTOOLCHAIN","value":"local"},
            {"name":"GOTMPDIR","value":("/go/build-cache/" + strenv(CACHE_KEY) + "/tmp")},
            {"name":"HOME","value":("/go/build-cache/" + strenv(CACHE_KEY) + "/home")},
            {"name":"KODEX_DEV_AIR_VERSION","value":"v1.63.4"}
          ])
      )
    )
  ' "$render"
}

patch_go_init_container() {
  local workload=$1 container=$2 module=$3 package=$4
  WORKLOAD="$workload" CONTAINER="$container" MODULE="$module" PACKAGE="$package" \
  CACHE_KEY="$workload-$container" \
  GO_IMAGE='docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83' \
  yq -i '
    with(select(.kind == "Deployment" and .metadata.name == strenv(WORKLOAD));
      (.spec.template.spec.initContainers[] | select(.name == strenv(CONTAINER))) |= (
        .image = strenv(GO_IMAGE) |
        .imagePullPolicy = "IfNotPresent" |
        .command = ["/workspace/tools/dev/run-authority-socket-init.sh"] |
        .args = [] |
        .workingDir = ("/workspace/" + strenv(MODULE)) |
        .resources = {"requests":{"cpu":"25m","memory":"64Mi"}} |
        .securityContext.runAsNonRoot = false |
        .securityContext.runAsUser = 0 |
        .securityContext.runAsGroup = 0 |
        .securityContext.readOnlyRootFilesystem = false |
        .securityContext.capabilities = {
          "drop":["ALL"],
          "add":["SETUID","SETGID"]
        } |
        .volumeMounts = (((.volumeMounts // []) |
          map(select(.name != "dev-source" and .name != "dev-go-mod" and .name != "dev-go-sumdb" and
            .name != "dev-go-build" and .name != "dev-go-tools"))) +
          [
            {"name":"dev-source","mountPath":"/workspace","readOnly":true},
            {"name":"dev-go-mod","mountPath":"/go/pkg/mod"},
            {"name":"dev-go-sumdb","mountPath":"/go/pkg/sumdb"},
            {"name":"dev-go-build","mountPath":"/go/build-cache"},
            {"name":"dev-go-tools","mountPath":"/go/tools"}
          ]) |
        .env = (((.env // []) | map(select(.name != "GOMODCACHE" and .name != "GOCACHE" and
          .name != "GOWORK" and .name != "GOTOOLCHAIN" and .name != "GOTMPDIR" and
          .name != "HOME"))) +
          [
            {"name":"GOMODCACHE","value":"/go/pkg/mod"},
            {"name":"GOCACHE","value":("/go/build-cache/" + strenv(CACHE_KEY))},
            {"name":"GOWORK","value":"off"},
            {"name":"GOTOOLCHAIN","value":"local"},
            {"name":"GOTMPDIR","value":("/go/build-cache/" + strenv(CACHE_KEY) + "/tmp")},
            {"name":"HOME","value":("/go/build-cache/" + strenv(CACHE_KEY) + "/home")}
          ])
      )
    )
  ' "$render"
}

patch_go_job() {
  local workload=$1 container=$2 module=$3 package=$4
  shift 4
  local args_json
  args_json=$(printf '%s\n' "$@" | jq -Rsc 'split("\n") | map(select(length > 0))')
  add_development_volumes Job "$workload"
  WORKLOAD="$workload" CONTAINER="$container" MODULE="$module" PACKAGE="$package" \
  COMMAND_ARGS="$args_json" CACHE_KEY="$workload-$container" \
  GO_IMAGE='docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83' \
  yq -i '
    with(select(.kind == "Job" and .metadata.name == strenv(WORKLOAD));
      (.spec.template.spec.containers[] | select(.name == strenv(CONTAINER))) |= (
        .image = strenv(GO_IMAGE) |
        .imagePullPolicy = "IfNotPresent" |
        .command = ["/workspace/tools/dev/run-go-command.sh"] |
        .args = ([strenv(MODULE),strenv(PACKAGE)] + (strenv(COMMAND_ARGS) | from_json)) |
        .workingDir = ("/workspace/" + strenv(MODULE)) |
        .resources = {"requests":{"cpu":"50m","memory":"128Mi"}} |
        .volumeMounts = (((.volumeMounts // []) |
          map(select(.name != "dev-source" and .name != "dev-go-mod" and .name != "dev-go-sumdb" and
            .name != "dev-go-build" and .name != "dev-go-tools"))) +
          [
            {"name":"dev-source","mountPath":"/workspace","readOnly":true},
            {"name":"dev-go-mod","mountPath":"/go/pkg/mod"},
            {"name":"dev-go-sumdb","mountPath":"/go/pkg/sumdb"},
            {"name":"dev-go-build","mountPath":"/go/build-cache"},
            {"name":"dev-go-tools","mountPath":"/go/tools"}
          ]) |
        .env = (((.env // []) | map(select(.name != "GOMODCACHE" and .name != "GOCACHE" and
          .name != "GOWORK" and .name != "GOTOOLCHAIN" and .name != "GOTMPDIR" and
          .name != "HOME"))) +
          [
            {"name":"GOMODCACHE","value":"/go/pkg/mod"},
            {"name":"GOCACHE","value":("/go/build-cache/" + strenv(CACHE_KEY))},
            {"name":"GOWORK","value":"off"},
            {"name":"GOTOOLCHAIN","value":"local"},
            {"name":"GOTMPDIR","value":("/go/build-cache/" + strenv(CACHE_KEY) + "/tmp")},
            {"name":"HOME","value":("/go/build-cache/" + strenv(CACHE_KEY) + "/home")}
          ])
      )
    )
  ' "$render"
}

patch_go_container Deployment control-plane control-plane services/internal/control-plane ./cmd/control-plane
patch_go_container Deployment control-plane internal-rpc-authority-verifier services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-verifier
patch_go_container Deployment secret-broker secret-broker services/internal/secret-broker ./cmd/secret-broker
patch_go_container Deployment secret-broker internal-rpc-authority-issuer services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-issuer
patch_go_container Deployment secret-broker platform-worker-grant-agent services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-platform-worker-grant-agent
patch_go_container Deployment control-api-gateway control-api-gateway services/external/control-api-gateway ./cmd/control-api-gateway
patch_go_container Deployment control-api-gateway internal-rpc-authority-issuer services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-issuer
patch_go_container Deployment egress-gateway egress-gateway services/external/egress-gateway ./cmd/egress-gateway
patch_go_container Deployment runtime-controller runtime-controller services/internal/runtime-controller ./cmd/runtime-controller
patch_go_container Deployment runtime-controller internal-rpc-authority-issuer services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-issuer
patch_go_container Deployment runtime-controller platform-worker-grant-agent services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-platform-worker-grant-agent
patch_go_container Deployment integration-gateway integration-gateway services/external/integration-gateway ./cmd/integration-gateway
patch_go_container Deployment integration-gateway internal-rpc-authority-issuer services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-issuer
patch_go_container Deployment integration-gateway platform-worker-grant-agent services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-platform-worker-grant-agent
patch_go_container Deployment integration-synthetic integration-synthetic services/external/integration-gateway ./cmd/integration-synthetic
patch_go_container Deployment automation-scheduler automation-scheduler services/jobs/automation-scheduler ./cmd/automation-scheduler
patch_go_container Deployment artifact-retention artifact-retention services/jobs/artifact-retention ./cmd/artifact-retention
patch_go_container Deployment automation-scheduler internal-rpc-authority-issuer services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-issuer
patch_go_container Deployment automation-scheduler platform-worker-grant-agent services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-platform-worker-grant-agent
patch_go_container Deployment session-archive internal-rpc-authority-issuer services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-issuer
patch_go_container Deployment session-archive platform-worker-grant-agent services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-platform-worker-grant-agent
patch_go_container Deployment internal-rpc-authority-publisher publisher services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-publisher
patch_go_container Deployment internal-rpc-authority-readback-attestor readback-attestor services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-readback-attestor
patch_go_container Deployment internal-rpc-authority-restore-controller restore-controller services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-restore-controller

yq -i '
  with(select(.kind == "Deployment" and .metadata.name == "integration-synthetic");
    (.spec.template.spec.containers[] | select(.name == "integration-synthetic")) |= (
      .resources = {
        "requests":{"cpu":"25m","memory":"64Mi"},
        "limits":{"cpu":"500m","memory":"512Mi"}
      } |
      .securityContext.readOnlyRootFilesystem = true
    ) |
    (.spec.template.spec.volumes[] | select(.name == "tmp")).emptyDir = {"sizeLimit":"64Mi"}
  )
' "$render"

for workload in control-plane secret-broker control-api-gateway runtime-controller integration-gateway automation-scheduler session-archive; do
  patch_go_init_container "$workload" internal-rpc-authority-socket-init \
    services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-socket-init
done

patch_go_job internal-rpc-authority-migrate migrate services/internal/internal-rpc-authority ./cmd/cli up
patch_go_job control-plane-migrate migrate services/internal/control-plane ./cmd/cli up
patch_go_job control-plane-broker-bootstrap bootstrap services/internal/control-plane ./cmd/cli broker bootstrap

NODE_IMAGE='docker.io/library/node:24.17.0-alpine3.23@sha256:7c70d1235c0b4c2bc9eeed5393d19f1bbdde6885ba0d58ba62bb385d7b0f3ff1' \
SOURCE_ROOT="$source_root" CACHE_ROOT="$cache_root" PUBLIC_HOST="$public_host" \
SOURCE_DIGEST="$source_digest" OIDC_ISSUER="$oidc_issuer" yq -i '
  with(select(.kind == "Deployment" and .metadata.name == "staff-control-center");
    .spec.replicas = 1 |
    .spec.template.spec.securityContext.runAsNonRoot = false |
    .spec.template.spec.securityContext.runAsUser = 0 |
    .spec.template.spec.securityContext.runAsGroup = 0 |
    .spec.template.spec.volumes = (
      ((.spec.template.spec.volumes // []) |
        map(select(.name != "dev-source" and .name != "dev-node-modules"))) +
      [
        {"name":"dev-source","hostPath":{"path":strenv(SOURCE_ROOT),"type":"Directory"}},
        {"name":"dev-node-modules","hostPath":{"path":(strenv(CACHE_ROOT) + "/node-modules"),"type":"Directory"}}
      ]
    ) |
    (.spec.template.spec.containers[] | select(.name == "staff-control-center")) |= (
      .image = strenv(NODE_IMAGE) |
      .imagePullPolicy = "IfNotPresent" |
      .command = ["/workspace/tools/dev/run-frontend.sh"] |
      .args = [] |
      .workingDir = "/workspace/services/staff/control-center" |
      .ports = [{"name":"http","containerPort":8080,"protocol":"TCP"}] |
      .resources = {"requests":{"cpu":"50m","memory":"128Mi"}} |
      .securityContext.runAsNonRoot = false |
      .securityContext.runAsUser = 0 |
      .securityContext.runAsGroup = 0 |
      .securityContext.readOnlyRootFilesystem = false |
      .volumeMounts = [
        {"name":"dev-source","mountPath":"/workspace","readOnly":true},
        {"name":"dev-node-modules","mountPath":"/workspace/services/staff/control-center/node_modules"},
        (((.volumeMounts // [])[] | select(.name == "runtime-config")) |
          .mountPath = "/workspace/services/staff/control-center/public/config" |
          .readOnly = true)
      ] |
      .env = [
        {"name":"KODEX_DEV_PUBLIC_HOST","value":strenv(PUBLIC_HOST)},
        {"name":"KODEX_DEV_API_TARGET","value":"https://control-api-gateway.kodex-system.svc:8443"}
      ]
    )
  ) |
  with(select(.kind == "Service" and .metadata.name == "staff-control-center");
    del(.metadata.annotations."traefik.ingress.kubernetes.io/service.serverstransport") |
    .spec.ports = [{"name":"http","port":8080,"targetPort":"http","protocol":"TCP"}]
  ) |
  with(select(.kind == "Ingress" and
      (.metadata.name == "staff-control-center" or .metadata.name == "staff-control-center-api"));
    del(.metadata.annotations."traefik.ingress.kubernetes.io/router.middlewares") |
    .spec.rules[].http.paths[].backend.service.port.name = "http"
  ) |
  with(select(.kind == "ConfigMap" and (.metadata.name | test("^staff-control-center-runtime-")));
    .immutable = false |
    .data."runtime-config.json" = ({
      "revision":strenv(SOURCE_DIGEST),
      "environment":"development",
      "apiBaseUrl":"/",
      "realtimeUrl":"/api/v1",
      "requestTimeoutMs":15000,
      "oidc":{
        "authority":strenv(OIDC_ISSUER),
        "clientId":"kodex-control-center",
        "redirectUri":"/auth/callback",
        "postLogoutRedirectUri":"/",
        "scope":"openid kodex.owner"
      }
    } | to_json)
  )
' "$render"

PUBLIC_HOST="$public_host" yq -i '
  with(select(.kind == "Deployment" and .metadata.name == "staff-control-center");
    (.spec.template.spec.containers[] | select(.name == "staff-control-center")) |= (
      .startupProbe = {"httpGet":{"path":"/src/main.ts","port":"http","scheme":"HTTP"},"periodSeconds":2,"timeoutSeconds":2,"failureThreshold":60} |
      .readinessProbe = {"httpGet":{"path":"/src/main.ts","port":"http","scheme":"HTTP"},"periodSeconds":3,"timeoutSeconds":2,"failureThreshold":3} |
      .livenessProbe = {"httpGet":{"path":"/src/main.ts","port":"http","scheme":"HTTP"},"periodSeconds":10,"timeoutSeconds":2,"failureThreshold":3}
    )
  )
' "$render"

SESSION_ARCHIVE_IMAGE="$session_archive_image" \
AUTHORITY_SOURCE_REVISION="$authority_source_revision" yq -i '
  with(select(.kind == "ConfigMap" and
      .metadata.name == "internal-rpc-authority-publisher-target-registry");
    .data."key-delivery-targets.yaml" |= (
      from_yaml |
      .source_revision = (strenv(AUTHORITY_SOURCE_REVISION) | tonumber) |
      .targets |= map(select(
        .workload_id == "automation-scheduler" or
        .workload_id == "control-api-gateway" or
        .workload_id == "control-plane" or
        .workload_id == "image-admission" or
        .workload_id == "image-promotion" or
        .workload_id == "integration-gateway" or
        .workload_id == "role-image-builder" or
        .workload_id == "secret-broker" or
        .workload_id == "session-archive" or
        .workload_id == "runtime-controller"
      )) |
      to_yaml
    )
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "session-archive");
    (.spec.template.spec.containers[] | select(.name == "session-archive")) |= (
      .image = strenv(SESSION_ARCHIVE_IMAGE) |
      .imagePullPolicy = "IfNotPresent" |
      (.env[] | select(.name == "SESSION_ARCHIVE_WORKER_IMAGE").value) =
        strenv(SESSION_ARCHIVE_IMAGE)
    )
  )
' "$render"

# Keep the exact local API endpoint in the deterministic render input for
# diagnostics and readback.
jq -n --arg endpoint "$kubernetes_endpoint_cidr" --arg port "$kubernetes_endpoint_port" \
  '{endpointCIDR:$endpoint,endpointPort:($port|tonumber)}' >"$temporary_directory/api.json"

# These annotations are inputs for runtime Pod materialization. Hot reload
# replaces both source containers, so synchronize only after every local image
# substitution and make the effective Pod template the canonical source.
yq -i '
  with(select(.kind == "Deployment" and .metadata.name == "runtime-controller");
    .spec.template.metadata.annotations."kodex.dev/controller-image" =
      (.spec.template.spec.containers[] |
        select(.name == "runtime-controller") | .image) |
    .spec.template.metadata.annotations."kodex.dev/authority-image" =
      (.spec.template.spec.containers[] |
        select(.name == "internal-rpc-authority-issuer") | .image)
  )
' "$render"

yq -o=json -I=0 '.' "$render" | jq -sc '
  map(select(.kind != null)) |
  unique_by([.apiVersion,.kind,(.metadata.namespace // ""),.metadata.name])[]
' | yq -p=json -P >"$output"

# Local workloads use disposable telemetry settings. Apply this only after all
# generated init containers have been materialized into the final manifest.
yq -i '
  with(select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "Job");
    (.spec.template.spec.initContainers[]?,
      .spec.template.spec.containers[]?) |= (
      .env = ((.env // []) | map(select(.name != "OTEL_SDK_DISABLED")) +
        [{"name":"OTEL_SDK_DISABLED","value":"true"}])
    )
  )
' "$output"

# Kubernetes still resolves YAML 1.1 boolean-like scalars in string fields.
# Quote every literal env value so tokens such as GOWORK=off remain strings.
yq -i '
  (select(.kind == "Deployment" or .kind == "Job" or .kind == "StatefulSet") |
    .spec.template.spec |
    (.initContainers[]?, .containers[]?) |
    .env[]? |
    select(has("value")) |
    .value) style="double"
' "$output"

render_digest=$(sha256sum "$output" | awk '{print $1}')
RENDER_DIGEST="$render_digest" yq -i '
  with(select(.kind == "Deployment");
    .spec.template.metadata.annotations."kodex.dev/render-sha256" = strenv(RENDER_DIGEST)
  )
' "$output"

if rg -n '__KODEX_[A-Z0-9_]+__|\.invalid' "$output" >/dev/null; then
  fail 'local render contains unresolved placeholders'
fi
if yq -e '
  select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "Job") |
  .spec.template.spec |
  (.initContainers[]?, .containers[]?) | .env[]? |
  select(.valueFrom.configMapKeyRef.name == "kodex-image-admission-policy")
' "$output" >/dev/null 2>&1; then
  fail 'local workload retains an indirect immutable admission policy reference'
fi
if yq -e '
  select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "Job") |
  .spec.template.spec |
  (.initContainers[]?, .containers[]?) |
  select(.image | test("@sha256:0{64}$"))
' "$output" >/dev/null 2>&1; then
  fail 'local workload contains an unresolved image digest'
fi
yq -o=json -I=0 '.' "$output" | jq -s -e '
  any(.[];
    .kind == "Deployment" and .metadata.name == "runtime-controller" and
    (.spec.template.metadata.annotations["kodex.dev/controller-image"] |
      test("@sha256:[a-f0-9]{64}$")) and
    (.spec.template.metadata.annotations["kodex.dev/authority-image"] |
      test("@sha256:[a-f0-9]{64}$")) and
    .spec.template.metadata.annotations["kodex.dev/controller-image"] ==
      ([.spec.template.spec.containers[] |
        select(.name == "runtime-controller") | .image] | first) and
    .spec.template.metadata.annotations["kodex.dev/authority-image"] ==
      ([.spec.template.spec.containers[] |
        select(.name == "internal-rpc-authority-issuer") | .image] | first))
' >/dev/null || fail 'runtime-controller image annotations do not match effective local containers'
yq -e 'select(.kind == "Deployment" and .metadata.name == "staff-control-center")' "$output" >/dev/null ||
  fail 'frontend development workload is absent'
yq -e 'select(.kind == "Deployment" and .metadata.name == "control-plane")' "$output" >/dev/null ||
  fail 'Control Plane development workload is absent'
yq -o=json -I=0 '.' "$output" | jq -s -e --arg runnerImage "$runtime_runner_image" '
  any(.[];
    .kind == "Deployment" and .metadata.name == "secret-broker" and
    any(.spec.template.spec.initContainers[]?;
      .name == "codex-cli-install" and .image == $runnerImage and
      .imagePullPolicy == "IfNotPresent" and
      .resources.requests.cpu == "10m" and
      .resources.requests.memory == "64Mi" and
      .resources.limits.cpu == "100m" and
      .resources.limits.memory == "256Mi" and
      any(.args[]?; contains("binary=/usr/local/bin/codex")) and
      any(.args[]?; contains("mv -f \"$temporary\" /codex/codex")) and
      any(.volumeMounts[]?; .name == "codex-cli" and .mountPath == "/codex")) and
    any(.spec.template.spec.containers[]?;
      .name == "secret-broker" and
      any(.env[]?;
        .name == "SECRET_BROKER_CODEX_BINARY" and
        .value == "/codex/codex") and
      any(.volumeMounts[]?;
        .name == "codex-cli" and
        .mountPath == "/codex" and .readOnly == true)) and
    any(.spec.template.spec.volumes[]?;
      .name == "codex-cli" and .emptyDir.sizeLimit == "384Mi"))
' >/dev/null || fail 'secret-broker local Codex app-server binary is absent'
yq -o=json -I=0 '.' "$output" | jq -s -e '
  [
    .[] |
    select(.kind == "Deployment") as $deployment |
    $deployment.spec.template.spec.containers[] |
    select(
      .name == "internal-rpc-authority-issuer" or
      .name == "internal-rpc-authority-verifier"
    ) as $container |
    ($container.name | sub("^internal-rpc-authority-"; "")) as $role |
    ("internal-rpc-authority-" + $deployment.metadata.name + "-" + $role + "-postgresql") as $expected |
    ($container.env[] |
      select(.name == "INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER") |
      .valueFrom.secretKeyRef.name) as $identitySecret |
    ($container.volumeMounts[] |
      select(.mountPath == "/var/run/secrets/kodex/internal-rpc-authority/postgres") |
      .name) as $databaseVolume |
    ($deployment.spec.template.spec.volumes[] |
      select(.name == $databaseVolume) |
      .secret.secretName) as $databaseSecret |
    select($identitySecret != $expected or $databaseSecret != $expected) |
    {
      workload: $deployment.metadata.name,
      role: $role,
      expected: $expected,
      identitySecret: $identitySecret,
      databaseSecret: $databaseSecret
    }
  ] | length == 0
' >/dev/null || fail 'authority PostgreSQL identity render is inconsistent'
yq -o=json -I=0 '.' "$output" | jq -s -e '
  any(.[]; .kind == "Namespace" and .metadata.name == "kodex-runtime") and
  any(.[];
    .kind == "ServiceAccount" and .metadata.name == "agent-runner" and
    .metadata.namespace == "kodex-runtime") and
  any(.[];
    .kind == "RoleBinding" and .metadata.name == "runtime-controller-workloads" and
    .metadata.namespace == "kodex-runtime" and
    .subjects == [{"kind":"ServiceAccount","name":"runtime-controller","namespace":"kodex-system"}]) and
  any(.[];
    .kind == "RoleBinding" and .metadata.name == "secret-broker-runtime-secrets" and
    .metadata.namespace == "kodex-runtime" and
    .subjects == [{"kind":"ServiceAccount","name":"secret-broker","namespace":"kodex-system"}]) and
  any(.[];
    .kind == "Role" and .metadata.name == "secret-broker-runtime-secrets" and
    .metadata.namespace == "kodex-runtime" and .rules == [{
      "apiGroups":[""],
      "resources":["secrets"],
      "verbs":["get","list","create","update","delete"]
    }]) and
  any(.[];
    .kind == "Deployment" and .metadata.name == "runtime-controller" and
    .metadata.namespace == "kodex-system" and
    any(.spec.template.spec.containers[];
      .name == "runtime-controller" and
      any(.env[]?; .name == "POD_NAMESPACE" and has("valueFrom")))) and
  any(.[];
    .kind == "Deployment" and .metadata.name == "secret-broker" and
    .metadata.namespace == "kodex-system" and
    any(.spec.template.spec.containers[];
      .name == "secret-broker" and
      any(.env[]?; .name == "POD_NAMESPACE" and has("valueFrom")) and
      any(.env[]?; .name == "SECRET_BROKER_RUNTIME_NAMESPACE" and
        .value == "kodex-runtime"))) and
  any(.[];
    .kind == "Role" and .metadata.name == "runtime-controller" and
    .metadata.namespace == "kodex-system" and .rules == [{
      "apiGroups":["coordination.k8s.io"],
      "resources":["leases"],
      "verbs":["get","create","update","patch"]
    }]) and
  ([ .[] |
    select(.kind == "Role" and .metadata.namespace == "kodex-system" and
      (.metadata.name == "runtime-controller" or .metadata.name == "secret-broker")) |
    .rules[]? | .resources[]? ] | index("secrets") == null)
' >/dev/null || fail 'dedicated local runtime namespace boundary is invalid'
yq -e 'select(.kind == "Deployment" and .metadata.name == "integration-synthetic")' "$output" >/dev/null ||
  fail 'integration-synthetic development workload is absent'
yq -o=json -I=0 '.' "$output" | jq -s -e --arg image "$session_archive_image" '
  any(.[];
    .kind == "Deployment" and .metadata.name == "session-archive" and
    .metadata.namespace == "kodex-system" and
    any(.spec.template.spec.containers[];
      .name == "session-archive" and
      .image == $image and .imagePullPolicy == "IfNotPresent" and
      any(.env[];
        .name == "SESSION_ARCHIVE_WORKER_IMAGE" and .value == $image)) and
    any(.spec.template.spec.containers[]; .name == "internal-rpc-authority-issuer") and
    any(.spec.template.spec.containers[]; .name == "platform-worker-grant-agent")) and
  any(.[];
    .kind == "ConfigMap" and .metadata.name == "session-archive-runtime" and
    .metadata.namespace == "kodex-system" and
    .data.SESSION_ARCHIVE_WORKER_NAMESPACE == "kodex-runtime") and
  any(.[];
    .kind == "ServiceAccount" and .metadata.name == "session-archive-worker" and
    .metadata.namespace == "kodex-runtime" and .automountServiceAccountToken == false) and
  any(.[];
    .kind == "RoleBinding" and .metadata.name == "session-archive-controller" and
    .metadata.namespace == "kodex-runtime" and
    .roleRef.kind == "Role" and .roleRef.name == "session-archive-controller" and
    (.subjects | length) == 1 and .subjects[0].kind == "ServiceAccount" and
    .subjects[0].name == "session-archive" and .subjects[0].namespace == "kodex-system") and
  any(.[];
    .kind == "Role" and .metadata.name == "session-archive-controller" and
    .metadata.namespace == "kodex-runtime" and
    ([.rules[] | {
      apiGroups:(.apiGroups | sort),
      resources:(.resources | sort),
      verbs:(.verbs | sort)
    }] | sort_by(.resources[0])) ==
    ([
      {apiGroups:[""], resources:["configmaps"], verbs:["create","delete","deletecollection"]},
      {apiGroups:[""], resources:["persistentvolumeclaims"], verbs:["create","delete","get"]},
      {apiGroups:[""], resources:["pods"], verbs:["list"]},
      {apiGroups:["batch"], resources:["jobs"], verbs:["create","delete","deletecollection","get","list"]}
    ] | sort_by(.resources[0]))) and
  any(.[];
    .kind == "Role" and .metadata.name == "session-archive-worker" and
    .metadata.namespace == "kodex-runtime" and (.rules | length) == 0) and
  any(.[];
    .kind == "NetworkPolicy" and
    .metadata.name == "session-archive-worker-default-deny" and
    .metadata.namespace == "kodex-runtime" and
    (.spec.policyTypes | sort) == (["Egress","Ingress"] | sort) and
    ((.spec.ingress // []) | length) == 0 and
    ((.spec.egress // []) | length) == 0) and
  ([.[] | select(
    .metadata.namespace == "kodex-system" and
    ((.kind == "ServiceAccount" and .metadata.name == "session-archive-worker") or
     (.kind == "Role" and .metadata.name == "session-archive-controller") or
     (.kind == "RoleBinding" and .metadata.name == "session-archive-controller") or
     (.kind == "NetworkPolicy" and .metadata.name == "session-archive-worker-object-storage")))]
    | length) == 0
' >/dev/null || fail 'session-archive local controller or exact worker image is absent'
yq -o=json -I=0 '.' "$output" | jq -s -e '
  any(.[];
    .kind == "NetworkPolicy" and
    .metadata.name == "session-archive-worker-object-storage" and
    .metadata.namespace == "kodex-runtime" and
    .spec.podSelector.matchLabels["session-archive.kodex.dev/managed"] == "true" and
    (.spec.policyTypes | sort) == (["Egress","Ingress"] | sort) and
    ((.spec.ingress // []) | length) == 0 and
    (.spec.egress | length) == 2 and
    ([.spec.egress[] | select(
      (.to | length) == 1 and
      .to[0].namespaceSelector.matchLabels["kubernetes.io/metadata.name"] == "kube-system" and
      .to[0].podSelector.matchLabels["k8s-app"] == "kube-dns" and
      ([.ports[] | [.protocol,.port]] | sort) == ([ ["TCP",53], ["UDP",53] ] | sort)
    )] | length) == 1 and
    ([.spec.egress[] | select(
      (.to | length) == 1 and
      .to[0].namespaceSelector.matchLabels["kubernetes.io/metadata.name"] == "kodex-system" and
      .to[0].podSelector.matchLabels["app.kubernetes.io/name"] == "seaweedfs" and
      .to[0].podSelector.matchLabels["app.kubernetes.io/component"] == "object-storage" and
      (.ports | length) == 1 and .ports[0].protocol == "TCP" and .ports[0].port == 8333
    )] | length) == 1) and
  any(.[];
    .kind == "NetworkPolicy" and
    .metadata.name == "seaweedfs-session-archive-runtime-ingress" and
    .metadata.namespace == "kodex-system" and
    any(.spec.ingress[];
      any(.from[]?;
        .namespaceSelector.matchLabels["kubernetes.io/metadata.name"] == "kodex-runtime" and
        .podSelector.matchLabels["session-archive.kodex.dev/managed"] == "true") and
      any(.ports[]?; .protocol == "TCP" and .port == 8333))) and
  any(.[];
    .kind == "NetworkPolicy" and
    .metadata.name == "seaweedfs-exact-local-paths" and
    any(.spec.ingress[];
      any(.from[]?;
        .podSelector.matchLabels["session-archive.kodex.dev/managed"] == "true") and
      any(.ports[]?; .protocol == "TCP" and .port == 8333))) and
  any(.[];
    .kind == "NetworkPolicy" and
    .metadata.name == "seaweedfs-exact-local-paths" and
    any(.spec.ingress[];
      any(.from[]?;
        .podSelector.matchLabels["app.kubernetes.io/name"] == "artifact-retention" and
        .podSelector.matchLabels["app.kubernetes.io/component"] == "retention-job") and
      any(.ports[]?; .protocol == "TCP" and .port == 8333)))
' >/dev/null || fail 'local object storage network paths are incomplete'
BACKUP_CONTROLLER_IMAGE="$backup_controller_image" yq -e '
  select(.kind == "Deployment" and .metadata.name == "backup-controller") |
  .spec.template.spec.containers[] | select(.name == "backup-controller") |
  .image == strenv(BACKUP_CONTROLLER_IMAGE)
' "$output" >/dev/null || fail 'backup-controller exact local image is invalid'
BACKUP_CONTROLLER_REVISION="${backup_controller_image#*@}" yq -e '
  select(.kind == "Deployment" and .metadata.name == "backup-controller") |
  .spec.template.spec.containers[] | select(.name == "backup-controller") |
  .env[] | select(.name == "BACKUP_CONTROLLER_RELEASE_REVISION") |
  .value == strenv(BACKUP_CONTROLLER_REVISION)
' "$output" >/dev/null || fail 'backup-controller local release revision is invalid'
yq -o=json -I=0 '.' "$output" | jq -s -e '
  any(.[];
    .kind == "Deployment" and .metadata.name == "integration-synthetic" and
    .metadata.namespace == "kodex-system" and
    .spec.replicas == 1 and .spec.strategy.type == "Recreate" and
    .spec.template.spec.automountServiceAccountToken == false and
    .spec.template.spec.securityContext.runAsNonRoot == true and
    any(.spec.template.spec.containers[];
      .name == "integration-synthetic" and
      .securityContext.allowPrivilegeEscalation == false and
      .securityContext.readOnlyRootFilesystem == true and
      .securityContext.capabilities.drop == ["ALL"] and
      .resources.requests.cpu == "25m" and .resources.requests.memory == "64Mi" and
      .resources.limits.cpu == "500m" and .resources.limits.memory == "512Mi"))
' >/dev/null || fail 'integration-synthetic security or resource boundary is invalid'
yq -o=json -I=0 '.' "$output" | jq -s -e '
  any(.[];
    .kind == "NetworkPolicy" and .metadata.name == "integration-synthetic-exact-runtime-paths" and
    .metadata.namespace == "kodex-system" and .spec.egress == [] and
    .spec.ingress[0].from[0].namespaceSelector.matchLabels["kubernetes.io/metadata.name"] == "kodex-system" and
    .spec.ingress[0].from[0].podSelector.matchLabels["app.kubernetes.io/name"] == "integration-gateway" and
    .spec.ingress[0].from[0].podSelector.matchLabels["app.kubernetes.io/component"] == "integration-worker" and
    .spec.ingress[0].ports == [{"protocol":"TCP","port":8080}])
' >/dev/null || fail 'integration-synthetic exact NetworkPolicy is absent'
yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "integration-gateway-exact-runtime-paths")' "$output" >/dev/null ||
  fail 'integration-gateway exact NetworkPolicy is absent from the local fixture path'
yq -e 'select(.kind == "StatefulSet" and .metadata.name == "seaweedfs")' "$output" >/dev/null ||
  fail 'SeaweedFS local workload is absent'
yq -e '
  select(.kind == "Job" and .metadata.name == "seaweedfs-bucket-bootstrap") |
  any(.spec.template.spec.containers[];
    .name == "bootstrap" and
    .resources.requests.cpu == "500m" and
    .resources.requests.memory == "256Mi" and
    .resources.limits.cpu == "2" and
    .resources.limits.memory == "512Mi")
' "$output" >/dev/null || fail 'SeaweedFS bucket bootstrap dev resources are invalid'
yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "control-plane-local-object-storage-egress")' "$output" >/dev/null ||
  fail 'Control Plane local object storage egress is absent'
yq -o=json -I=0 '.' "$output" | jq -s -e '
  any(.[];
    .kind == "NetworkPolicy" and .metadata.name == "control-plane-exact-runtime-paths" and
    ([.spec.ingress[0].from[]? | .podSelector.matchLabels["app.kubernetes.io/name"] // empty] |
      index("secret-broker") != null and index("interaction-gateway") != null))
' >/dev/null || fail 'Control Plane internal caller ingress is incomplete'

PROMOTED_PULL_HOST="$promoted_pull_host" \
ROLE_IMAGE_BUILDER_IMAGE="$role_image_builder_image" \
IMAGE_ADMISSION_IMAGE="$image_admission_image" \
IMAGE_ADMISSION_TOOLS_IMAGE="$image_admission_tools_image" \
AUTHORITY_IMAGE="$authority_image" \
RUNNER_IMAGE="$runner_image" yq -o=json -I=0 '.' "$output" | jq -s -e \
  --arg pullHost "$promoted_pull_host" \
  --arg builderImage "$role_image_builder_image" \
  --arg admissionImage "$image_admission_image" \
  --arg toolsImage "$image_admission_tools_image" \
  --arg authorityImage "$authority_image" \
  --arg runnerDigest "$runner_digest" '
  . as $resources |
  (first($resources[] | select(.kind == "ConfigMap" and
    .metadata.name == "kodex-image-admission-policy"))) as $intent |
  (first($resources[] | select(.kind == "ImageAdmissionPolicyParameters" and
    .metadata.name == "kodex-image-admission-policy"))) as $parameters |
  $intent.immutable == true and
  $intent.metadata.annotations["kodex.dev/admission-tools-sha256"] ==
    ($toolsImage | split("@") | .[1]) and
  $intent.data == $parameters.spec and
  $intent.data.toolsImage == $toolsImage and
  $intent.data.admissionImage == $admissionImage and
  $intent.data.authorityImage == $authorityImage and
  $intent.data.pullRegistryHost == $pullHost and
  $intent.data.promotedPullRepository == ($pullHost + "/kodex/roles") and
  $intent.data.nodeReadbackImage ==
    ($pullHost + "/kodex/agent-runner@" + $runnerDigest) and
  ($intent.data.pullCredentialGeneration | tonumber) > 0 and
  ($intent.data.policyRevision | tonumber) > 0 and
  any($resources[]; .kind == "CustomResourceDefinition" and
    .metadata.name == "imageadmissionpolicyparameters.supplychain.kodex.dev") and
  any($resources[]; .kind == "ValidatingAdmissionPolicy" and
    .metadata.name == "kodex-image-admission-controller-jobs") and
  any($resources[]; .kind == "ValidatingAdmissionPolicyBinding" and
    .metadata.name == "kodex-image-admission-controller-jobs") and
  any($resources[]; .kind == "ValidatingAdmissionPolicy" and
    .metadata.name == "kodex-image-admission-controller-workspaces") and
  any($resources[]; .kind == "ValidatingAdmissionPolicyBinding" and
    .metadata.name == "kodex-image-admission-controller-workspaces") and
  (["kodex-image-registry-pull","kodex-image-registry-push",
       "kodex-image-registry-promotion","kodex-image-registry-staging-read",
       "kodex-image-registry-evidence","kodex-buildkit",
       "image-admission-controller","role-image-builder"] |
    all(. as $name |
      any($resources[]; .kind == "Deployment" and .metadata.name == $name))) and
  any($resources[]; .kind == "Deployment" and .metadata.name == "kodex-buildkit" and
    .spec.replicas == 1 and .spec.template.spec.hostUsers == false and
    any(.spec.template.spec.containers[];
      .name == "buildkitd" and .securityContext.privileged == true and
      .resources.requests.cpu == "8" and
      .resources.requests.memory == "8Gi" and
      .resources.limits.cpu == "24" and
      .resources.limits.memory == "64Gi")) and
  any($resources[]; .kind == "Deployment" and .metadata.name == "role-image-builder" and
    any(.spec.template.spec.containers[];
      .name == "role-image-builder" and .image == $builderImage))
' >/dev/null || fail 'local RoleImage supply-chain render contract is invalid'

target_registry=$(yq -N -r '
  select(.kind == "ConfigMap" and
    .metadata.name == "internal-rpc-authority-publisher-target-registry") |
  .data."key-delivery-targets.yaml"
' "$output")
yq -e '
  [.targets[] | select(
    (.workload_id == "image-admission" and
     .service_account == "image-admission") or
    (.workload_id == "image-promotion" and
     .service_account == "image-promotion") or
    (.workload_id == "role-image-builder" and
     .service_account == "role-image-builder")
  )] | length == 3
' <<<"$target_registry" >/dev/null ||
  fail 'local RoleImage authority targets are incomplete'

yq '
  select(
    (.kind == "Deployment" and
      (.metadata.name | test("^(kodex-image-registry-|kodex-buildkit$|image-admission-controller$|role-image-builder$)"))) or
    (.kind == "ConfigMap" and
      (.metadata.name == "kodex-image-admission-policy" or
       .metadata.name == "kodex-buildkit-config")) or
    .kind == "ImageAdmissionPolicyParameters"
  )
' "$output" >"$temporary_directory/image-supply-chain-render.yaml"
if rg -n 'skipTLSVerify:[[:space:]]*true|insecure:[[:space:]]*true|tls_verify:[[:space:]]*false' \
  "$temporary_directory/image-supply-chain-render.yaml" >/dev/null; then
  fail 'local RoleImage supply-chain contains an insecure transport fallback'
fi

printf 'Kodex local render created: %s\n' "$output"
