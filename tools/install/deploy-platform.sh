#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex platform deployment failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode defer-public-tls|prepare-preflight|preflight|apply|readback" \
    '  --render <exact-release.yaml> --public-tls-mode deferred|enabled' >&2
}

context=""
mode=""
render_file=""
public_tls_mode=enabled
while (($# > 0)); do
  case "$1" in
    --context) context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --render) render_file="${2:-}"; shift 2 ;;
    --public-tls-mode) public_tls_mode="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail 'exact Kubernetes context is required'
case "$mode" in
  defer-public-tls|prepare-preflight|preflight|apply|readback) ;;
  *) fail 'mode is invalid' ;;
esac
case "$public_tls_mode" in deferred|enabled) ;; *) fail 'public TLS mode is invalid' ;; esac
[[ -f "$render_file" && -s "$render_file" && ! -L "$render_file" ]] ||
  fail 'release render is invalid'
for command_name in jq kubectl rg sha256sum sort yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'current Kubernetes context mismatch'

namespace=kodex-system
public_certificate_name=staff-control-center-public
export KODEX_DEPLOY_PUBLIC_TLS_MODE=$public_tls_mode
export KODEX_DEPLOY_PUBLIC_CERTIFICATE_NAME=$public_certificate_name
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
projection_registry="$repository_root/tools/install/secret-projections.json"
jq -e '
  .version == 1 and .namespace == "kodex-system" and (.secrets | length > 0) and
  ([.secrets[].name] | length == (unique | length)) and
  all(.secrets[]; (.items | type == "array" and length > 0) and
    ([.items[].key] | length == (unique | length)) and
    all(.items[]; ((.required // true) | type == "boolean")))
' "$projection_registry" >/dev/null || fail 'secret projection registry is invalid'
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

rg -n '__KODEX_[A-Z0-9_]+__|\.invalid|sha256:0{64}|kind: (Vault|SecretProviderClass)' \
  "$render_file" >/dev/null && fail 'release render contains unresolved or retired resources'
yq -e 'select(.kind == "Namespace" and .metadata.name == "kodex-system")' \
  "$render_file" >/dev/null || fail 'Kodex namespace is absent from the release render'
yq -o=json -I=0 '.' "$render_file" | jq -s -e '
  map(select(.kind != null)) as $resources |
  ($resources | length) > 0 and
  ($resources | group_by([.apiVersion,.kind,(.metadata.namespace // ""),.metadata.name]) |
    all(.[]; length == 1))
' >/dev/null || fail 'release render has duplicate resource identities'

required_secret_names=$(jq -r '.secrets[].name' "$projection_registry" | sort -u)
while IFS= read -r secret_name; do
  [[ -n "$secret_name" ]] || continue
  kubectl --context "$context" -n "$namespace" get secret "$secret_name" >/dev/null 2>&1 ||
    fail "required Kubernetes Secret is absent: $secret_name"
done <<<"$required_secret_names"
for secret_name in kodex-installation-ca kodex-postgresql-bootstrap \
  kodex-postgresql-runtime-credentials kodex-nats-credentials \
  runtime-provider-openai-default-r1 internal-rpc-authority-bootstrap-roots; do
  kubectl --context "$context" -n "$namespace" get secret "$secret_name" >/dev/null 2>&1 ||
    fail "installation Secret is absent: $secret_name"
done

render_filter() {
  local name=$1 expression=$2 output
  output="$temporary_directory/$name.yaml"
  yq "$expression" "$render_file" >"$output"
  [[ -s "$output" ]] || fail "release phase is empty: $name"
  printf '%s' "$output"
}

apply_render() {
  local name=$1 expression=$2 output
  output=$(render_filter "$name" "$expression")
  kubectl --context "$context" apply --server-side --field-manager=kodex-install \
    -f "$output" >/dev/null
}

wait_statefulset() {
  kubectl --context "$context" -n "$namespace" rollout status "statefulset/$1" \
    --timeout=15m >/dev/null || fail "StatefulSet rollout failed: $1"
}

apply_job() {
  local name=$1 job_file="$temporary_directory/job-$1.yaml"
  JOB_NAME="$name" yq 'select(.kind == "Job" and .metadata.name == strenv(JOB_NAME))' \
    "$render_file" >"$job_file"
  [[ -s "$job_file" ]] || fail "release Job is absent: $name"
  kubectl --context "$context" -n "$namespace" delete "job/$name" \
    --ignore-not-found --wait=true --timeout=5m >/dev/null
  kubectl --context "$context" apply --server-side --field-manager=kodex-install \
    -f "$job_file" >/dev/null
  kubectl --context "$context" -n "$namespace" wait --for=condition=Complete \
    "job/$name" --timeout=20m >/dev/null || {
      kubectl --context "$context" -n "$namespace" logs "job/$name" --all-containers \
        --tail=200 >&2 || true
      fail "release Job failed: $name"
    }
}

ensure_seed_secret() {
  local name=$1 seed="$temporary_directory/seed-$1.yaml"
  if kubectl --context "$context" -n "$namespace" get "secret/$name" >/dev/null 2>&1; then
    return
  fi
  SECRET_NAME="$name" yq '
    select(.kind == "Secret" and .metadata.namespace == "kodex-system" and
      .metadata.name == strenv(SECRET_NAME))
  ' "$render_file" >"$seed"
  [[ -s "$seed" ]] || fail "release seed Secret is absent: $name"
  kubectl --context "$context" create --field-manager=kodex-install -f "$seed" >/dev/null
}

preflight_seed_secret() {
  local name=$1 seed="$temporary_directory/preflight-seed-$1.yaml"
  if kubectl --context "$context" -n "$namespace" get "secret/$name" >/dev/null 2>&1; then
    return
  fi
  SECRET_NAME="$name" yq '
    select(.kind == "Secret" and .metadata.namespace == "kodex-system" and
      .metadata.name == strenv(SECRET_NAME))
  ' "$render_file" >"$seed"
  [[ -s "$seed" ]] || fail "release seed Secret is absent: $name"
  kubectl --context "$context" create --dry-run=server \
    --field-manager=kodex-install -f "$seed" >/dev/null
}

preflight_recreated_resources() {
  local name=$1 expression=$2 source output
  source=$(render_filter "preflight-$name-source" "$expression")
  output="$temporary_directory/preflight-$name.yaml"
  yq '
    .metadata.generateName = "kodex-preflight-" |
    del(.metadata.name)
  ' "$source" >"$output"
  kubectl --context "$context" create --dry-run=server \
    --field-manager=kodex-install -f "$output" >/dev/null
}

reconcile_immutable_configmaps() {
  local name current expected current_content expected_content
  while IFS= read -r name; do
    [[ -n "$name" ]] || continue
    if ! current=$(kubectl --context "$context" -n "$namespace" get \
      "configmap/$name" -o json 2>/dev/null); then
      continue
    fi
    expected=$(CONFIGMAP_NAME="$name" yq -o=json -I=0 '
      select(.kind == "ConfigMap" and .metadata.namespace == "kodex-system" and
        .metadata.name == strenv(CONFIGMAP_NAME))
    ' "$render_file")
    [[ -n "$expected" ]] || fail "immutable ConfigMap is absent from render: $name"
    current_content=$(jq -S -c \
      '{immutable:.immutable,data:.data,binaryData:.binaryData}' <<<"$current")
    expected_content=$(jq -S -c \
      '{immutable:.immutable,data:.data,binaryData:.binaryData}' <<<"$expected")
    if [[ "$current_content" == "$expected_content" ]]; then
      continue
    fi
    jq -e '
      .immutable == true and
      .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
      .metadata.labels["kodex.dev/owner-intent"] == "true"
    ' <<<"$current" >/dev/null ||
      fail "immutable ConfigMap is not owned by Kodex: $name"
    kubectl --context "$context" -n "$namespace" delete "configmap/$name" \
      --wait=true --timeout=3m >/dev/null
  done < <(yq -N -r '
    select(.kind == "ConfigMap" and .metadata.namespace == "kodex-system" and
      .immutable == true) | .metadata.name
  ' "$render_file" | sort -u)
}

reconcile_image_admission_policy_parameters() {
  local name=kodex-image-admission-policy current expected current_spec expected_spec
  if ! current=$(kubectl --context "$context" -n "$namespace" get \
    "imageadmissionpolicyparameters.supplychain.kodex.dev/$name" -o json 2>/dev/null); then
    return
  fi
  expected=$(PARAMETERS_NAME="$name" yq -o=json -I=0 '
    select(.apiVersion == "supplychain.kodex.dev/v1alpha1" and
      .kind == "ImageAdmissionPolicyParameters" and
      .metadata.namespace == "kodex-system" and
      .metadata.name == strenv(PARAMETERS_NAME))
  ' "$render_file")
  [[ -n "$expected" ]] || fail 'image admission policy parameters are absent from render'
  current_spec=$(jq -S -c '.spec' <<<"$current")
  expected_spec=$(jq -S -c '.spec' <<<"$expected")
  if [[ "$current_spec" == "$expected_spec" ]]; then
    return 0
  fi
  jq -e '
    .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
    .metadata.labels["kodex.dev/owner-intent"] == "true"
  ' <<<"$current" >/dev/null ||
    fail 'image admission policy parameters are not owned by Kodex'
  kubectl --context "$context" -n "$namespace" delete \
    "imageadmissionpolicyparameters.supplychain.kodex.dev/$name" \
    --wait=true --timeout=3m >/dev/null
}

prepare_image_admission_preflight() {
  local parameters=kodex-image-admission-policy
  local binding=kodex-image-admission-controller-jobs
  local current expected current_identity expected_identity
  if kubectl --context "$context" -n "$namespace" get \
    "imageadmissionpolicyparameters.supplychain.kodex.dev/$parameters" >/dev/null 2>&1; then
    return
  fi
  if ! current=$(kubectl --context "$context" get \
    "validatingadmissionpolicybinding.admissionregistration.k8s.io/$binding" \
    -o json 2>/dev/null); then
    return
  fi
  expected=$(BINDING_NAME="$binding" yq -o=json -I=0 '
    select(.apiVersion == "admissionregistration.k8s.io/v1" and
      .kind == "ValidatingAdmissionPolicyBinding" and
      .metadata.name == strenv(BINDING_NAME))
  ' "$render_file")
  [[ -n "$expected" ]] || fail 'image admission policy binding is absent from render'
  current_identity=$(jq -S -c '
    .spec.matchResources.matchPolicy //= "Equivalent" |
    .spec.matchResources.objectSelector //= {} |
    {labels:(.metadata.labels // {}),spec:.spec}
  ' <<<"$current")
  expected_identity=$(jq -S -c '
    .spec.matchResources.matchPolicy //= "Equivalent" |
    .spec.matchResources.objectSelector //= {} |
    {labels:(.metadata.labels // {}),spec:.spec}
  ' <<<"$expected")
  [[ "$current_identity" == "$expected_identity" ]] ||
    fail 'stale image admission policy binding differs from exact render'
  kubectl --context "$context" delete \
    "validatingadmissionpolicybinding.admissionregistration.k8s.io/$binding" \
    --wait=true --timeout=3m >/dev/null
  if kubectl --context "$context" get \
    "validatingadmissionpolicybinding.admissionregistration.k8s.io/$binding" >/dev/null 2>&1; then
    fail 'stale image admission policy binding remains after deletion'
  fi
  printf 'Stale image admission policy binding retired before preflight\n'
}

wait_trust_material() {
  local resource_name
  while IFS= read -r resource_name; do
    [[ -n "$resource_name" ]] || continue
    kubectl --context "$context" -n "$namespace" wait --for=condition=Ready \
      "certificate/$resource_name" --timeout=10m >/dev/null ||
      fail "Certificate is not ready: $resource_name"
  done < <(yq -N -r '
    select(.kind == "Certificate" and
      (strenv(KODEX_DEPLOY_PUBLIC_TLS_MODE) == "enabled" or
       .metadata.name != strenv(KODEX_DEPLOY_PUBLIC_CERTIFICATE_NAME))) |
    .metadata.name
  ' "$render_file" | sort -u)
  while IFS= read -r resource_name; do
    [[ -n "$resource_name" ]] || continue
    kubectl --context "$context" wait --for=condition=Synced \
      "bundle/$resource_name" --timeout=10m >/dev/null ||
      fail "trust Bundle is not synced: $resource_name"
  done < <(yq -N -r 'select(.kind == "Bundle") | .metadata.name' "$render_file" | sort -u)
}

public_tls_descendant_count() {
  kubectl --context "$context" -n "$namespace" get \
    certificaterequests.cert-manager.io,orders.acme.cert-manager.io,challenges.acme.cert-manager.io \
    -o json | jq --arg prefix "$public_certificate_name-" '
      [.items[] | select(.metadata.name | startswith($prefix))] | length
    '
}

verify_public_tls_deferred() {
  if kubectl --context "$context" -n "$namespace" get \
    "certificate/$public_certificate_name" >/dev/null 2>&1; then
    fail 'deferred public TLS Certificate remains active'
  fi
  [[ "$(public_tls_descendant_count)" == 0 ]] ||
    fail 'deferred public TLS ACME descendants remain active'
}

defer_public_tls() {
  local deadline
  kubectl --context "$context" -n "$namespace" delete \
    "certificate/$public_certificate_name" --ignore-not-found --wait=true \
    --timeout=3m >/dev/null
  deadline=$((SECONDS + 180))
  while ((SECONDS < deadline)); do
    [[ "$(public_tls_descendant_count)" == 0 ]] && return
    sleep 2
  done
  fail 'public TLS ACME descendants were not garbage-collected'
}

if [[ "$mode" == defer-public-tls ]]; then
  [[ "$public_tls_mode" == deferred ]] ||
    fail 'defer-public-tls requires deferred public TLS mode'
  defer_public_tls
  verify_public_tls_deferred
  printf 'Kodex public TLS issuance deferred\n'
  exit 0
fi

wait_authority_projections() {
	local phase=${1:-all} name required_keys allowed_keys deadline secret_json
	[[ "$phase" == bootstrap || "$phase" == all ]] ||
		fail "authority projection phase is invalid: $phase"
	while IFS=$'\t' read -r name required_keys allowed_keys; do
		[[ -n "$name" ]] || continue
		deadline=$((SECONDS + 600))
		while ((SECONDS < deadline)); do
			secret_json=$(kubectl --context "$context" -n "$namespace" \
				get secret "$name" -o json 2>/dev/null || true)
			if jq -e --argjson required "$required_keys" --argjson allowed "$allowed_keys" '
				(.metadata.annotations["kodex.dev/secret-generation"] // "") as $generation |
				([.data | keys[] | select(. != "_generation")] | sort) as $actual |
				($generation | test("^[1-9][0-9]*$")) and
				((.data["_generation"] // "" | @base64d) == $generation) and
				(($required - $actual) | length == 0) and
				(($actual - $allowed) | length == 0) and
				(.data | length > 1) and
				all(.data[]; type == "string" and length > 0)
			' <<<"$secret_json" >/dev/null 2>&1; then
				break
			fi
      sleep 2
    done
    ((SECONDS < deadline)) || fail "authority Secret projection is not ready: $name"
	done < <(jq -r --arg phase "$phase" '
		.secrets[] |
		select(.dynamic == true) |
		select($phase == "all" or
			(any(.items[]; .key == "issuance_directive_jti") | not)) |
		[.name,
			([.items[] | select(.required != false) | .key] | sort | @json),
			([.items[].key] | sort | @json)] | @tsv' "$projection_registry")
}

wait_workloads() {
  local kind name resource
  while IFS=$'\t' read -r kind name; do
    [[ -n "$kind" && -n "$name" ]] || continue
    resource=${kind,,}
    kubectl --context "$context" -n "$namespace" rollout status "$resource/$name" \
      --timeout=15m >/dev/null || fail "workload rollout failed: $kind/$name"
  done < <(yq -N -r '
    select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "DaemonSet") |
    [.kind,.metadata.name] | @tsv
  ' "$render_file" | sort -u)
}

if [[ "$mode" == prepare-preflight ]]; then
  prepare_image_admission_preflight
  printf 'Kodex platform preflight preparation completed\n'
  exit 0
fi

if [[ "$mode" == preflight ]]; then
  crd_file=$(render_filter preflight-custom-resource-definitions \
    'select(.kind == "CustomResourceDefinition")')
  kubectl --context "$context" apply --server-side --dry-run=server \
    --field-manager=kodex-install \
    -f "$crd_file" >/dev/null

  preflight_expression='select(
    (strenv(KODEX_DEPLOY_PUBLIC_TLS_MODE) == "enabled" or
     .kind != "Certificate" or
     .metadata.name != strenv(KODEX_DEPLOY_PUBLIC_CERTIFICATE_NAME)) and
    .kind != "CustomResourceDefinition" and .kind != "Secret" and
    .kind != "Job" and (.kind != "ConfigMap" or .immutable != true)'
  while IFS= read -r api_version; do
    [[ "$api_version" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?/v[0-9][a-z0-9]*$ ]] ||
      fail "rendered CustomResourceDefinition API version is invalid: $api_version"
    preflight_expression+=" and .apiVersion != \"$api_version\""
  done < <(yq -N -r '
    select(.kind == "CustomResourceDefinition") |
    .spec.group as $group | .spec.versions[] | select(.served == true) |
    $group + "/" + .name
  ' "$render_file" | sort -u)
  preflight_expression+=')'
  known_resources_file=$(render_filter preflight-known-resources "$preflight_expression")
  kubectl --context "$context" apply --server-side --dry-run=server \
    --field-manager=kodex-install \
    -f "$known_resources_file" >/dev/null
  preflight_recreated_resources immutable-configmaps \
    'select(.kind == "ConfigMap" and .immutable == true)'
  preflight_recreated_resources jobs 'select(.kind == "Job")'
  preflight_seed_secret internal-rpc-authority-restore-evidence
  preflight_seed_secret internal-rpc-authority-snapshot
  printf 'Kodex platform deployment preflight completed\n'
  exit 0
fi

if [[ "$mode" == apply ]]; then
  if [[ "$public_tls_mode" == deferred ]]; then
    defer_public_tls
  fi
  apply_render custom-resource-definitions 'select(.kind == "CustomResourceDefinition")'
  while IFS= read -r resource_name; do
    kubectl --context "$context" wait --for=condition=Established \
      "customresourcedefinition/$resource_name" --timeout=3m >/dev/null ||
      fail "CustomResourceDefinition was not established: $resource_name"
  done < <(yq -N -r 'select(.kind == "CustomResourceDefinition") | .metadata.name' "$render_file")

  reconcile_image_admission_policy_parameters
  ensure_seed_secret internal-rpc-authority-restore-evidence
  ensure_seed_secret internal-rpc-authority-snapshot
  reconcile_immutable_configmaps
  apply_render foundation '
    select(.kind != "CustomResourceDefinition" and .kind != "Deployment" and
      .kind != "StatefulSet" and .kind != "DaemonSet" and .kind != "Job" and
      .kind != "CronJob" and .kind != "Secret" and
      (strenv(KODEX_DEPLOY_PUBLIC_TLS_MODE) == "enabled" or
       .kind != "Certificate" or
       .metadata.name != strenv(KODEX_DEPLOY_PUBLIC_CERTIFICATE_NAME)))
  '
  wait_trust_material
  apply_render statefulsets 'select(.kind == "StatefulSet")'
  wait_statefulset kodex-postgresql
  wait_statefulset kodex-nats

  apply_job internal-rpc-authority-migrate
  apply_job control-plane-migrate
  apply_job kodex-postgresql-runtime-credentials
  apply_job control-plane-broker-bootstrap

	apply_render authority-publisher '
		select(.kind == "Deployment" and .metadata.name == "internal-rpc-authority-publisher")
	'
	# Publisher materializes bootstrap keys before it can become Ready: full
	# readiness requires readbacks from workloads applied in the next phase.
	wait_authority_projections bootstrap

	apply_render workloads-before-role-image-builder '
    select((.kind == "Deployment" and .metadata.name != "role-image-builder") or
      .kind == "DaemonSet" or .kind == "CronJob")
  '
  for dependency in egress-gateway kodex-image-registry-promotion; do
    kubectl --context "$context" -n "$namespace" rollout status "deployment/$dependency" \
      --timeout=15m >/dev/null || fail "release materializer dependency failed: $dependency"
  done
  apply_job release-artifact-materializer
  for dependency in kodex-image-registry-pull kodex-buildkit; do
    kubectl --context "$context" -n "$namespace" rollout status "deployment/$dependency" \
      --timeout=15m >/dev/null || fail "role image builder dependency failed: $dependency"
  done
  apply_render role-image-builder '
    select(.kind == "Deployment" and .metadata.name == "role-image-builder")
  '
  wait_workloads
fi

if [[ "$public_tls_mode" == deferred ]]; then
  verify_public_tls_deferred
fi
wait_trust_material
wait_authority_projections all
wait_workloads
for job_name in kodex-postgresql-runtime-credentials internal-rpc-authority-migrate \
  control-plane-migrate control-plane-broker-bootstrap release-artifact-materializer; do
  [[ "$(kubectl --context "$context" -n "$namespace" get "job/$job_name" \
    -o jsonpath='{.status.succeeded}')" == 1 ]] || fail "Job readback failed: $job_name"
done
failing_pods=$(kubectl --context "$context" -n "$namespace" get pods -o json | jq -r '
  [.items[] | select(any(.status.containerStatuses[]?;
    .state.waiting.reason == "CrashLoopBackOff" or .state.waiting.reason == "ImagePullBackOff" or
    .state.waiting.reason == "ErrImagePull" or .state.waiting.reason == "CreateContainerConfigError")) |
    .metadata.name] | join(",")
')
[[ -z "$failing_pods" ]] || fail "failing Pods remain: $failing_pods"
printf 'Kodex platform deployment completed: %s render_sha256=%s\n' \
  "$mode" "$(sha256sum "$render_file" | awk '{print $1}')"
