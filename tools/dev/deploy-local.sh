#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local deployment failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'Usage: deploy-local.sh --context <exact-context> --mode apply|readback' \
    '  --render <path> --state-directory <path> [--tls-mode local-ca|public-acme]' >&2
}

context=""
mode=""
render=""
state_directory=""
tls_mode=local-ca
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --mode) mode=${2:-}; shift 2 ;;
    --render) render=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --tls-mode) tls_mode=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail 'exact Kubernetes context is required'
case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
case "$tls_mode" in local-ca|public-acme) ;; *) fail 'development TLS mode is invalid' ;; esac
[[ -f "$render" && -s "$render" && ! -L "$render" ]] || fail 'local render is invalid'
[[ "$state_directory" == /* && "$state_directory" != / && -d "$state_directory" &&
  ! -L "$state_directory" ]] || fail 'state directory is invalid'
for command_name in docker jq kubectl openssl sha256sum yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
[[ "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
  fail 'production context is forbidden'

namespace=kodex-system
runtime_namespace=kodex-runtime
script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
object_storage_secret_name=""
temporary_directory=$(mktemp -d)
image_admission_controller_restore_replicas=""

cleanup_on_exit() {
  if [[ -n "$image_admission_controller_restore_replicas" ]]; then
    kubectl -n "$namespace" scale deployment/image-admission-controller \
      --replicas="$image_admission_controller_restore_replicas" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$temporary_directory"
}
trap cleanup_on_exit EXIT

filter_render() {
  local name=$1 expression=$2 output
  output="$temporary_directory/$name.yaml"
  yq "$expression" "$render" >"$output"
  [[ -s "$output" ]] || fail "local deployment phase is empty: $name"
  printf '%s' "$output"
}

apply_render() {
  local name=$1 expression=$2 output
  output=$(filter_render "$name" "$expression")
  kubectl apply --server-side --force-conflicts --field-manager=kodex-local-dev -f "$output" >/dev/null
}

cleanup_local_frontend_transport() {
  kubectl get customresourcedefinition/serverstransports.traefik.io >/dev/null 2>&1 || return 0
  kubectl -n "$namespace" delete \
    serverstransport.traefik.io/staff-control-center \
    --ignore-not-found --wait=true --timeout=2m >/dev/null ||
    fail 'obsolete local frontend ServersTransport cleanup failed'
}

readback_local_frontend_transport() {
  kubectl get customresourcedefinition/serverstransports.traefik.io >/dev/null 2>&1 || return 0
  if kubectl -n "$namespace" get \
    serverstransport.traefik.io/staff-control-center >/dev/null 2>&1; then
    fail 'obsolete local frontend ServersTransport is still present'
  fi
  kubectl -n "$namespace" get \
    serverstransport.traefik.io/control-api-gateway -o json | jq -e '
      .spec.serverName == "control-api-gateway.kodex-system.svc" and
      .spec.insecureSkipVerify == false and
      .spec.rootCAs == [{secret:"control-api-gateway-public-tls-material"}] and
      ((.spec.rootCAsSecrets // []) | length) == 0
    ' >/dev/null || fail 'local Control API ServersTransport readback failed'
  kubectl -n "$namespace" get service/control-api-gateway -o json | jq -e '
    .metadata.annotations["traefik.ingress.kubernetes.io/service.serverstransport"] ==
      "kodex-system-control-api-gateway@kubernetescrd"
  ' >/dev/null || fail 'local Control API Service transport readback failed'
  kubectl -n "$namespace" get ingress/staff-control-center-api -o json | jq -e \
    --arg tls_mode "$tls_mode" '
    .spec.rules[0].http.paths == [{
      path:"/api/v1",pathType:"Prefix",
      backend:{service:{name:"control-api-gateway",port:{name:"https"}}}
    }] and
    (if $tls_mode == "public-acme" then
      .metadata.annotations["traefik.ingress.kubernetes.io/router.middlewares"] ==
        "kodex-system-oauth2-control-center-auth@kubernetescrd"
    else
      (.metadata.annotations["traefik.ingress.kubernetes.io/router.middlewares"] // "") == ""
    end)
  ' >/dev/null || fail 'local Control API direct Ingress readback failed'
  kubectl -n "$namespace" get ingress/staff-control-center -o json | jq -e \
    --arg tls_mode "$tls_mode" '
    .metadata.annotations["traefik.ingress.kubernetes.io/router.middlewares"] ==
      (if $tls_mode == "public-acme" then
        "kodex-system-oauth2-control-center-chain@kubernetescrd,kodex-system-staff-control-center-retry@kubernetescrd"
      else "kodex-system-staff-control-center-retry@kubernetescrd" end)
  ' >/dev/null || fail 'local frontend middleware Ingress readback failed'
  kubectl -n "$namespace" get middleware.traefik.io/staff-control-center-retry -o json | jq -e '
    .spec.retry == {attempts:4,initialInterval:"100ms"}
  ' >/dev/null || fail 'local frontend retry Middleware readback failed'
}

apply_image_admission_crd() {
  local output="$temporary_directory/image-admission-crd.yaml"
  yq 'select(.kind == "CustomResourceDefinition" and
    .metadata.name == "imageadmissionpolicyparameters.supplychain.kodex.dev")' \
    "$render" >"$output"
  [[ -s "$output" ]] || fail 'image admission policy CRD is absent from local render'
  kubectl apply --server-side --force-conflicts --field-manager=kodex-local-dev \
    -f "$output" >/dev/null
  kubectl wait --for=condition=Established \
    customresourcedefinition/imageadmissionpolicyparameters.supplychain.kodex.dev \
    --timeout=3m >/dev/null || fail 'image admission policy CRD is not Established'
}

cleanup_local_image_admission_runs() {
  local selector inventory
  selector='app.kubernetes.io/name=kodex-image-admission,kodex.dev/image-admission-orchestrated=true'
  inventory=$(kubectl -n "$namespace" get jobs,persistentvolumeclaims \
    -l "$selector" -o json) ||
    fail 'local image admission inventory is unavailable for revision cleanup'
  jq -e --arg namespace "$namespace" '
    all(.items[];
      .metadata.namespace == $namespace and
      .metadata.labels["app.kubernetes.io/name"] == "kodex-image-admission" and
      .metadata.labels["kodex.dev/image-admission-orchestrated"] == "true" and
      (if .kind == "Job" then
        (.metadata.name | test(
          "^mc-admit-[a-f0-9]{32}-(claim|scan|sign|admit|promote)$"))
       elif .kind == "PersistentVolumeClaim" then
        (.metadata.name | test("^mc-admit-[a-f0-9]{32}$"))
       else false end))
  ' <<<"$inventory" >/dev/null ||
    fail 'local image admission inventory contains an unmanaged resource'
  kubectl -n "$namespace" delete jobs,persistentvolumeclaims -l "$selector" \
    --ignore-not-found --wait=true --timeout=3m >/dev/null
}

pause_local_image_admission_controller() {
  local controller replicas
  controller=$(kubectl -n "$namespace" get \
    deployment/image-admission-controller -o json 2>/dev/null || true)
  [[ -n "$controller" ]] || return 0
  jq -e --arg namespace "$namespace" '
    .metadata.namespace == $namespace and
    .metadata.name == "image-admission-controller" and
    .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
    .metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
    ((.spec.replicas // 0) == 0 or (.spec.replicas // 0) == 1)
  ' <<<"$controller" >/dev/null ||
    fail 'image admission controller is not owned by the local Kodex profile'
  replicas=$(jq -er '.spec.replicas // 0' <<<"$controller")
  ((replicas > 0)) || return 0
  kubectl -n "$namespace" scale deployment/image-admission-controller \
    --replicas=0 >/dev/null || fail 'local image admission controller cannot be paused'
  image_admission_controller_restore_replicas=$replicas
  for attempt in $(seq 1 180); do
    controller=$(kubectl -n "$namespace" get \
      deployment/image-admission-controller -o json) ||
      fail 'paused local image admission controller is unavailable'
    if jq -e '
      (.spec.replicas // 0) == 0 and
      (.status.replicas // 0) == 0 and
      (.status.availableReplicas // 0) == 0
    ' <<<"$controller" >/dev/null; then
      return 0
    fi
    ((attempt < 180)) || fail 'local image admission controller did not stop'
    sleep 1
  done
}

reconcile_local_immutable_image_admission_policy() {
  local desired current desired_digest current_digest
  desired=$(yq -o=json -I=0 '
    select(.kind == "ConfigMap" and .metadata.namespace == "kodex-system" and
      .metadata.name == "kodex-image-admission-policy")
  ' "$render")
  [[ -n "$desired" && "$desired" != null ]] ||
    fail 'local immutable image admission ConfigMap is absent'
  current=$(kubectl -n "$namespace" get \
    configmap/kodex-image-admission-policy -o json 2>/dev/null || true)
  if [[ -n "$current" ]]; then
    desired_digest=$(jq -Sc '{immutable,data,annotations:.metadata.annotations}' \
      <<<"$desired" | sha256sum | awk '{print $1}')
    current_digest=$(jq -Sc '{immutable,data,annotations:.metadata.annotations}' \
      <<<"$current" | sha256sum | awk '{print $1}')
    if [[ "$current_digest" != "$desired_digest" ]]; then
      jq -e '
        .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
        .metadata.labels["kodex.dev/local-profile"] == "hot-reload"
      ' <<<"$current" >/dev/null ||
        fail 'immutable image admission ConfigMap is not owned by the local Kodex profile'
      kubectl -n "$namespace" delete configmap/kodex-image-admission-policy \
        --wait=true --timeout=2m >/dev/null
      cleanup_local_image_admission_runs
    fi
  fi

  desired=$(yq -o=json -I=0 '
    select(.apiVersion == "supplychain.kodex.dev/v1alpha1" and
      .kind == "ImageAdmissionPolicyParameters" and
      .metadata.namespace == "kodex-system" and
      .metadata.name == "kodex-image-admission-policy")
  ' "$render")
  [[ -n "$desired" && "$desired" != null ]] ||
    fail 'local ImageAdmissionPolicyParameters is absent'
  current=$(kubectl -n "$namespace" get \
    imageadmissionpolicyparameters/kodex-image-admission-policy -o json 2>/dev/null || true)
  [[ -n "$current" ]] || return 0
  desired_digest=$(jq -Sc '.spec' <<<"$desired" | sha256sum | awk '{print $1}')
  current_digest=$(jq -Sc '.spec' <<<"$current" | sha256sum | awk '{print $1}')
  [[ "$current_digest" != "$desired_digest" ]] || return 0
  jq -e '
    .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
    .metadata.labels["kodex.dev/local-profile"] == "hot-reload"
  ' <<<"$current" >/dev/null ||
    fail 'immutable image admission policy is not owned by the local Kodex profile'
  kubectl -n "$namespace" delete \
    imageadmissionpolicyparameters/kodex-image-admission-policy \
    --wait=true --timeout=2m >/dev/null
}

reconcile_local_mutable_configmaps() {
  local name current
  while IFS= read -r name; do
    [[ -n "$name" ]] || continue
    current=$(kubectl -n "$namespace" get "configmap/$name" -o json 2>/dev/null || true)
    [[ -n "$current" ]] || continue
    jq -e '.immutable == true' <<<"$current" >/dev/null 2>&1 || continue
    jq -e '
      .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
      .metadata.labels["kodex.dev/local-profile"] == "hot-reload"
    ' <<<"$current" >/dev/null ||
      fail "immutable ConfigMap is not owned by the local Kodex profile: $name"
    kubectl -n "$namespace" delete "configmap/$name" --wait=true --timeout=2m >/dev/null
  done < <(yq -N -r '
    select(.kind == "ConfigMap" and .metadata.namespace == "kodex-system" and
      .immutable != true and
      .metadata.labels."app.kubernetes.io/part-of" == "kodex" and
      .metadata.labels."kodex.dev/local-profile" == "hot-reload") |
    .metadata.name
  ' "$render" | sort -u)
}

reconcile_local_statefulset_rollout() {
  local workload state current_revision update_revision pod
  for workload in "$@"; do
    state=$(kubectl -n "$namespace" get "statefulset/$workload" -o json)
    current_revision=$(jq -r '.status.currentRevision // ""' <<<"$state")
    update_revision=$(jq -r '.status.updateRevision // ""' <<<"$state")
    [[ -n "$current_revision" && -n "$update_revision" &&
      "$current_revision" != "$update_revision" ]] || continue
    while IFS= read -r pod; do
      [[ -n "$pod" ]] || continue
      kubectl -n "$namespace" delete "pod/$pod" --wait=true --timeout=3m >/dev/null
    done < <(kubectl -n "$namespace" get pods -o json | jq -r --arg workload "$workload" '
      .items[] |
      select(any(.metadata.ownerReferences[]?;
        .kind == "StatefulSet" and .name == $workload)) |
      .metadata.name
    ')
  done
}

wait_job() {
  local name=$1 deadline=$((SECONDS + 900)) state
  while ((SECONDS < deadline)); do
    state=$(kubectl -n "$namespace" get "job/$name" -o json 2>/dev/null || true)
    if jq -e 'any(.status.conditions[]?; .type == "Complete" and .status == "True")' \
      <<<"$state" >/dev/null 2>&1; then
      return
    fi
    if jq -e 'any(.status.conditions[]?; .type == "Failed" and .status == "True")' \
      <<<"$state" >/dev/null 2>&1; then
      kubectl -n "$namespace" logs "job/$name" --all-containers --tail=200 >&2 || true
      fail "local Job failed: $name"
    fi
    sleep 2
  done
  kubectl -n "$namespace" logs "job/$name" --all-containers --tail=200 >&2 || true
  fail "local Job timed out: $name"
}

apply_job() {
  local name=$1 output
  output="$temporary_directory/job-$name.yaml"
  JOB_NAME="$name" yq 'select(.kind == "Job" and .metadata.name == strenv(JOB_NAME))' \
    "$render" >"$output"
  [[ -s "$output" ]] || fail "local Job is absent: $name"
  kubectl -n "$namespace" delete "job/$name" --ignore-not-found --wait=true --timeout=3m >/dev/null
  kubectl apply --server-side --force-conflicts --field-manager=kodex-local-dev -f "$output" >/dev/null
  wait_job "$name"
}

wait_certificates() {
  local name
  while IFS= read -r name; do
    [[ -n "$name" ]] || continue
    kubectl -n "$namespace" wait --for=condition=Ready "certificate/$name" --timeout=5m >/dev/null ||
      fail "local Certificate is not ready: $name"
  done < <(yq -N -r 'select(.kind == "Certificate" and .metadata.namespace == "kodex-system") | .metadata.name' "$render" | sort -u)
  while IFS= read -r name; do
    [[ -n "$name" ]] || continue
    kubectl wait --for=condition=Synced "bundle/$name" --timeout=5m >/dev/null ||
      fail "local trust Bundle is not synced: $name"
  done < <(yq -N -r 'select(.kind == "Bundle") | .metadata.name' "$render" | sort -u)
}

ensure_seed_secrets() {
  local name output
  while IFS= read -r name; do
    [[ -n "$name" ]] || continue
    kubectl -n "$namespace" get "secret/$name" >/dev/null 2>&1 && continue
    output="$temporary_directory/secret-$name.yaml"
    SECRET_NAME="$name" yq 'select(.kind == "Secret" and .metadata.name == strenv(SECRET_NAME))' \
      "$render" >"$output"
    kubectl create --field-manager=kodex-local-dev -f "$output" >/dev/null
  done < <(yq -N -r 'select(.kind == "Secret") | .metadata.name' "$render" | sort -u)
}

readback_local_object_storage_secret() {
  local state
  [[ -n "$object_storage_secret_name" ]] || fail 'local object storage Secret name is absent'
  state=$(kubectl -n "$namespace" get "secret/$object_storage_secret_name" -o json 2>/dev/null) ||
    fail 'local object storage Secret is absent'
  jq -e '
    .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
    .metadata.labels["app.kubernetes.io/name"] == "seaweedfs" and
    .metadata.labels["app.kubernetes.io/component"] == "object-storage" and
    .metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
    .immutable == true and
    ((.data | keys | sort) ==
      (["access-key","bucket","endpoint","region","s3.json","secret-key"] | sort)) and
    ((.data.endpoint | @base64d) ==
      "http://seaweedfs-s3.kodex-system.svc.cluster.local:8333") and
    ((.data.region | @base64d) == "us-east-1") and
    ((.data.bucket | @base64d) == "kodex-artifacts") and
    ((.data["access-key"] | @base64d) | length == 32 and test("^[a-f0-9]+$")) and
    ((.data["secret-key"] | @base64d) | length == 64 and test("^[a-f0-9]+$")) and
    ((.data["s3.json"] | @base64d | fromjson) as $config |
      ($config.identities | length) == 1 and
      $config.identities[0].name == "control-plane" and
      ($config.identities[0].credentials | length) == 1 and
      $config.identities[0].credentials[0].accessKey ==
        (.data["access-key"] | @base64d) and
      $config.identities[0].credentials[0].secretKey ==
        (.data["secret-key"] | @base64d) and
      ($config.identities[0].actions | sort) ==
        (["Admin","List","Read","Tagging","Write"] | sort))
  ' <<<"$state" >/dev/null || fail 'local object storage Secret readback failed'
}

discover_local_object_storage_secret() {
  local state
  state=$(kubectl -n "$namespace" get secrets \
    -l 'kodex.dev/local-credential=object-storage' -o json) ||
    fail 'local object storage Secret discovery failed'
  object_storage_secret_name=$(jq -er '
    [.items[] | select(
      .immutable == true and
      .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
      .metadata.labels["kodex.dev/local-profile"] == "hot-reload"
    )] |
    if length == 1 then .[0].metadata.name
    elif length == 0 then error("local object storage Secret is absent")
    else error("multiple local object storage Secrets are present") end
  ' <<<"$state") || fail 'local object storage Secret discovery is ambiguous'
  readback_local_object_storage_secret
}

readback_session_archive_worker_secret() {
  local source runtime
  [[ -n "$object_storage_secret_name" ]] || fail 'session archive object storage Secret name is absent'
  source=$(kubectl -n "$namespace" get "secret/$object_storage_secret_name" -o json) ||
    fail 'session archive source object storage Secret is absent'
  runtime=$(kubectl -n "$runtime_namespace" get "secret/$object_storage_secret_name" -o json) ||
    fail 'session archive runtime object storage Secret is absent'
  jq -e --arg name "$object_storage_secret_name" --argjson source "$source" '
    .metadata.name == $name and .metadata.namespace == "kodex-runtime" and
    .metadata.labels["app.kubernetes.io/name"] == "session-archive" and
    .metadata.labels["app.kubernetes.io/component"] == "archive-worker" and
    .metadata.labels["kodex.dev/local-credential"] == "session-archive-object-storage" and
    .metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
    .immutable == true and
    (.data | keys | sort) == (["access-key", "secret-key"] | sort) and
    .data["access-key"] == $source.data["access-key"] and
    .data["secret-key"] == $source.data["secret-key"]
  ' <<<"$runtime" >/dev/null || fail 'session archive runtime object storage Secret readback failed'
}

readback_session_archive() {
  local deployment expected_image endpoint_slices target_registry resource
  expected_image=$(yq -N -r '
    select(.kind == "Deployment" and .metadata.name == "session-archive") |
    .spec.template.spec.containers[] |
    select(.name == "session-archive") |
    .env[] |
    select(.name == "SESSION_ARCHIVE_WORKER_IMAGE") |
    .value
  ' "$render")
  [[ "$expected_image" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] ||
    fail 'rendered session archive worker image is invalid'

  deployment=$(kubectl -n "$namespace" get deployment/session-archive -o json) ||
    fail 'session archive Deployment is absent'
  jq -e --arg image "$expected_image" '
    .metadata.namespace == "kodex-system" and
    .spec.replicas == 1 and .status.readyReplicas == 1 and
    .spec.template.spec.serviceAccountName == "session-archive" and
    ([.spec.template.spec.containers[].name] | sort) ==
      (["internal-rpc-authority-issuer", "platform-worker-grant-agent", "session-archive"] | sort) and
    any(.spec.template.spec.containers[];
      .name == "session-archive" and
      .image == $image and .imagePullPolicy == "IfNotPresent" and
      any(.env[];
        .name == "SESSION_ARCHIVE_WORKER_IMAGE" and .value == $image))
  ' <<<"$deployment" >/dev/null || fail 'session archive Deployment readback failed'

  kubectl -n "$namespace" get configmap/session-archive-runtime -o json | jq -e '
    .metadata.namespace == "kodex-system" and
    .data.SESSION_ARCHIVE_WORKER_NAMESPACE == "kodex-runtime"
  ' >/dev/null || fail 'session archive exact worker namespace readback failed'

  for resource in serviceaccount/session-archive \
    networkpolicy/session-archive-default-deny \
    networkpolicy/session-archive-exact-paths \
    networkpolicy/session-archive-internal-rpc-authority-exact-paths; do
    kubectl -n "$namespace" get "$resource" >/dev/null 2>&1 ||
      fail "session archive controller resource is absent: $resource"
  done
  for resource in serviceaccount/session-archive-worker \
    role/session-archive-controller role/session-archive-worker \
    rolebinding/session-archive-controller rolebinding/session-archive-worker \
    networkpolicy/session-archive-worker-default-deny \
    networkpolicy/session-archive-worker-object-storage; do
    kubectl -n "$runtime_namespace" get "$resource" >/dev/null 2>&1 ||
      fail "session archive worker resource is absent: $resource"
  done
  readback_session_archive_worker_secret

  kubectl -n "$runtime_namespace" get role/session-archive-controller -o json | jq -e '
    ([.rules[] | {apiGroups:(.apiGroups | sort), resources:(.resources | sort), verbs:(.verbs | sort)}] | sort_by(.resources[0])) ==
    ([
      {apiGroups:[""], resources:["configmaps"], verbs:["create","delete","deletecollection"]},
      {apiGroups:[""], resources:["persistentvolumeclaims"], verbs:["create","delete","get"]},
      {apiGroups:[""], resources:["pods"], verbs:["list"]},
      {apiGroups:["batch"], resources:["jobs"], verbs:["create","delete","deletecollection","get","list"]}
    ] | sort_by(.resources[0]))
  ' >/dev/null || fail 'session archive controller runtime Role is broader than required'
  kubectl -n "$runtime_namespace" get role/session-archive-worker -o json | jq -e '
    (.rules | length) == 0
  ' >/dev/null || fail 'session archive worker received Kubernetes API permissions'
  kubectl -n "$runtime_namespace" get serviceaccount/session-archive-worker -o json | jq -e '
    .automountServiceAccountToken == false
  ' >/dev/null || fail 'session archive worker ServiceAccount token automount is enabled'
  kubectl -n "$runtime_namespace" get rolebinding/session-archive-controller -o json | jq -e '
    (.subjects | length) == 1 and .subjects[0].kind == "ServiceAccount" and
    .subjects[0].name == "session-archive" and .subjects[0].namespace == "kodex-system" and
    .roleRef.kind == "Role" and .roleRef.name == "session-archive-controller"
  ' >/dev/null || fail 'session archive controller cross-namespace RoleBinding is invalid'
  kubectl -n "$runtime_namespace" get rolebinding/session-archive-worker -o json | jq -e '
    (.subjects | length) == 1 and .subjects[0].kind == "ServiceAccount" and
    .subjects[0].name == "session-archive-worker" and .subjects[0].namespace == "kodex-runtime" and
    .roleRef.kind == "Role" and .roleRef.name == "session-archive-worker"
  ' >/dev/null || fail 'session archive worker RoleBinding is invalid'
  kubectl -n "$runtime_namespace" get networkpolicy/session-archive-worker-default-deny -o json | jq -e '
    .spec.podSelector.matchLabels["session-archive.kodex.dev/managed"] == "true" and
    (.spec.policyTypes | sort) == (["Egress","Ingress"] | sort) and
    ((.spec.ingress // []) | length) == 0 and ((.spec.egress // []) | length) == 0
  ' >/dev/null || fail 'session archive worker default-deny policy is invalid'
  kubectl -n "$runtime_namespace" get networkpolicy/session-archive-worker-object-storage -o json | jq -e '
    .spec.podSelector.matchLabels["session-archive.kodex.dev/managed"] == "true" and
    (.spec.policyTypes | sort) == (["Egress","Ingress"] | sort) and
    ((.spec.ingress // []) | length) == 0 and (.spec.egress | length) == 2 and
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
    )] | length) == 1
  ' >/dev/null || fail 'session archive worker object-storage policy is invalid'

  kubectl auth can-i create jobs.batch -n "$runtime_namespace" \
    --as=system:serviceaccount:kodex-system:session-archive | grep -qx yes ||
    fail 'session archive controller cannot create runtime worker Jobs'
  if kubectl auth can-i create jobs.batch -n "$namespace" \
    --as=system:serviceaccount:kodex-system:session-archive | grep -qx yes; then
    fail 'session archive controller can create worker Jobs in kodex-system'
  fi
  if kubectl auth can-i list pods -n "$runtime_namespace" \
    --as=system:serviceaccount:kodex-runtime:session-archive-worker | grep -qx yes; then
    fail 'session archive worker can access the Kubernetes API'
  fi

  for resource in serviceaccount/session-archive-worker role/session-archive-controller \
    role/session-archive-worker rolebinding/session-archive-controller \
    rolebinding/session-archive-worker networkpolicy/session-archive-worker-default-deny \
    networkpolicy/session-archive-worker-object-storage; do
    if kubectl -n "$namespace" get "$resource" >/dev/null 2>&1; then
      fail "session archive worker resource leaked into kodex-system: $resource"
    fi
  done
  for resource in jobs configmaps; do
    kubectl -n "$namespace" get "$resource" \
      -l 'session-archive.kodex.dev/managed=true' -o json | jq -e '.items | length == 0' >/dev/null ||
      fail "session archive managed $resource leaked into kodex-system"
  done
  kubectl -n "$namespace" get persistentvolumeclaims -o json | jq -e '
    [.items[] | select(.metadata.name | startswith("runtime-session-"))] | length == 0
  ' >/dev/null || fail 'session archive session PVC leaked into kodex-system'

  target_registry=$(kubectl -n "$namespace" get \
    configmap/internal-rpc-authority-publisher-target-registry -o jsonpath='{.data.key-delivery-targets\.yaml}')
  yq -e '
    [.targets[] | select(
      .workload_id == "session-archive" and
      .service_account == "session-archive" and
      .startup_readback_required == true
    )] | length == 1
  ' <<<"$target_registry" >/dev/null || fail 'session archive authority target readback failed'

  endpoint_slices=$(kubectl -n "$namespace" get endpointslice \
    -l kubernetes.io/service-name=session-archive -o json)
  jq -e '
    any(.items[];
      any(.ports[]?; .name == "metrics" and .protocol == "TCP" and .port == 9090) and
      any(.endpoints[]?; .conditions.ready == true and (.addresses | length) > 0)
    )
  ' <<<"$endpoint_slices" >/dev/null || fail 'session archive EndpointSlice readback failed'
}

ensure_local_object_storage_secret() {
  local secret_directory="$temporary_directory/object-storage-secret" state manifest digest current_digest
  state=$(kubectl -n "$namespace" get secrets \
    -l 'kodex.dev/local-credential=object-storage' -o json | jq -c '
      [.items[] | select(
        .immutable == true and
        ((.data["access-key"] | @base64d) | length == 32 and test("^[a-f0-9]+$")) and
        ((.data["secret-key"] | @base64d) | length == 64 and test("^[a-f0-9]+$"))
      )] |
      if length > 1 then error("multiple local object storage credentials")
      elif length == 1 then .[0] else empty end
    ')
  if [[ -z "$state" ]]; then
    state=$(kubectl -n "$namespace" get secret/kodex-external-s3 -o json 2>/dev/null || true)
    if [[ -n "$state" ]] && ! jq -e '
      .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
      .metadata.labels["kodex.dev/local-profile"] == "hot-reload"
    ' <<<"$state" >/dev/null; then
      fail 'legacy object storage Secret is not owned by the local Kodex profile'
    fi
    if [[ -z "$state" ]] || ! jq -e '
      ((.data["access-key"] | @base64d) | length == 32 and test("^[a-f0-9]+$")) and
      ((.data["secret-key"] | @base64d) | length == 64 and test("^[a-f0-9]+$"))
    ' <<<"$state" >/dev/null 2>&1; then
      install -d -m 0700 "$secret_directory"
      printf '%s' "$(openssl rand -hex 16)" >"$secret_directory/access-key"
      printf '%s' "$(openssl rand -hex 32)" >"$secret_directory/secret-key"
      printf '%s' 'http://seaweedfs-s3.kodex-system.svc.cluster.local:8333' >"$secret_directory/endpoint"
      printf '%s' 'us-east-1' >"$secret_directory/region"
      printf '%s' 'kodex-artifacts' >"$secret_directory/bucket"
      jq -n --rawfile access_key "$secret_directory/access-key" \
        --rawfile secret_key "$secret_directory/secret-key" '
          {identities:[{name:"control-plane",credentials:[{
            accessKey:$access_key,secretKey:$secret_key
          }],actions:["Admin","Read","List","Tagging","Write"]}]}
        ' >"$secret_directory/s3.json"
      chmod 0600 "$secret_directory"/*
      state=$(kubectl -n "$namespace" create secret generic object-storage-candidate \
        --from-file=access-key="$secret_directory/access-key" \
        --from-file=secret-key="$secret_directory/secret-key" \
        --from-file=endpoint="$secret_directory/endpoint" \
        --from-file=region="$secret_directory/region" \
        --from-file=bucket="$secret_directory/bucket" \
        --from-file=s3.json="$secret_directory/s3.json" \
        --dry-run=client -o json)
    fi
    digest=$(jq -Sc '.data' <<<"$state" | sha256sum | awk '{print $1}')
    object_storage_secret_name="kodex-external-s3-local-${digest:0:16}"
    manifest=$(jq --arg name "$object_storage_secret_name" '
      .metadata = {name:$name,namespace:"kodex-system",labels:{
        "app.kubernetes.io/part-of":"kodex",
        "app.kubernetes.io/name":"seaweedfs",
        "app.kubernetes.io/component":"object-storage",
        "app.kubernetes.io/managed-by":"tools-dev",
        "kodex.dev/local-profile":"hot-reload",
        "kodex.dev/local-credential":"object-storage"
      }} | .immutable = true | del(.status)
    ' <<<"$state")
    if kubectl -n "$namespace" get "secret/$object_storage_secret_name" >/dev/null 2>&1; then
      current_digest=$(kubectl -n "$namespace" get "secret/$object_storage_secret_name" -o json |
        jq -Sc '.data' | sha256sum | awk '{print $1}')
      [[ "$current_digest" == "$digest" ]] || fail 'content-addressed object storage Secret differs'
    else
      kubectl create --field-manager=kodex-local-dev -f - <<<"$manifest" >/dev/null
    fi
  else
    object_storage_secret_name=$(jq -r '.metadata.name' <<<"$state")
  fi
  OBJECT_STORAGE_SECRET_NAME="$object_storage_secret_name" yq -i '
    (.. | select(tag == "!!str")) |=
      sub("^kodex-external-s3$"; strenv(OBJECT_STORAGE_SECRET_NAME))
  ' "$render"
  readback_local_object_storage_secret
}

ensure_session_archive_worker_secret() {
  local source manifest current source_digest current_digest stale
  source=$(kubectl -n "$namespace" get "secret/$object_storage_secret_name" -o json) ||
    fail 'session archive source object storage Secret is absent'
  manifest=$(jq --arg name "$object_storage_secret_name" '
    {
      apiVersion:"v1",
      kind:"Secret",
      metadata:{
        name:$name,
        namespace:"kodex-runtime",
        labels:{
          "app.kubernetes.io/part-of":"kodex",
          "app.kubernetes.io/name":"session-archive",
          "app.kubernetes.io/component":"archive-worker",
          "app.kubernetes.io/managed-by":"tools-dev",
          "kodex.dev/local-profile":"hot-reload",
          "kodex.dev/local-credential":"session-archive-object-storage"
        }
      },
      immutable:true,
      type:"Opaque",
      data:{
        "access-key":.data["access-key"],
        "secret-key":.data["secret-key"]
      }
    }
  ' <<<"$source")
  source_digest=$(jq -Sc '.data' <<<"$manifest" | sha256sum | awk '{print $1}')
  current=$(kubectl -n "$runtime_namespace" get "secret/$object_storage_secret_name" -o json 2>/dev/null || true)
  if [[ -n "$current" ]]; then
    current_digest=$(jq -Sc '.data' <<<"$current" | sha256sum | awk '{print $1}')
    [[ "$current_digest" == "$source_digest" ]] ||
      fail 'content-addressed session archive runtime Secret differs'
  else
    kubectl create --field-manager=kodex-local-dev -f - <<<"$manifest" >/dev/null
  fi
  while IFS= read -r stale; do
    [[ -n "$stale" && "$stale" != "$object_storage_secret_name" ]] || continue
    kubectl -n "$runtime_namespace" delete "secret/$stale" --wait=true --timeout=2m >/dev/null
  done < <(kubectl -n "$runtime_namespace" get secrets \
    -l 'kodex.dev/local-credential=session-archive-object-storage' \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
  readback_session_archive_worker_secret
}

cleanup_legacy_session_archive_worker_resources() {
  kubectl -n "$namespace" delete --ignore-not-found \
    serviceaccount/session-archive-worker \
    role/session-archive-controller role/session-archive-worker \
    rolebinding/session-archive-controller rolebinding/session-archive-worker \
    networkpolicy/session-archive-worker-default-deny \
    networkpolicy/session-archive-worker-object-storage >/dev/null
}

write_local_backup_controller_credentials() {
  local output=$1
  local s3_state="$temporary_directory/backup-controller-s3.json"
  local postgresql_state="$temporary_directory/backup-controller-postgresql.json"

  kubectl -n "$namespace" get "secret/$object_storage_secret_name" -o json >"$s3_state" ||
    fail 'local object storage Secret is unavailable for backup-controller'
  kubectl -n "$namespace" get secret/kodex-postgresql-runtime-credentials -o json \
    >"$postgresql_state" ||
    fail 'local PostgreSQL credentials are unavailable for backup-controller'
  chmod 0600 "$s3_state" "$postgresql_state"

  jq -n --slurpfile s3 "$s3_state" --slurpfile postgresql "$postgresql_state" '
    def secret($document; $key):
      ($document.data[$key] // error("required Secret key is absent")) |
      @base64d | rtrimstr("\n");
    ($s3[0]) as $storage |
    ($postgresql[0]) as $database |
    (secret($storage; "endpoint")) as $endpoint |
    (secret($storage; "region")) as $region |
    (secret($storage; "access-key")) as $accessKey |
    (secret($storage; "secret-key")) as $secretKey |
    (secret($storage; "bucket")) as $artifactBucket |
    if $endpoint != "http://seaweedfs-s3.kodex-system.svc.cluster.local:8333" or
      $region != "us-east-1" or $artifactBucket != "kodex-artifacts" or
      ($accessKey | length) == 0 or ($secretKey | length) == 0
    then error("local object storage contract is invalid")
    else {
      schemaVersion: 1,
      destination: {
        name: "backup-repository",
        endpoint: $endpoint,
        region: $region,
        bucket: "kodex-backups",
        accessKeyId: $accessKey,
        secretAccessKey: $secretKey,
        usePathStyle: true,
        allowInsecureLocal: true
      },
      databases: [
        {
          name: "control-plane",
          host: "kodex-postgresql.kodex-system.svc.cluster.local",
          port: 5432,
          database: "control_plane",
          user: "kodex_backup_reader",
          password: secret($database; "kodex_backup_reader"),
          tlsMode: "verify-full",
          tlsServerName: "kodex-postgresql.kodex-system.svc.cluster.local",
          caFile: "/var/run/secrets/kodex/backup-controller/tls/ca.pem",
          schemaKind: "goose"
        },
        {
          name: "internal-rpc-authority",
          host: "kodex-postgresql.kodex-system.svc.cluster.local",
          port: 5432,
          database: "internal_rpc_authority",
          user: "kodex_backup_reader",
          password: secret($database; "kodex_backup_reader"),
          tlsMode: "verify-full",
          tlsServerName: "kodex-postgresql.kodex-system.svc.cluster.local",
          caFile: "/var/run/secrets/kodex/backup-controller/tls/ca.pem",
          schemaKind: "goose"
        }
      ],
      objectStores: [
        {
          name: "artifacts",
          endpoint: $endpoint,
          region: $region,
          bucket: $artifactBucket,
          prefix: "organizations",
          accessKeyId: $accessKey,
          secretAccessKey: $secretKey,
          usePathStyle: true,
          allowInsecureLocal: true
        },
        {
          name: "session-archives",
          endpoint: $endpoint,
          region: $region,
          bucket: "kodex-session-archives",
          prefix: "session-archive/v1",
          accessKeyId: $accessKey,
          secretAccessKey: $secretKey,
          usePathStyle: true,
          allowInsecureLocal: true
        }
      ]
    } end
  ' >"$output" || fail 'build local backup-controller credentials'
  chmod 0600 "$output"
}

readback_local_backup_controller_secret() {
  local expected=$1 state actual expected_digest actual_digest
  state=$(kubectl -n "$namespace" get secret/backup-controller-credentials -o json 2>/dev/null) ||
    fail 'local backup-controller credentials Secret is absent'
  jq -e '
    .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
    .metadata.labels["app.kubernetes.io/name"] == "backup-controller" and
    .metadata.labels["app.kubernetes.io/managed-by"] == "tools-dev" and
    .metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
    ((.data | keys) == ["credentials.json"])
  ' <<<"$state" >/dev/null || fail 'local backup-controller Secret metadata is invalid'
  actual="$temporary_directory/backup-controller-credentials-readback.json"
  jq -er '.data["credentials.json"] | @base64d' <<<"$state" >"$actual" ||
    fail 'local backup-controller Secret payload is unavailable'
  chmod 0600 "$actual"
  expected_digest=$(jq -Sc '.' "$expected" | sha256sum | awk '{print $1}')
  actual_digest=$(jq -Sc '.' "$actual" | sha256sum | awk '{print $1}')
  [[ "$actual_digest" == "$expected_digest" ]] ||
    fail 'local backup-controller Secret content readback failed'
}

ensure_local_backup_controller_secret() {
  local credentials="$temporary_directory/backup-controller-credentials.json" credentials_digest
  write_local_backup_controller_credentials "$credentials"
  credentials_digest=$(jq -Sc '.' "$credentials" | sha256sum | awk '{print $1}')
  [[ "$credentials_digest" =~ ^[a-f0-9]{64}$ ]] ||
    fail 'local backup-controller credentials digest is invalid'
  BACKUP_CONTROLLER_CREDENTIALS_DIGEST="$credentials_digest" yq -i '
    with(select(.kind == "Deployment" and .metadata.name == "backup-controller");
      .spec.template.metadata.annotations["kodex.dev/backup-credentials-sha256"] =
        strenv(BACKUP_CONTROLLER_CREDENTIALS_DIGEST)
    )
  ' "$render"
  kubectl -n "$namespace" create secret generic backup-controller-credentials \
    --from-file=credentials.json="$credentials" \
    --dry-run=client -o yaml |
    yq '
      .metadata.labels = {
        "app.kubernetes.io/part-of":"kodex",
        "app.kubernetes.io/name":"backup-controller",
        "app.kubernetes.io/component":"backup-job",
        "app.kubernetes.io/managed-by":"tools-dev",
        "kodex.dev/local-profile":"hot-reload"
      }
    ' |
    kubectl apply --server-side --force-conflicts --field-manager=kodex-local-dev -f - >/dev/null
  readback_local_backup_controller_secret "$credentials"
}

verify_local_backup_controller() {
  local deadline=$((SECONDS + 900)) status
  while ((SECONDS < deadline)); do
    status=$(kubectl -n "$namespace" exec deployment/backup-controller \
      -c backup-controller -- wget -qO- http://127.0.0.1:9090/status 2>/dev/null || true)
    if jq -e '
      .state == "idle" and
      (.lastVerifiedBackup | type == "string" and length > 0) and
      (.lastSuccessAt | type == "string" and length > 0)
    ' <<<"$status" >/dev/null 2>&1; then
      return
    fi
    sleep 3
  done
  kubectl -n "$namespace" logs deployment/backup-controller \
    -c backup-controller --tail=200 >&2 || true
  fail 'local backup-controller did not produce a verified backup'
}

wait_warm_runtime() {
  local pod=system-assistant-warm deadline=$((SECONDS + 300))
  while ((SECONDS < deadline)); do
    kubectl -n "$runtime_namespace" get "pod/$pod" >/dev/null 2>&1 && break
    sleep 2
  done
  kubectl -n "$runtime_namespace" get "pod/$pod" >/dev/null 2>&1 ||
    fail 'local warm runtime Pod was not materialized'
  if kubectl -n "$runtime_namespace" wait --for=condition=Ready "pod/$pod" --timeout=5m >/dev/null; then
    return
  fi
  kubectl -n "$runtime_namespace" get "pod/$pod" -o wide >&2 || true
  kubectl -n "$runtime_namespace" describe "pod/$pod" >&2 || true
  while IFS= read -r container; do
    [[ -n "$container" ]] || continue
    printf '%s\n' "--- $pod/$container ---" >&2
    kubectl -n "$runtime_namespace" logs "pod/$pod" -c "$container" --tail=200 >&2 || true
  done < <(kubectl -n "$runtime_namespace" get "pod/$pod" \
    -o jsonpath='{range .spec.initContainers[*]}{.name}{"\n"}{end}{range .spec.containers[*]}{.name}{"\n"}{end}')
  fail 'local warm runtime Pod is unavailable'
}

wait_stable_workloads() {
	local deadline=$((SECONDS + 300)) stable_since=0 snapshot
	while ((SECONDS < deadline)); do
		snapshot=$(kubectl -n "$namespace" get pods,replicasets,statefulsets -o json)
		if jq -e '
			([.items[] |
				select(.kind == "ReplicaSet" and (.spec.replicas // 0) > 0) |
				.metadata.name]) as $activeReplicaSets |
			([.items[] |
				select(.kind == "StatefulSet" and (.spec.replicas // 0) > 0) |
				.metadata.name]) as $activeStatefulSets |
			[.items[] |
				select(.kind == "Pod") |
				select(
					.metadata.name == "system-assistant-warm" or
					any(.metadata.ownerReferences[]?;
						(.kind == "ReplicaSet" and (.name as $name | $activeReplicaSets | index($name) != null)) or
						(.kind == "StatefulSet" and (.name as $name | $activeStatefulSets | index($name) != null)))
				)] as $workloads |
      ($workloads | length) > 0 and
      all($workloads[];
        .status.phase == "Running" and
        (.status.containerStatuses | length) > 0 and
        all(.status.containerStatuses[]; .ready == true)
      )
    ' <<<"$snapshot" >/dev/null; then
      ((stable_since > 0)) || stable_since=$SECONDS
      if ((SECONDS - stable_since >= 20)); then
        return
      fi
    else
      stable_since=0
    fi
    sleep 2
  done
  kubectl -n "$namespace" get pods -o wide >&2 || true
  fail 'local workloads did not retain a stable Ready state'
}

readback_local_image_supply_chain() {
  local expected_policy actual_policy policy_resource controller workloads expected_deployments
  local expected_digest actual_digest
  local target_registry promoted_pull_host resource name
  expected_policy=$(yq -o=json -I=0 '
    select(.kind == "ConfigMap" and .metadata.namespace == "kodex-system" and
      .metadata.name == "kodex-image-admission-policy") | .data
  ' "$render")
  [[ -n "$expected_policy" && "$expected_policy" != null ]] ||
    fail 'rendered image admission policy is absent'
  actual_policy=$(kubectl -n "$namespace" get \
    configmap/kodex-image-admission-policy -o json | jq -cS '.data') ||
    fail 'image admission policy ConfigMap is absent'
  expected_digest=$(jq -cS . <<<"$expected_policy" | sha256sum | awk '{print $1}')
  actual_digest=$(jq -cS . <<<"$actual_policy" | sha256sum | awk '{print $1}')
  [[ "$actual_digest" == "$expected_digest" ]] ||
    fail 'image admission policy ConfigMap readback mismatch'

  policy_resource=$(kubectl -n "$namespace" get \
    imageadmissionpolicyparameters/kodex-image-admission-policy -o json) ||
    fail 'ImageAdmissionPolicyParameters is absent'
  actual_digest=$(jq -cS '.spec' <<<"$policy_resource" | sha256sum | awk '{print $1}')
  [[ "$actual_digest" == "$expected_digest" ]] ||
    fail 'immutable image admission policy readback mismatch'
  controller=$(kubectl -n "$namespace" get deployment/runtime-controller -o json) ||
    fail 'runtime-controller Deployment is absent'
  jq -e --argjson policy "$expected_policy" '
    .spec.template.metadata.annotations[
      "kodex.dev/runtime-admission-policy-sha256"] == $policy.policySHA256 and
    ([.spec.template.spec.containers[] |
      select(.name == "runtime-controller") | .env[] |
      select(.name == "RUNTIME_CONTROLLER_PROMOTED_ROLE_IMAGE_REPOSITORY") |
      .value] | first) == $policy.promotedPullRepository and
    ([.spec.template.spec.containers[] |
      select(.name == "runtime-controller") | .env[] |
      select(.name == "RUNTIME_CONTROLLER_DEFAULT_ROLE_IMAGE_REFERENCE") |
      .value] | first) == $policy.nodeReadbackImage and
    ([.spec.template.spec.containers[] |
      select(.name == "runtime-controller") | .env[] |
      select(.name == "RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_REVISION") |
      .value] | first) == $policy.roleRuntimeContractRevision and
    ([.spec.template.spec.containers[] |
      select(.name == "runtime-controller") | .env[] |
      select(.name == "RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_SHA256") |
      .value] | first) == $policy.roleRuntimeContractSHA256
  ' <<<"$controller" >/dev/null ||
    fail 'runtime-controller materialization config readback mismatch'
  workloads=$(kubectl -n "$namespace" get deployments -o json) ||
    fail 'local Deployments are unavailable for policy readback'
  expected_deployments=$(yq -o=json -I=0 '
    select(.kind == "Deployment" and .metadata.namespace == "kodex-system") |
    .metadata.name
  ' "$render" | jq -sc 'unique | sort')
  jq -e --argjson policy "$expected_policy" \
    --argjson expected_deployments "$expected_deployments" \
    -f "$script_directory/readback-rendered-deployments.jq" \
    <<<"$workloads" >/dev/null ||
    fail 'local Deployment admission policy materialization readback mismatch'
  jq -e '
    .metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
    (.spec.orchestrationRevision | test("^[a-f0-9]{40}$")) and
    (.spec.toolsImage | test("@sha256:[a-f0-9]{64}$")) and
    (.spec.admissionImage | test("@sha256:[a-f0-9]{64}$")) and
    (.spec.authorityImage | test("@sha256:[a-f0-9]{64}$")) and
    (.spec.nodeReadbackImage | test("@sha256:[a-f0-9]{64}$")) and
    (.spec.trustedRoleBaseDigest | test("^sha256:[a-f0-9]{64}$")) and
    (.spec.pullCredentialGeneration | tonumber) > 0 and
    (.spec.policyRevision | tonumber) > 0 and
    ([.spec.toolsImage,.spec.admissionImage,.spec.authorityImage,
      .spec.nodeReadbackImage,.spec.trustedRoleBaseDigest,.spec.policySHA256,
      .spec.frontendSHA256,.spec.toolchainSHA256,
      .spec.roleRuntimeContractSHA256] |
      all(test("0{64}") | not))
  ' <<<"$policy_resource" >/dev/null || fail 'image admission policy is unresolved'

  for resource in \
    customresourcedefinition/imageadmissionpolicyparameters.supplychain.kodex.dev \
    validatingadmissionpolicy/kodex-image-admission-controller-jobs \
    validatingadmissionpolicybinding/kodex-image-admission-controller-jobs \
    validatingadmissionpolicy/kodex-image-admission-controller-workspaces \
    validatingadmissionpolicybinding/kodex-image-admission-controller-workspaces; do
    kubectl get "$resource" >/dev/null 2>&1 ||
      fail "cluster-scoped image supply-chain resource is absent: $resource"
  done
  for name in kodex-image-registry-pull kodex-image-registry-push \
    kodex-image-registry-promotion kodex-image-registry-staging-read \
    kodex-image-registry-evidence kodex-buildkit image-admission-controller \
    role-image-builder; do
    kubectl -n "$namespace" get "deployment/$name" >/dev/null 2>&1 ||
      fail "image supply-chain Deployment is absent: $name"
  done
  for name in kodex-image-registry-pull kodex-image-registry-push \
    kodex-image-registry-promotion kodex-image-registry-staging-read \
    kodex-image-registry-evidence kodex-buildkit image-admission-controller \
    role-image-builder; do
    kubectl -n "$namespace" get "service/$name" >/dev/null 2>&1 ||
      fail "image supply-chain Service is absent: $name"
  done
  for name in kodex-image-registry-staging kodex-image-registry-promoted; do
    kubectl -n "$namespace" get "persistentvolumeclaim/$name" >/dev/null 2>&1 ||
      fail "image supply-chain PVC is absent: $name"
  done

  kubectl -n "$namespace" get deployment/kodex-buildkit -o json | jq -e '
    .spec.replicas == 1 and .status.readyReplicas == 1 and
    .spec.template.spec.hostUsers == false and
    any(.spec.template.spec.containers[];
      .name == "buildkitd" and .securityContext.privileged == true and
      .securityContext.runAsUser == 0 and
      any(.args[]; . == "--config=/var/run/config/kodex/buildkit/buildkitd.toml"))
  ' >/dev/null || fail 'BuildKit user-namespace/readiness contract failed'

  target_registry=$(kubectl -n "$namespace" get \
    configmap/internal-rpc-authority-publisher-target-registry \
    -o jsonpath='{.data.key-delivery-targets\.yaml}')
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
    fail 'image supply-chain authority targets readback failed'

  promoted_pull_host=$(jq -er '.pullRegistryHost' <<<"$expected_policy")
  "$script_directory/configure-local-node-registry.sh" --mode readback \
    --context "$context" --material-directory "$state_directory/material" \
    --promoted-pull-host "$promoted_pull_host" >/dev/null
}

if [[ "$mode" == apply ]]; then
  ensure_local_object_storage_secret
  ensure_local_backup_controller_secret
  ensure_seed_secrets
  apply_image_admission_crd
  pause_local_image_admission_controller
  cleanup_local_image_admission_runs
  reconcile_local_immutable_image_admission_policy
  reconcile_local_mutable_configmaps
  apply_render foundation '
    select(.kind != "Deployment" and .kind != "StatefulSet" and .kind != "Job" and
      .kind != "Secret" and .kind != "CustomResourceDefinition")
  '
  cleanup_local_frontend_transport
  ensure_session_archive_worker_secret
  cleanup_legacy_session_archive_worker_resources
  wait_certificates
  apply_render statefulsets 'select(.kind == "StatefulSet")'
  reconcile_local_statefulset_rollout kodex-postgresql kodex-nats seaweedfs
  for workload in kodex-postgresql kodex-nats seaweedfs; do
    kubectl -n "$namespace" rollout status "statefulset/$workload" --timeout=10m >/dev/null ||
      fail "local StatefulSet is unavailable: $workload"
  done

  apply_job seaweedfs-bucket-bootstrap

  apply_job internal-rpc-authority-migrate
  apply_job control-plane-migrate
  apply_job kodex-postgresql-runtime-credentials
  apply_job control-plane-broker-bootstrap

  apply_render authority-publisher '
    select(.kind == "Deployment" and .metadata.name == "internal-rpc-authority-publisher")
  '
  apply_render image-registry-workloads '
    select(.kind == "Deployment" and
      (.metadata.name | test("^kodex-image-registry-(pull|push|promotion|staging-read|evidence)$")))
  '
  "$script_directory/seed-local-image-supply-chain.sh" --context "$context" \
    --state-directory "$state_directory" --render "$render"
  apply_render buildkit-workload '
    select(.kind == "Deployment" and .metadata.name == "kodex-buildkit")
  '
  kubectl -n "$namespace" rollout status deployment/kodex-buildkit --timeout=15m >/dev/null ||
    fail 'local BuildKit is unavailable after registry seed'
  apply_render image-admission-workloads '
    select(.kind == "Deployment" and
      (.metadata.name == "image-admission-controller" or
       .metadata.name == "role-image-builder"))
  '
  image_admission_controller_restore_replicas=""
  apply_render application-workloads '
    select(.kind == "Deployment" and
      .metadata.name != "internal-rpc-authority-publisher" and
      .metadata.name != "kodex-buildkit" and
      .metadata.name != "image-admission-controller" and
      .metadata.name != "role-image-builder" and
      (.metadata.name | test("^kodex-image-registry-") | not))
  '
else
  discover_local_object_storage_secret
  readback_session_archive_worker_secret
fi

readback_local_frontend_transport
wait_certificates
readback_local_object_storage_secret
expected_backup_credentials="$temporary_directory/backup-controller-credentials-expected.json"
write_local_backup_controller_credentials "$expected_backup_credentials"
readback_local_backup_controller_secret "$expected_backup_credentials"
for workload in kodex-postgresql kodex-nats seaweedfs; do
  kubectl -n "$namespace" rollout status "statefulset/$workload" --timeout=10m >/dev/null ||
    fail "local StatefulSet is unavailable: $workload"
done
while IFS= read -r workload; do
  [[ -n "$workload" ]] || continue
  kubectl -n "$namespace" rollout status "deployment/$workload" --timeout=15m >/dev/null || {
    kubectl -n "$namespace" get pods -l "app.kubernetes.io/name=$workload" -o wide >&2 || true
    kubectl -n "$namespace" logs "deployment/$workload" --all-containers --tail=120 >&2 || true
    fail "local Deployment is unavailable: $workload"
  }
done < <(yq -N -r 'select(.kind == "Deployment") | .metadata.name' "$render" | sort -u)

for job in seaweedfs-bucket-bootstrap internal-rpc-authority-migrate control-plane-migrate \
  kodex-postgresql-runtime-credentials control-plane-broker-bootstrap; do
  [[ "$(kubectl -n "$namespace" get "job/$job" -o jsonpath='{.status.succeeded}')" == 1 ]] ||
    fail "local Job readback failed: $job"
done

kubectl -n "$namespace" get endpointslice \
  -l kubernetes.io/service-name=seaweedfs-s3 -o json | jq -e '
    any(.items[];
      any(.ports[]?; .name == "s3" and .protocol == "TCP" and .port == 8333) and
      any(.endpoints[]?; .conditions.ready == true and (.addresses | length) > 0)
    )
  ' >/dev/null || fail 'SeaweedFS S3 EndpointSlice readback failed'

readback_session_archive
readback_local_image_supply_chain

wait_warm_runtime
wait_stable_workloads
verify_local_backup_controller

failing=$(kubectl -n "$namespace" get pods -o json | jq -r '
  [.items[] | select(any(.status.containerStatuses[]?;
    .state.waiting.reason == "CrashLoopBackOff" or .state.waiting.reason == "ImagePullBackOff" or
    .state.waiting.reason == "ErrImagePull" or .state.waiting.reason == "CreateContainerConfigError")) |
    .metadata.name] | join(",")
')
[[ -z "$failing" ]] || fail "failing local Pods remain: $failing"

printf 'Kodex local deployment completed: %s\n' "$mode"
