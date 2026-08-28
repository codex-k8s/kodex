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
for command_name in jq kubectl yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'

namespace=kodex-system
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
    snapshot=$(kubectl -n "$namespace" get pods -o json)
    if jq -e '
      [.items[] |
        select(
          .metadata.name == "system-assistant-warm" or
          any(.metadata.ownerReferences[]?; .kind == "ReplicaSet" or .kind == "StatefulSet")
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
  ensure_seed_secrets
  apply_render foundation '
    select(.kind != "Deployment" and .kind != "StatefulSet" and .kind != "Job" and
      .kind != "Secret")
  '
  wait_certificates
  apply_render statefulsets 'select(.kind == "StatefulSet")'
  for workload in kodex-postgresql kodex-nats; do
    kubectl -n "$namespace" rollout status "statefulset/$workload" --timeout=10m >/dev/null ||
      fail "local StatefulSet is unavailable: $workload"
  done

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
for workload in kodex-postgresql kodex-nats; do
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

for job in internal-rpc-authority-migrate control-plane-migrate \
  kodex-postgresql-runtime-credentials control-plane-broker-bootstrap; do
  [[ "$(kubectl -n "$namespace" get "job/$job" -o jsonpath='{.status.succeeded}')" == 1 ]] ||
    fail "local Job readback failed: $job"
done

wait_warm_runtime
wait_stable_workloads

failing=$(kubectl -n "$namespace" get pods -o json | jq -r '
  [.items[] | select(any(.status.containerStatuses[]?;
    .state.waiting.reason == "CrashLoopBackOff" or .state.waiting.reason == "ImagePullBackOff" or
    .state.waiting.reason == "ErrImagePull" or .state.waiting.reason == "CreateContainerConfigError")) |
    .metadata.name] | join(",")
')
[[ -z "$failing" ]] || fail "failing local Pods remain: $failing"

printf 'Kodex local deployment completed: %s\n' "$mode"
