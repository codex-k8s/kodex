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
    '  --kubernetes-endpoint-cidr <cidr> --kubernetes-endpoint-port <port>' \
    '  --runner-image <repository@sha256:digest>' \
    '  --session-archive-image <repository@sha256:digest>' \
    '  --backup-controller-image <repository@sha256:digest>' >&2
}

source_root=""
cache_root=""
output=""
public_host=""
oidc_host=""
kubernetes_service_cidr=""
kubernetes_endpoint_cidr=""
kubernetes_endpoint_port=""
runner_image=""
session_archive_image=""
backup_controller_image=""
while (($# > 0)); do
  case "$1" in
    --source-root) source_root=${2:-}; shift 2 ;;
    --cache-root) cache_root=${2:-}; shift 2 ;;
    --output) output=${2:-}; shift 2 ;;
    --public-host) public_host=${2:-}; shift 2 ;;
    --oidc-host) oidc_host=${2:-}; shift 2 ;;
    --kubernetes-service-cidr) kubernetes_service_cidr=${2:-}; shift 2 ;;
    --kubernetes-endpoint-cidr) kubernetes_endpoint_cidr=${2:-}; shift 2 ;;
    --kubernetes-endpoint-port) kubernetes_endpoint_port=${2:-}; shift 2 ;;
    --runner-image) runner_image=${2:-}; shift 2 ;;
    --session-archive-image) session_archive_image=${2:-}; shift 2 ;;
    --backup-controller-image) backup_controller_image=${2:-}; shift 2 ;;
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
[[ "$kubernetes_service_cidr" =~ /32$ && "$kubernetes_endpoint_cidr" =~ /32$ ]] ||
  fail 'Kubernetes API CIDRs are invalid'
[[ "$kubernetes_endpoint_port" =~ ^[1-9][0-9]{0,4}$ ]] || fail 'Kubernetes API port is invalid'
[[ "$runner_image" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] ||
  fail 'local runner image must use an exact manifest digest'
[[ "$session_archive_image" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] ||
  fail 'local session archive image must use an exact manifest digest'
[[ "$backup_controller_image" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] ||
  fail 'local backup-controller image must use an exact manifest digest'
for command_name in git jq kubectl sha256sum yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
[[ "$repository_root" == "$source_root" ]] || fail 'source root must match the current worktree'
lock_file="$repository_root/tools/dev/components.lock.json"
seaweedfs_image=$(jq -er '
  .images[] | select(.name == "seaweedfs" and .version == "4.41") | .reference
' "$lock_file") || fail 'SeaweedFS image lock is absent'
[[ "$seaweedfs_image" =~ ^docker\.io/chrislusf/seaweedfs@sha256:[a-f0-9]{64}$ ]] ||
  fail 'SeaweedFS image lock is invalid'
# These directories are mounted directly as hostPath volumes. k3s may remap
# container root to an unprivileged host UID, so the local-only shared caches
# must be writable independently of the private state directory permissions.
install -d -m 0777 "$cache_root/go-mod" "$cache_root/go-build" "$cache_root/go-tools" \
  "$cache_root/node-modules"
chmod 0777 "$cache_root/go-mod" "$cache_root/go-build" "$cache_root/go-tools" \
  "$cache_root/node-modules"
install -d -m 0777 "$source_root/services/staff/control-center/node_modules"
chmod 0777 "$source_root/services/staff/control-center/node_modules"

temporary_directory=$(mktemp -d)
render="$temporary_directory/local.yaml"
kubectl kustomize "$repository_root/deploy/k8s/profiles/web-only" >"$render"
printf '\n---\n' >>"$render"
kubectl kustomize "$repository_root/deploy/k8s/base/local-object-storage" >>"$render"
printf '\n---\n' >>"$render"
kubectl kustomize "$repository_root/deploy/k8s/overlays/local/integration-synthetic" >>"$render"

source_revision=$(git -C "$source_root" rev-parse HEAD)
source_digest=$(printf '%s' "$source_revision" | sha256sum | awk '{print $1}')
oidc_issuer="https://$oidc_host/realms/kodex"
oidc_jwks_url="$oidc_issuer/protocol/openid-connect/certs"
public_origin="https://$public_host"
oidc_origin="https://$oidc_host"

PUBLIC_HOST="$public_host" PUBLIC_ORIGIN="$public_origin" \
OIDC_ISSUER="$oidc_issuer" OIDC_JWKS_URL="$oidc_jwks_url" \
OIDC_HOST="$oidc_host" OIDC_ORIGIN="$oidc_origin" \
KUBERNETES_SERVICE_CIDR="$kubernetes_service_cidr" \
SOURCE_REVISION="$source_revision" SOURCE_DIGEST="$source_digest" \
SEAWEEDFS_IMAGE="$seaweedfs_image" yq -i '
  (.. | select(tag == "!!str")) |= (
    sub("__KODEX_PUBLIC_HOST__"; strenv(PUBLIC_HOST)) |
    sub("__KODEX_PUBLIC_ORIGIN__"; strenv(PUBLIC_ORIGIN)) |
    sub("__KODEX_OIDC_ISSUER__"; strenv(OIDC_ISSUER)) |
    sub("__KODEX_OIDC_JWKS_URL__"; strenv(OIDC_JWKS_URL)) |
    sub("__KODEX_OIDC_CONNECT_ADDRESS__"; "sso.identity.svc.cluster.local:443") |
    sub("__KODEX_OIDC_TLS_SERVER_NAME__"; strenv(OIDC_HOST)) |
    sub("__KODEX_OIDC_ORIGIN__"; strenv(OIDC_ORIGIN)) |
    sub("__KODEX_INGRESS_CLASS__"; "traefik") |
    sub("__KODEX_CLUSTER_ISSUER__"; "kodex-local") |
    sub("__KODEX_INGRESS_NAMESPACE__"; "kube-system") |
    sub("__KODEX_INGRESS_POD_NAME__"; "traefik") |
    sub("__KODEX_OIDC_NAMESPACE__"; "identity") |
    sub("__KODEX_OIDC_POD_NAME__"; "sso") |
    sub("__KODEX_OIDC_POD_COMPONENT__"; "identity-provider") |
    sub("__KODEX_KUBERNETES_API_SERVICE_CIDR__"; strenv(KUBERNETES_SERVICE_CIDR)) |
    sub("__KODEX_SEAWEEDFS_IMAGE__"; strenv(SEAWEEDFS_IMAGE)) |
    sub("registry-pull\\.invalid"; "registry.local.kodex") |
    sub("admission-tools\\.invalid"; "admission-tools.local.kodex")
  ) |
  with(select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "Job");
    .spec.template.metadata.labels."kodex.dev/environment" = "staging" |
    .spec.template.metadata.labels."kodex.dev/local-profile" = "hot-reload" |
    (.spec.template.spec.containers[] | select(.startupProbe != null) |
      .startupProbe.failureThreshold) = 180 |
    (.spec.template.spec.containers[] | select(.startupProbe != null) |
      .startupProbe.periodSeconds) = 2 |
    .spec.template.spec.containers[] |= (
      .env = ((.env // []) | map(select(.name != "OTEL_SDK_DISABLED")) +
        [{"name":"OTEL_SDK_DISABLED","value":"true"}])
    )
  ) |
  with(select(.metadata.labels != null);
    .metadata.labels."kodex.dev/environment" = "staging" |
    .metadata.labels."kodex.dev/local-profile" = "hot-reload"
  )
' "$render"

# Локальный профиль не запускает supply-chain и role runtime. Он сохраняет их
# конфигурационный контракт для Control Plane, но не тратит ресурсы на registry,
# BuildKit и promotion до отдельной локальной задачи.
yq -i '
  select(
    (.kind != "NetworkPolicy" or
      (.metadata.name | test("^(backup-controller|control-plane|seaweedfs|integration-gateway|integration-synthetic|session-archive)"))) and
    .kind != "PodDisruptionBudget" and
    .kind != "ServiceMonitor" and
    .kind != "PodMonitor" and
    .kind != "PrometheusRule" and
    .kind != "CronJob" and
    .kind != "ValidatingAdmissionPolicy" and
    .kind != "ValidatingAdmissionPolicyBinding" and
    .kind != "ImageAdmissionPolicyParameters" and
    (.kind != "CustomResourceDefinition" or .metadata.name != "imageadmissionpolicyparameters.supplychain.kodex.dev") and
    (.kind != "PersistentVolumeClaim") and
    (.kind != "IngressRouteTCP") and
    (.kind != "Deployment" or .metadata.name == "control-plane" or
      .metadata.name == "control-api-gateway" or .metadata.name == "egress-gateway" or
      .metadata.name == "runtime-controller" or .metadata.name == "integration-gateway" or
      .metadata.name == "integration-synthetic" or
      .metadata.name == "backup-controller" or
      .metadata.name == "automation-scheduler" or .metadata.name == "staff-control-center" or
      .metadata.name == "session-archive" or
      .metadata.name == "internal-rpc-authority-publisher" or
      .metadata.name == "internal-rpc-authority-readback-attestor" or
      .metadata.name == "internal-rpc-authority-restore-controller") and
    (.kind != "Job" or .metadata.name == "control-plane-migrate" or
      .metadata.name == "control-plane-broker-bootstrap" or
      .metadata.name == "internal-rpc-authority-migrate" or
      .metadata.name == "kodex-postgresql-runtime-credentials" or
      .metadata.name == "seaweedfs-bucket-bootstrap")
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

API_ENDPOINT_CIDR="$kubernetes_endpoint_cidr" API_ENDPOINT_PORT="$kubernetes_endpoint_port" \
OIDC_HOST="$oidc_host" yq -i '
  with(select(.kind == "ConfigMap" and .metadata.name == "kodex-platform-endpoints");
    .data.oidcConnectAddress = "sso.identity.svc.cluster.local:443" |
    .data.oidcTlsServerName = strenv(OIDC_HOST)
  ) |
  with(select(.kind == "NetworkPolicy" and .metadata.name == "control-plane-exact-runtime-paths");
    (.spec.egress[].ports[] | select(.port == "__KODEX_OIDC_TARGET_PORT__").port) = 443
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
        .name == "internal-rpc-authority-verifier") | .env) =
      (((.spec.template.spec.containers[] |
        select(.name == "internal-rpc-authority-issuer" or
          .name == "internal-rpc-authority-verifier") | .env // []) |
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
          map(select(.name != "dev-source" and .name != "dev-go-mod" and
            .name != "dev-go-build" and .name != "dev-go-tools"))) +
        [
          {"name":"dev-source","hostPath":{"path":strenv(SOURCE_ROOT),"type":"Directory"}},
          {"name":"dev-go-mod","hostPath":{"path":(strenv(CACHE_ROOT) + "/go-mod"),"type":"Directory"}},
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
          map(select(.name != "dev-source" and .name != "dev-go-mod" and
            .name != "dev-go-build" and .name != "dev-go-tools"))) +
          [
            {"name":"dev-source","mountPath":"/workspace","readOnly":true},
            {"name":"dev-go-mod","mountPath":"/go/pkg/mod"},
            {"name":"dev-go-build","mountPath":"/go/build-cache"},
            {"name":"dev-go-tools","mountPath":"/go/tools"}
          ]) |
        .env = (((.env // []) | map(select(.name != "GOMODCACHE" and .name != "GOCACHE" and
          .name != "GOWORK" and .name != "GOTOOLCHAIN" and .name != "HOME" and
          .name != "KODEX_DEV_AIR_VERSION"))) +
          [
            {"name":"GOMODCACHE","value":"/go/pkg/mod"},
            {"name":"GOCACHE","value":("/go/build-cache/" + strenv(CACHE_KEY))},
            {"name":"GOWORK","value":"off"},
            {"name":"GOTOOLCHAIN","value":"local"},
            {"name":"HOME","value":"/tmp/kodex-home"},
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
        .command = ["/workspace/tools/dev/run-go-command.sh"] |
        .args = [strenv(MODULE),strenv(PACKAGE)] |
        .workingDir = ("/workspace/" + strenv(MODULE)) |
        .resources = {"requests":{"cpu":"25m","memory":"64Mi"}} |
        .securityContext.runAsNonRoot = false |
        .securityContext.runAsUser = 0 |
        .securityContext.runAsGroup = 0 |
        .securityContext.readOnlyRootFilesystem = false |
        .volumeMounts = (((.volumeMounts // []) |
          map(select(.name != "dev-source" and .name != "dev-go-mod" and
            .name != "dev-go-build" and .name != "dev-go-tools"))) +
          [
            {"name":"dev-source","mountPath":"/workspace","readOnly":true},
            {"name":"dev-go-mod","mountPath":"/go/pkg/mod"},
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
          map(select(.name != "dev-source" and .name != "dev-go-mod" and
            .name != "dev-go-build" and .name != "dev-go-tools"))) +
          [
            {"name":"dev-source","mountPath":"/workspace","readOnly":true},
            {"name":"dev-go-mod","mountPath":"/go/pkg/mod"},
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
patch_go_container Deployment automation-scheduler internal-rpc-authority-issuer services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-issuer
patch_go_container Deployment automation-scheduler platform-worker-grant-agent services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-platform-worker-grant-agent
patch_go_container Deployment session-archive session-archive services/jobs/session-archive ./cmd/session-archive controller
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

for workload in control-plane control-api-gateway runtime-controller integration-gateway automation-scheduler session-archive; do
  patch_go_init_container "$workload" internal-rpc-authority-socket-init \
    services/internal/internal-rpc-authority ./cmd/internal-rpc-authority-socket-init
done

yq -i '
  with(select(.kind == "Deployment");
    (.spec.template.spec.initContainers[]? |
      select(.name == "internal-rpc-authority-socket-init")) |= (
        .command = ["/workspace/tools/dev/run-authority-socket-init.sh"] |
        .args = [] |
        .securityContext.capabilities = {
          "drop":["ALL"],
          "add":["SETUID","SETGID"]
        }
      )
  )
' "$render"

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

# Dev workload не проходит production supply-chain admission, но значения
# image policy остаются синтаксически валидными для Control Plane.
SOURCE_DIGEST="$source_digest" SOURCE_REVISION="$source_revision" RUNNER_IMAGE="$runner_image" \
SESSION_ARCHIVE_IMAGE="$session_archive_image" yq -i '
  with(select(.kind == "ConfigMap" and .metadata.name == "kodex-image-admission-policy");
    .immutable = false |
    .data.orchestrationRevision = strenv(SOURCE_REVISION) |
    .data.admissionImage = ("registry.local.kodex/kodex/image-admission@sha256:" + strenv(SOURCE_DIGEST)) |
    .data.authorityImage = ("registry.local.kodex/kodex/internal-rpc-authority@sha256:" + strenv(SOURCE_DIGEST)) |
    .data.toolsImage = ("admission-tools.local.kodex/kodex/image-admission-tools@sha256:" + strenv(SOURCE_DIGEST)) |
    .data.policyRevision = "1" |
    .data.policySHA256 = strenv(SOURCE_DIGEST) |
    .data.pullCredentialGeneration = "1" |
    .data.roleRuntimeContractRevision = "1" |
    .data.roleRuntimeContractSHA256 = strenv(SOURCE_DIGEST) |
    .data.frontendSHA256 = strenv(SOURCE_DIGEST) |
    .data.toolchainSHA256 = strenv(SOURCE_DIGEST) |
    .data.trustedRoleBaseDigest = ("sha256:" + strenv(SOURCE_DIGEST)) |
    .data.nodeReadbackImage = strenv(RUNNER_IMAGE)
  ) |
  with(select(.kind == "ConfigMap" and .metadata.name == "kodex-role-environments");
    .immutable = false |
    .data."catalog.json" |= (
      from_json |
      .context.sourceRevision = strenv(SOURCE_REVISION) |
      .context.sourceSha256 = strenv(SOURCE_DIGEST) |
      .context.contextSha256 = strenv(SOURCE_DIGEST) |
      .context.contextRef = ("oci://kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/role-image-inputs@sha256:" + strenv(SOURCE_DIGEST)) |
      .environments[].baseImageDigest = ("sha256:" + strenv(SOURCE_DIGEST)) |
      to_json
    )
  ) |
  with(select(.kind == "ConfigMap" and
      .metadata.name == "internal-rpc-authority-publisher-target-registry");
    .data."key-delivery-targets.yaml" |= (
      from_yaml |
      .source_revision = 1 |
      .targets |= map(select(
        .workload_id == "automation-scheduler" or
        .workload_id == "control-api-gateway" or
        .workload_id == "control-plane" or
        .workload_id == "integration-gateway" or
        .workload_id == "session-archive" or
        .workload_id == "runtime-controller"
      )) |
      to_yaml
    )
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "session-archive");
    (.spec.template.spec.containers[] | select(.name == "session-archive") |
      .env[] | select(.name == "SESSION_ARCHIVE_WORKER_IMAGE").value) =
      strenv(SESSION_ARCHIVE_IMAGE)
  )
' "$render"

# API endpoint egress policy was removed for hot reload. These values still
# participate in deterministic render input and are checked by the installer.
jq -n --arg endpoint "$kubernetes_endpoint_cidr" --arg port "$kubernetes_endpoint_port" \
  '{endpointCIDR:$endpoint,endpointPort:($port|tonumber)}' >"$temporary_directory/api.json"

yq -o=json -I=0 '.' "$render" | jq -sc '
  map(select(.kind != null)) |
  unique_by([.apiVersion,.kind,(.metadata.namespace // ""),.metadata.name])[]
' | yq -p=json -P >"$output"

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
  (.initContainers[]?, .containers[]?) |
  select(.image | test("@sha256:0{64}$"))
' "$output" >/dev/null 2>&1; then
  fail 'local workload contains an unresolved image digest'
fi
yq -e 'select(.kind == "Deployment" and .metadata.name == "staff-control-center")' "$output" >/dev/null ||
  fail 'frontend development workload is absent'
yq -e 'select(.kind == "Deployment" and .metadata.name == "control-plane")' "$output" >/dev/null ||
  fail 'Control Plane development workload is absent'
yq -e 'select(.kind == "Deployment" and .metadata.name == "integration-synthetic")' "$output" >/dev/null ||
  fail 'integration-synthetic development workload is absent'
yq -o=json -I=0 '.' "$output" | jq -s -e --arg image "$session_archive_image" '
  any(.[];
    .kind == "Deployment" and .metadata.name == "session-archive" and
    .metadata.namespace == "kodex-system" and
    any(.spec.template.spec.containers[];
      .name == "session-archive" and
      any(.env[];
        .name == "SESSION_ARCHIVE_WORKER_IMAGE" and .value == $image)) and
    any(.spec.template.spec.containers[]; .name == "internal-rpc-authority-issuer") and
    any(.spec.template.spec.containers[]; .name == "platform-worker-grant-agent"))
' >/dev/null || fail 'session-archive local controller or exact worker image is absent'
yq -o=json -I=0 '.' "$output" | jq -s -e '
  any(.[];
    .kind == "NetworkPolicy" and
    .metadata.name == "session-archive-worker-object-storage" and
    any(.spec.egress[];
      any(.to[]?;
        .podSelector.matchLabels["app.kubernetes.io/name"] == "seaweedfs") and
      any(.ports[]?; .protocol == "TCP" and .port == 8333))) and
  any(.[];
    .kind == "NetworkPolicy" and
    .metadata.name == "seaweedfs-exact-local-paths" and
    any(.spec.ingress[];
      any(.from[]?;
        .podSelector.matchLabels["session-archive.kodex.dev/managed"] == "true") and
      any(.ports[]?; .protocol == "TCP" and .port == 8333)))
' >/dev/null || fail 'session-archive local object storage network path is absent'
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
yq -e 'select(.kind == "Job" and .metadata.name == "seaweedfs-bucket-bootstrap")' "$output" >/dev/null ||
  fail 'SeaweedFS bucket bootstrap is absent'
yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "control-plane-local-object-storage-egress")' "$output" >/dev/null ||
  fail 'Control Plane local object storage egress is absent'

printf 'Kodex local render created: %s\n' "$output"
