#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local deployment failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --context <exact-context> --mode apply|readback --render <path>\n' "$0" >&2
}

context=""
mode=""
render=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --mode) mode=${2:-}; shift 2 ;;
    --render) render=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail 'exact Kubernetes context is required'
case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ -f "$render" && -s "$render" && ! -L "$render" ]] || fail 'local render is invalid'
for command_name in jq kubectl openssl sha256sum yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'

namespace=kodex-system
object_storage_secret_name=""
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

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

readback_session_archive() {
  local deployment expected_image endpoint_slices target_registry
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
      any(.env[];
        .name == "SESSION_ARCHIVE_WORKER_IMAGE" and .value == $image))
  ' <<<"$deployment" >/dev/null || fail 'session archive Deployment readback failed'

  for resource in serviceaccount/session-archive serviceaccount/session-archive-worker \
    role/session-archive-controller rolebinding/session-archive-controller \
    networkpolicy/session-archive-default-deny \
    networkpolicy/session-archive-exact-paths \
    networkpolicy/session-archive-worker-object-storage \
    networkpolicy/session-archive-internal-rpc-authority-exact-paths; do
    kubectl -n "$namespace" get "$resource" >/dev/null 2>&1 ||
      fail "session archive runtime resource is absent: $resource"
  done

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
      objectStores: [{
        name: "artifacts",
        endpoint: $endpoint,
        region: $region,
        bucket: $artifactBucket,
        prefix: "organizations",
        accessKeyId: $accessKey,
        secretAccessKey: $secretKey,
        usePathStyle: true,
        allowInsecureLocal: true
      }]
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
    kubectl -n "$namespace" get "pod/$pod" >/dev/null 2>&1 && break
    sleep 2
  done
  kubectl -n "$namespace" get "pod/$pod" >/dev/null 2>&1 ||
    fail 'local warm runtime Pod was not materialized'
  if kubectl -n "$namespace" wait --for=condition=Ready "pod/$pod" --timeout=5m >/dev/null; then
    return
  fi
  kubectl -n "$namespace" get "pod/$pod" -o wide >&2 || true
  kubectl -n "$namespace" describe "pod/$pod" >&2 || true
  while IFS= read -r container; do
    [[ -n "$container" ]] || continue
    printf '%s\n' "--- $pod/$container ---" >&2
    kubectl -n "$namespace" logs "pod/$pod" -c "$container" --tail=200 >&2 || true
  done < <(kubectl -n "$namespace" get "pod/$pod" \
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

if [[ "$mode" == apply ]]; then
  ensure_local_object_storage_secret
  ensure_local_backup_controller_secret
  ensure_seed_secrets
  reconcile_local_mutable_configmaps
  apply_render foundation '
    select(.kind != "Deployment" and .kind != "StatefulSet" and .kind != "Job" and
      .kind != "Secret")
  '
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
  apply_render application-workloads '
    select(.kind == "Deployment" and .metadata.name != "internal-rpc-authority-publisher")
  '
fi

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
