#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Direct production operation failed: %s\n' "$*" >&2; exit 1; }
require_denied() {
  local failure_message=$1 output status
  shift
  set +e
  output=$("$@")
  status=$?
  set -e
  [[ $status -eq 1 && "$output" == no ]] || fail "$failure_message"
}
usage() {
  printf 'Usage: %s --context <exact-context> --operation preflight|apply|readback --mode dark|cutover|rollback --source-sha <40-hex> --lock <path> --lock-sha256 <64-hex> [--gate-evidence <path>]\n' "$0" >&2
}

expected_context=""
operation=""
mode=""
source_sha=""
lock_file=""
lock_sha256=""
gate_evidence=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --operation) operation="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --source-sha) source_sha="${2:-}"; shift 2 ;;
    --lock) lock_file="${2:-}"; shift 2 ;;
    --lock-sha256) lock_sha256="${2:-}"; shift 2 ;;
    --gate-evidence) gate_evidence="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail "exact Kubernetes context is required"
case "$operation" in preflight|apply|readback) ;; *) fail "operation must be preflight, apply or readback" ;; esac
case "$mode" in dark|cutover|rollback) ;; *) fail "mode must be dark, cutover or rollback" ;; esac
[[ "$source_sha" =~ ^[a-f0-9]{40}$ ]] || fail "source SHA must be exact lowercase 40-hex"
for command_name in kubectl jq yq sha256sum base64; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] || fail "Kubernetes context mismatch"

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
validator="$repository_root/tools/release/validate-release-lock.sh"
renderer="$repository_root/tools/release/render-direct-production.sh"
workload_contract_renderer="$repository_root/tools/release/render-production-workload-contracts.sh"
authority_revision_resolver="$repository_root/tools/deploy/resolve-authority-source-revision.sh"
"$validator" --lock "$lock_file" --source-sha "$source_sha" --sha256 "$lock_sha256" >/dev/null

if [[ "$operation" != preflight ]]; then
  [[ -r "$gate_evidence" ]] || fail "owner gate evidence is required"
  jq -e --arg source_sha "$source_sha" --arg mode "$mode" '
    .schema_version == 2 and
    .repository == "codex-k8s/matter-codex" and
    .workflow == ".github/workflows/deploy-production.yml" and
    .workflow_ref == "codex-k8s/matter-codex/.github/workflows/deploy-production.yml@refs/heads/main" and
    .environment == "production" and .owner_actor_verified == true and
    (.workflow_sha | type == "string" and test("^[a-f0-9]{40}$")) and
    .workflow_head_sha == .workflow_sha and
    .source_sha == $source_sha and .mode == $mode and
    ($mode == "rollback" or .workflow_sha == $source_sha) and
    (.run_id | type == "string" and test("^[0-9]+$"))
  ' "$gate_evidence" >/dev/null || fail "owner gate evidence mismatch"
fi

if [[ "$mode" == cutover ]]; then
  command -v gh >/dev/null 2>&1 || fail "gh is required for cutover blocker verification"
  for issue_number in 241 237 194; do
    state=$(gh issue view "$issue_number" --repo codex-k8s/matter-codex --json state --jq .state)
    [[ "$state" == CLOSED ]] || fail "cutover blocker #$issue_number is not closed"
  done
  fail "cutover manifest is intentionally absent from Wave A; materialize it after blockers #241, #237 and #194"
fi

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
render_file="$temporary_directory/direct-production.yaml"
"$renderer" --lock "$lock_file" --source-sha "$source_sha" --sha256 "$lock_sha256" --output "$render_file" >/dev/null

desired_authority_registry="$temporary_directory/desired-authority-registry.yaml"
desired_authority_policy="$temporary_directory/desired-authority-policy.json"
yq -r '
  select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-publisher-target-registry") |
  .data."key-delivery-targets.yaml"
' "$render_file" >"$desired_authority_registry"
yq -r '
  select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-publisher-target-registry") |
  .data."authority-policy.json"
' "$render_file" >"$desired_authority_policy"
[[ -s "$desired_authority_registry" && -s "$desired_authority_policy" ]] ||
  fail "rendered authority registry or policy is absent"

current_authority_registry_object="$temporary_directory/current-authority-registry.json"
current_authority_snapshot_object="$temporary_directory/current-authority-snapshot.json"
set +e
kubectl --context "$expected_context" -n mattercodex-system get \
  configmap internal-rpc-authority-publisher-target-registry -o json \
  >"$current_authority_registry_object" 2>/dev/null
current_authority_registry_status=$?
kubectl --context "$expected_context" -n mattercodex-system get \
  secret internal-rpc-authority-snapshot -o json \
  >"$current_authority_snapshot_object" 2>/dev/null
current_authority_snapshot_status=$?
set -e
if ((current_authority_registry_status == 0 && current_authority_snapshot_status == 0)); then
  current_authority_registry="$temporary_directory/current-authority-registry.yaml"
  authority_snapshot_payload="$temporary_directory/authority-snapshot-payload.json"
  jq -er '.data["key-delivery-targets.yaml"]' "$current_authority_registry_object" \
    >"$current_authority_registry" || fail "installed authority registry is absent"
  authority_snapshot_compact=$(jq -er '.data["snapshot.jws"]' "$current_authority_snapshot_object" | base64 --decode) ||
    fail "installed authority snapshot is absent or malformed"
  [[ "$authority_snapshot_compact" =~ ^[^.]+\.([^.]+)\.[^.]+$ ]] ||
    fail "installed authority snapshot is not a compact JWS"
  authority_snapshot_payload_segment=${BASH_REMATCH[1]}
  case $((${#authority_snapshot_payload_segment} % 4)) in
    0) authority_snapshot_payload_padding="" ;;
    2) authority_snapshot_payload_padding="==" ;;
    3) authority_snapshot_payload_padding="=" ;;
    *) fail "installed authority snapshot payload has invalid base64url length" ;;
  esac
  printf '%s%s' "$authority_snapshot_payload_segment" "$authority_snapshot_payload_padding" |
    tr '_-' '/+' | base64 --decode >"$authority_snapshot_payload" ||
    fail "installed authority snapshot payload cannot be decoded"
  unset authority_snapshot_compact authority_snapshot_payload_segment authority_snapshot_payload_padding
  authority_revision_recovery_arguments=()
  if ! kubectl --context "$expected_context" -n mattercodex-system get \
    deployment internal-rpc-authority-publisher -o json 2>/dev/null | jq -e '
      (.status.observedGeneration // 0) >= .metadata.generation and
      (.status.readyReplicas // 0) == .spec.replicas and
      (.status.availableReplicas // 0) == .spec.replicas
    ' >/dev/null; then
    authority_revision_recovery_arguments+=(--publisher-recovery-required)
  fi
  resolved_authority_revision=$("$authority_revision_resolver" \
    --desired-registry "$desired_authority_registry" \
    --desired-policy "$desired_authority_policy" \
    --current-registry "$current_authority_registry" \
    --snapshot-payload "$authority_snapshot_payload" \
    "${authority_revision_recovery_arguments[@]}")
elif ((current_authority_registry_status != 0 && current_authority_snapshot_status != 0)); then
  resolved_authority_revision=$("$authority_revision_resolver" \
    --desired-registry "$desired_authority_registry" \
    --desired-policy "$desired_authority_policy")
else
  fail "authority registry and snapshot presence is inconsistent"
fi
[[ "$resolved_authority_revision" =~ ^[1-9][0-9]*$ ]] ||
  fail "resolved authority source revision is invalid"
RESOLVED_AUTHORITY_REVISION="$resolved_authority_revision" yq -i '
  with(select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-publisher-target-registry");
    .data."key-delivery-targets.yaml" = (
      .data."key-delivery-targets.yaml" | from_yaml |
      .source_revision = (env(RESOLVED_AUTHORITY_REVISION) | tonumber) |
      to_yaml
    )
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "control-plane");
    (.spec.template.spec.containers[] | select(.name == "internal-rpc-authority-verifier") | .env) = (
      ((.spec.template.spec.containers[] | select(.name == "internal-rpc-authority-verifier") | .env) // [] |
        map(select(.name != "INTERNAL_RPC_AUTHORITY_RESOLVER_PROOF_SIGNER_GENERATION"))) +
      [{
        "name": "INTERNAL_RPC_AUTHORITY_RESOLVER_PROOF_SIGNER_GENERATION",
        "value": strenv(RESOLVED_AUTHORITY_REVISION)
      }]
    )
  )
' "$render_file"
resolver_generation_count=$(yq -o=json eval-all '.' "$render_file" | jq -sc --arg revision "$resolved_authority_revision" '
  [.[].spec.template.spec.containers[]? |
    select(.name == "internal-rpc-authority-verifier") |
    .env[]? |
    select(.name == "INTERNAL_RPC_AUTHORITY_RESOLVER_PROOF_SIGNER_GENERATION" and .value == $revision)] |
  length
')
[[ "$resolver_generation_count" == 1 ]] ||
  fail "control-plane resolver proof signer generation was not bound to authority revision"

workload_contract_file="$temporary_directory/production-workload-contracts.yaml"
"$workload_contract_renderer" --manifest "$render_file" --output "$workload_contract_file" >/dev/null
yq -o=json eval-all '.' "$render_file" | jq -sc -e '
  map(select(type == "object" and .kind != null)) as $resources |
  ($resources | length) > 0 and
  all($resources[];
    .metadata.namespace == "mattercodex-system" and
    .metadata.labels["mattercodex.dev/profile"] == "direct-production-single-node-prototype" and
    .metadata.labels["mattercodex.dev/release-managed"] == "true" and
    (if (.kind == "Deployment" or .kind == "StatefulSet" or .kind == "Job") then
       .spec.template.metadata.labels["mattercodex.dev/release-managed"] == "true"
     elif .kind == "CronJob" then
       .spec.jobTemplate.spec.template.metadata.labels["mattercodex.dev/release-managed"] == "true"
     else true end))
' >/dev/null || fail "render contains a resource outside the owner-gated release boundary"

# Owner bootstrap initially materializes foundation resources with client-side
# apply. The exact, lock-validated release manifest then becomes their sole SSA
# owner. Force is deliberately scoped to slices of that validated manifest.
apply_release_manifest() {
  kubectl --context "$expected_context" apply --server-side --force-conflicts \
    --field-manager=mattercodex-production-deployer "$@"
}

can_i() {
  local verb=$1 resource=$2 namespace=$3
  kubectl --context "$expected_context" auth can-i "$verb" "$resource" -n "$namespace" | grep -qx yes ||
    fail "deployer cannot $verb $resource in $namespace"
}

for permission in \
  'get configmaps' 'list configmaps' 'create configmaps' 'patch configmaps' 'update configmaps' \
  'get services' 'list services' 'create services' 'patch services' 'update services' \
  'get deployments.apps' 'list deployments.apps' 'watch deployments.apps' 'create deployments.apps' 'patch deployments.apps' 'update deployments.apps' \
  'get statefulsets.apps' 'list statefulsets.apps' 'watch statefulsets.apps' 'create statefulsets.apps' 'patch statefulsets.apps' 'update statefulsets.apps' \
  'get jobs.batch' 'list jobs.batch' 'watch jobs.batch' 'create jobs.batch' 'patch jobs.batch' 'update jobs.batch' \
  'get cronjobs.batch' 'list cronjobs.batch' 'create cronjobs.batch' 'patch cronjobs.batch' 'update cronjobs.batch' \
  'get pods' 'list pods' \
  'get persistentvolumeclaims' 'list persistentvolumeclaims' \
  'get ingresses.networking.k8s.io' 'list ingresses.networking.k8s.io'; do
  read -r verb resource <<<"$permission"
  can_i "$verb" "$resource" mattercodex-system
done
for migration in control-plane-broker-bootstrap control-plane-migrate integration-gateway-migrate interaction-gateway-migrate \
  internal-rpc-authority-migrate mattercodex-postgresql-principal-bootstrap runtime-controller-migration; do
  can_i delete "jobs.batch/$migration" mattercodex-system
done
can_i get secrets/internal-rpc-authority-snapshot mattercodex-system
for permission in 'get services' 'list services' 'get deployments.apps' 'list deployments.apps' 'get statefulsets.apps' 'list statefulsets.apps' 'get ingresses.networking.k8s.io' 'list ingresses.networking.k8s.io'; do
  read -r verb resource <<<"$permission"
  can_i "$verb" "$resource" matter-kodex-prod
done
 require_denied "routine deployer must not have broad Secret read access" \
  kubectl --context "$expected_context" auth can-i get secrets -n mattercodex-system
 require_denied "routine deployer must not read Pod logs" \
  kubectl --context "$expected_context" auth can-i get pods --subresource=log -n mattercodex-system
 require_denied "routine deployer must not create Certificates" \
  kubectl --context "$expected_context" auth can-i create certificates.cert-manager.io \
  -n mattercodex-system
kubectl --context "$expected_context" -n mattercodex-system get configmap mattercodex-bootstrap-readiness \
  -o json | jq -e '.data.status == "ready" and .data.profile == "direct-production single-node prototype"' >/dev/null ||
  fail "owner bootstrap readiness is absent or invalid"
kubectl --context "$expected_context" -n matter-kodex-prod get service matter-codex-registry >/dev/null

# The admission allowlist is derived from the same exact, lock-validated render
# as the workloads. Apply it first so server-side validation compares the new
# workload revision with its matching owner contract instead of the previous
# release. Preflight remains read-only and therefore validates against the
# currently installed contract.
if [[ "$operation" == apply ]]; then
  apply_release_manifest -f "$workload_contract_file" >/dev/null
fi
non_job_render="$temporary_directory/non-jobs.yaml"
yq eval-all 'select(.kind != "Job")' "$render_file" >"$non_job_render"
apply_release_manifest --dry-run=server -f "$non_job_render" >/dev/null
while IFS= read -r migration; do
  [[ -n "$migration" && "$migration" != '---' ]] || continue
  migration_manifest="$temporary_directory/job-$migration.yaml"
  MIGRATION="$migration" yq eval-all 'select(.kind == "Job" and .metadata.name == strenv(MIGRATION))' \
    "$render_file" >"$migration_manifest"
  kubectl --context "$expected_context" create --dry-run=client -f "$migration_manifest" >/dev/null
  if kubectl --context "$expected_context" -n mattercodex-system get job "$migration" >/dev/null 2>&1; then
    set +e
    replace_output=$(kubectl --context "$expected_context" replace --dry-run=server \
      -f "$migration_manifest" 2>&1)
    replace_status=$?
    set -e
    if ((replace_status != 0)); then
      [[ "$replace_output" == *"field is immutable"* ]] ||
        fail "existing migration Job failed server-side validation: $migration"
      for rejection_marker in 'forbidden' 'denied request' 'strict decoding error' \
        'error validating data' 'unknown field'; do
        [[ "${replace_output,,}" != *"$rejection_marker"* ]] ||
          fail "existing migration Job was rejected before immutable replacement: $migration"
      done
    fi
  else
    kubectl --context "$expected_context" create --dry-run=server -f "$migration_manifest" >/dev/null
  fi
done < <(yq eval-all -r 'select(.kind == "Job") | .metadata.name' "$render_file")

negative_secret_mount="$temporary_directory/negative-secret-mount.yaml"
yq eval-all '
  select(.kind == "Deployment" and .metadata.name == "control-plane") |
  (.spec.template.spec.volumes[] | select(.secret != null) | .secret.secretName) = "forbidden-production-secret"
' "$render_file" >"$negative_secret_mount"
if apply_release_manifest --dry-run=server -f "$negative_secret_mount" >/dev/null 2>&1; then
  fail "production admission accepted a forged Secret mount"
fi

if [[ "$operation" == preflight ]]; then
  printf 'Direct production preflight completed for mode %s\n' "$mode"
  exit 0
fi

select_kinds() {
  local destination=$1
  shift
  KINDS=$(printf '%s\n' "$@" | jq -Rsc 'split("\n") | map(select(length > 0))') \
    yq eval-all 'select(.kind != null and (.kind as $kind | env(KINDS) | any_c(. == $kind)))' \
      "$render_file" >"$destination"
}

wait_rollouts() {
  local resource_kind=$1 manifest_kind=$2
  while IFS= read -r name; do
    [[ -n "$name" && "$name" != '---' ]] || continue
    kubectl --context "$expected_context" -n mattercodex-system rollout status "$resource_kind/$name" --timeout=5m >/dev/null
  done < <(MANIFEST_KIND="$manifest_kind" yq eval-all -r 'select(.kind == strenv(MANIFEST_KIND)) | .metadata.name' "$render_file")
}

run_job() {
  local name=$1 manifest="$temporary_directory/job-$name.yaml"
  NAME="$name" yq eval-all 'select(.kind == "Job" and .metadata.name == strenv(NAME))' "$render_file" >"$manifest"
  [[ -s "$manifest" ]] || fail "required Job is absent from render: $name"
  kubectl --context "$expected_context" -n mattercodex-system delete job "$name" --ignore-not-found --wait >/dev/null
  apply_release_manifest -f "$manifest" >/dev/null
  kubectl --context "$expected_context" -n mattercodex-system wait --for=condition=complete "job/$name" --timeout=5m >/dev/null
}

if [[ "$operation" == apply ]]; then
  select_kinds "$temporary_directory/foundation.yaml" ConfigMap Service StatefulSet
  apply_release_manifest -f "$temporary_directory/foundation.yaml" >/dev/null
  wait_rollouts statefulset StatefulSet

  run_job mattercodex-postgresql-principal-bootstrap

  yq eval-all 'select(.kind == "Job" and .metadata.name != "mattercodex-postgresql-principal-bootstrap")' \
    "$render_file" >"$temporary_directory/migrations.yaml"
  while IFS= read -r migration; do
    [[ -n "$migration" && "$migration" != '---' ]] || continue
    kubectl --context "$expected_context" -n mattercodex-system delete job "$migration" --ignore-not-found --wait >/dev/null
  done < <(yq eval-all -r 'select(.kind == "Job" and .metadata.name != "mattercodex-postgresql-principal-bootstrap") | .metadata.name' "$render_file")
  apply_release_manifest -f "$temporary_directory/migrations.yaml" >/dev/null
  while IFS= read -r migration; do
    [[ -n "$migration" && "$migration" != '---' ]] || continue
    kubectl --context "$expected_context" -n mattercodex-system wait --for=condition=complete "job/$migration" --timeout=5m >/dev/null
  done < <(yq eval-all -r 'select(.kind == "Job" and .metadata.name != "mattercodex-postgresql-principal-bootstrap") | .metadata.name' "$render_file")

  # Migrations may create generation roles; the owner-controlled password binding
  # is reasserted forward-only before any application can start.
  run_job mattercodex-postgresql-principal-bootstrap

  select_kinds "$temporary_directory/applications.yaml" Deployment CronJob
  yq eval-all 'select(.kind == "Deployment" and
    (.metadata.name == "internal-rpc-authority-publisher" or
     .metadata.name == "internal-rpc-authority-readback-attestor" or
     .metadata.name == "internal-rpc-authority-database-credential-reconciler"))' \
    "$render_file" >"$temporary_directory/authority-bootstrap.yaml"
  apply_release_manifest -f "$temporary_directory/authority-bootstrap.yaml" >/dev/null
  expected_authority_revision=$(yq -r '
    select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-publisher-target-registry") |
    .data."key-delivery-targets.yaml" | from_yaml | .source_revision
  ' "$render_file")
  [[ "$expected_authority_revision" =~ ^[1-9][0-9]*$ ]] ||
    fail "publisher authority source revision is invalid"
  snapshot_ready=false
  for _ in $(seq 1 60); do
    if kubectl --context "$expected_context" -n mattercodex-system get secret internal-rpc-authority-snapshot -o json |
      jq -e --arg expected_revision "$expected_authority_revision" '
        ((.data["snapshot.jws"] // "") | length > 0) and
        .metadata.annotations["mattercodex.dev/source-revision"] == $expected_revision
      ' >/dev/null; then
      snapshot_ready=true
      break
    fi
    sleep 5
  done
  [[ "$snapshot_ready" == true ]] || fail "publisher-owned authority snapshot was not materialized"
  apply_release_manifest -f "$temporary_directory/applications.yaml" >/dev/null
  wait_rollouts deployment Deployment
fi

stored_source=$(kubectl --context "$expected_context" -n mattercodex-system get configmap mattercodex-release-lock -o jsonpath='{.data.source_sha}')
stored_lock=$(kubectl --context "$expected_context" -n mattercodex-system get configmap mattercodex-release-lock -o jsonpath='{.data.release_lock_sha256}')
[[ "$stored_source" == "$source_sha" && "$stored_lock" == "$lock_sha256" ]] || fail "release readback mismatch"

expected_workload_contract=$(yq -o=json '.data' "$workload_contract_file" | jq -Sc .)
actual_workload_contract=$(kubectl --context "$expected_context" -n mattercodex-system \
  get configmap mattercodex-production-workload-contracts -o json | jq -Sc '.data')
[[ "$actual_workload_contract" == "$expected_workload_contract" ]] ||
  fail "production workload contract readback mismatch"

workload_contract_identity=$(yq -o=json '[.kind, .metadata.name]' "$workload_contract_file" | jq -Sc .)
expected_resources=$(yq -o=json eval-all '.' "$render_file" | jq -Scs --argjson workload_contract "$workload_contract_identity" '
  map(select(type == "object" and (.kind == "ConfigMap" or .kind == "Service" or .kind == "Deployment" or
    .kind == "StatefulSet" or .kind == "Job" or .kind == "CronJob"))) |
  (map([.kind,.metadata.name]) + [$workload_contract]) | sort
')
actual_resources=$(kubectl --context "$expected_context" -n mattercodex-system \
  get configmap,service,deployment,statefulset,job,cronjob -l mattercodex.dev/release-managed=true -o json |
  jq -Sc '[.items[] | [.kind,.metadata.name]] | sort')
[[ "$actual_resources" == "$expected_resources" ]] || fail "release-managed resource set mismatch"

kubectl --context "$expected_context" -n mattercodex-system get statefulset,deployment -l mattercodex.dev/release-managed=true -o json |
  jq -e 'all(.items[];
    (.status.observedGeneration >= .metadata.generation) and
    ((.status.readyReplicas // 0) == (.spec.replicas // 1)) and
    ((.status.availableReplicas // 0) == (.spec.replicas // 1)))' >/dev/null ||
  fail "a release-managed workload is not Ready and Available"
kubectl --context "$expected_context" -n mattercodex-system get job -l mattercodex.dev/release-managed=true -o json |
  jq -e 'all(.items[]; any(.status.conditions[]?; .type == "Complete" and .status == "True"))' >/dev/null ||
  fail "a migration Job is not complete"
kubectl --context "$expected_context" -n mattercodex-system get pvc -o json |
  jq -e '(.items | length) > 0 and all(.items[]; .status.phase == "Bound")' >/dev/null ||
  fail "a direct-production PVC is absent or not Bound"
# Deployment/StatefulSet readiness and Job completion are checked above. Image
# readback is scoped to the currently running pods so terminal pods retained by
# old ReplicaSets after node recovery cannot poison every later deployment.
kubectl --context "$expected_context" -n mattercodex-system get pods -l mattercodex.dev/release-managed=true -o json |
  jq -e -f "$script_directory/verify-running-pod-images.jq" >/dev/null ||
  fail "running image digest readback failed"
# The exact public entrypoint is owned by the separate owner bootstrap. The
# dark release itself still contains no Ingress, and every other route is
# rejected by the allowlist readback below.
kubectl --context "$expected_context" -n mattercodex-system get ingress -o json |
  jq -e -f "$script_directory/verify-dark-ingresses.jq" >/dev/null ||
  fail "dark namespace contains an unapproved Ingress"
printf 'Direct production %s completed for mode %s\n' "$operation" "$mode"
