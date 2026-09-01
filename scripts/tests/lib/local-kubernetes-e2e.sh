#!/usr/bin/env bash

kodex_e2e_ensure_private_directory() {
  local directory=$1
  [[ "$directory" == /* ]] || return 1
  if [[ -e "$directory" && ( ! -d "$directory" || -L "$directory" ) ]]; then
    return 1
  fi
  mkdir -p -- "$directory" || return 1
  chmod 0700 -- "$directory" || return 1
  [[ -d "$directory" && ! -L "$directory" ]]
}

kodex_e2e_require_seaweedfs_s3_endpoint() {
  local namespace=$1 service endpoint_slices
  service=$(kubectl -n "$namespace" get service/seaweedfs-s3 -o json) || return 1
  jq -e '
    .metadata.name == "seaweedfs-s3" and
    .metadata.namespace == "kodex-system" and
    .metadata.labels["app.kubernetes.io/name"] == "seaweedfs" and
    .metadata.labels["app.kubernetes.io/component"] == "object-storage" and
    .metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
    any(.spec.ports[]?;
      .name == "s3" and .protocol == "TCP" and .port == 8333 and .targetPort == "s3")
  ' <<<"$service" >/dev/null || return 1

  endpoint_slices=$(kubectl -n "$namespace" get endpointslices.discovery.k8s.io \
    -l kubernetes.io/service-name=seaweedfs-s3 -o json) || return 1
  jq -e '
    any(.items[]?;
      .metadata.namespace == "kodex-system" and
      .metadata.labels["kubernetes.io/service-name"] == "seaweedfs-s3" and
      .metadata.labels["app.kubernetes.io/name"] == "seaweedfs" and
      .metadata.labels["app.kubernetes.io/component"] == "object-storage" and
      .metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
      any(.ports[]?; .name == "s3" and .protocol == "TCP" and .port == 8333) and
      any(.endpoints[]?;
        .conditions.ready == true and
        (.conditions.serving // true) == true and
        (.conditions.terminating // false) == false and
        (.addresses | length) > 0))
  ' <<<"$endpoint_slices" >/dev/null
}

kodex_e2e_start_seaweedfs_port_forward() {
  local namespace=$1 log_file=$2 deadline
  KODEX_E2E_PORT_FORWARD_PID=""
  KODEX_E2E_PORT_FORWARD_ENDPOINT=""
  kodex_e2e_require_seaweedfs_s3_endpoint "$namespace" || return 1
  kubectl -n "$namespace" port-forward --address=127.0.0.1 service/seaweedfs-s3 :8333 \
    >"$log_file" 2>&1 &
  KODEX_E2E_PORT_FORWARD_PID=$!
  deadline=$((SECONDS + 60))
  while ((SECONDS < deadline)); do
    KODEX_E2E_PORT_FORWARD_ENDPOINT=$(sed -nE \
      's/^Forwarding from 127\.0\.0\.1:([0-9]+) -> 8333$/http:\/\/127.0.0.1:\1/p' \
      "$log_file" | head -n 1)
    [[ -n "$KODEX_E2E_PORT_FORWARD_ENDPOINT" ]] && return 0
    if ! kill -0 "$KODEX_E2E_PORT_FORWARD_PID" >/dev/null 2>&1; then
      wait "$KODEX_E2E_PORT_FORWARD_PID" >/dev/null 2>&1 || true
      KODEX_E2E_PORT_FORWARD_PID=""
      return 1
    fi
    sleep 0.2
  done
  kill "$KODEX_E2E_PORT_FORWARD_PID" >/dev/null 2>&1 || true
  wait "$KODEX_E2E_PORT_FORWARD_PID" >/dev/null 2>&1 || true
  KODEX_E2E_PORT_FORWARD_PID=""
  return 1
}

kodex_e2e_print_job_diagnostics() {
  local namespace=$1 job_name=$2 job pods
  job=$(kubectl -n "$namespace" get "job/$job_name" -o json 2>/dev/null || true)
  if [[ -n "$job" ]]; then
    jq -c '{
      kind: "Job",
      namespace: .metadata.namespace,
      name: .metadata.name,
      status: {
        active: (.status.active // 0),
        failed: (.status.failed // 0),
        succeeded: (.status.succeeded // 0),
        conditions: [.status.conditions[]? | {type, status, reason}]
      }
    }' <<<"$job" >&2 || true
  fi
  pods=$(kubectl -n "$namespace" get pods -l "job-name=$job_name" -o json 2>/dev/null || true)
  if [[ -n "$pods" ]]; then
    jq -c '.items[]? | {
      kind: "Pod",
      namespace: .metadata.namespace,
      name: .metadata.name,
      phase: .status.phase,
      containers: [.status.containerStatuses[]? | {
        name,
        ready,
        restartCount,
        waitingReason: (.state.waiting.reason // null),
        terminatedReason: (.state.terminated.reason // null),
        exitCode: (.state.terminated.exitCode // null),
        signal: (.state.terminated.signal // null)
      }]
    }' <<<"$pods" >&2 || true
  fi
}

kodex_e2e_wait_job_complete() {
  local namespace=$1 job_name=$2 timeout_seconds=$3 deadline state terminal
  [[ "$timeout_seconds" =~ ^[1-9][0-9]*$ ]] || return 1
  deadline=$((SECONDS + timeout_seconds))
  while ((SECONDS < deadline)); do
    state=$(kubectl -n "$namespace" get "job/$job_name" -o json 2>/dev/null || true)
    if [[ -n "$state" ]]; then
      terminal=$(jq -r '
        [.status.conditions[]? |
          select(.status == "True" and (.type == "Complete" or .type == "Failed")) |
          .type][0] // empty
      ' <<<"$state")
      case "$terminal" in
        Complete) return 0 ;;
        Failed)
          kodex_e2e_print_job_diagnostics "$namespace" "$job_name"
          return 1
          ;;
      esac
    fi
    sleep 2
  done
  kodex_e2e_print_job_diagnostics "$namespace" "$job_name"
  return 1
}

kodex_e2e_delete_owned_jobs() {
  local namespace=$1 selector=$2 name_pattern=$3 timeout=$4 inventory
  inventory=$(kubectl -n "$namespace" get jobs -l "$selector" -o json) || return 1
  jq -e --arg namespace "$namespace" --arg name_pattern "$name_pattern" '
    all(.items[]?;
      .metadata.namespace == $namespace and
      .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
      .metadata.labels["app.kubernetes.io/managed-by"] == "kodex-local-e2e" and
      .metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
      (.metadata.name | test($name_pattern)) and
      (.spec.activeDeadlineSeconds | type == "number" and . > 0 and . <= 900))
  ' <<<"$inventory" >/dev/null || return 1
  kubectl -n "$namespace" delete jobs -l "$selector" --ignore-not-found \
    --wait=true --timeout="$timeout" >/dev/null || return 1
  kubectl -n "$namespace" get jobs -l "$selector" -o json | jq -e '.items | length == 0' >/dev/null
}
