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

kodex_e2e_sanitize_diagnostic_stream() {
  sed -E \
    -e 's#((postgres(ql)?|redis|https?|s3)://)[^/@[:space:]]+@#\1[REDACTED]@#Ig' \
    -e 's#([Bb]earer[[:space:]]+)[^,;[:space:]]+#\1[REDACTED]#g' \
    -e 's#((authorization|proxy-authorization|cookie|set-cookie)["'"'"'=:[:space:]]+)[^,;[:space:]]+#\1[REDACTED]#Ig' \
    -e 's#((token|secret|password|passwd|api[-_]?key|access[-_]?key|private[-_]?key)["'"'"'=:[:space:]]+)[^,;[:space:]]+#\1[REDACTED]#Ig' \
    -e 's#[A-Za-z0-9_+./=-]{48,}#[REDACTED]#g'
}

kodex_e2e_retain_owned_terminal_jobs_on_failure() {
  local namespace=$1 selector=$2 name_pattern=$3 bundle_directory=$4
  local inventory terminal_inventory job_count job_index job_name job_uid pods pod_count
  local pod_index pod_name container_name log_file events_file temporary_file collected_at run_id

  if [[ ",$selector," =~ ,kodex\.dev/e2e-run=([a-z0-9]([-a-z0-9.]{0,61}[a-z0-9])?), ]]; then
    run_id=${BASH_REMATCH[1]}
  else
    return 1
  fi

  inventory=$(kubectl -n "$namespace" get jobs -l "$selector" -o json) || return 1
  jq -e --arg namespace "$namespace" --arg name_pattern "$name_pattern" --arg run_id "$run_id" '
    all(.items[]?;
      .metadata.namespace == $namespace and
      .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
      .metadata.labels["app.kubernetes.io/managed-by"] == "kodex-local-e2e" and
      .metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
      .metadata.labels["kodex.dev/e2e-run"] == $run_id and
      (.metadata.name | test($name_pattern)) and
      (.spec.activeDeadlineSeconds | type == "number" and . > 0 and . <= 900))
  ' <<<"$inventory" >/dev/null || return 1

  terminal_inventory=$(jq -c '{items:[.items[]? | select(any(.status.conditions[]?;
    .status == "True" and (.type == "Complete" or .type == "Failed")))]}' <<<"$inventory") || return 1
  job_count=$(jq '.items | length' <<<"$terminal_inventory") || return 1
  ((job_count > 0)) || return 0

  kodex_e2e_ensure_private_directory "$bundle_directory" || return 1
  mkdir -p -- "$bundle_directory/logs" "$bundle_directory/events"
  chmod 0700 -- "$bundle_directory/logs" "$bundle_directory/events"
  collected_at=$(date -u +%Y-%m-%dT%H:%M:%SZ) || return 1
  for ((job_index = 0; job_index < job_count; job_index++)); do
    job_name=$(jq -er --argjson index "$job_index" '.items[$index].metadata.name' <<<"$terminal_inventory") || return 1
    job_uid=$(jq -er --argjson index "$job_index" '.items[$index].metadata.uid | select(type == "string" and length > 0)' <<<"$terminal_inventory") || return 1
    pods=$(kubectl -n "$namespace" get pods -l "job-name=$job_name" -o json) || return 1
    jq -e --arg namespace "$namespace" --arg job_uid "$job_uid" '
      all(.items[]?;
        .metadata.namespace == $namespace and
        (.status.phase == "Succeeded" or .status.phase == "Failed") and
        any(.metadata.ownerReferences[]?;
          .apiVersion == "batch/v1" and .kind == "Job" and .uid == $job_uid and .controller == true))
    ' <<<"$pods" >/dev/null || return 1
    kubectl -n "$namespace" patch "job/$job_name" --type=merge \
      -p '{"spec":{"ttlSecondsAfterFinished":1800}}' >/dev/null || return 1
    pod_count=$(jq '.items | length' <<<"$pods") || return 1
    for ((pod_index = 0; pod_index < pod_count; pod_index++)); do
      pod_name=$(jq -er --argjson index "$pod_index" '.items[$index].metadata.name' <<<"$pods") || return 1
      mkdir -p -- "$bundle_directory/logs/$pod_name"
      chmod 0700 -- "$bundle_directory/logs/$pod_name"
      while IFS= read -r container_name; do
        [[ "$container_name" =~ ^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$ ]] || return 1
        log_file="$bundle_directory/logs/$pod_name/$container_name.log"
        kubectl -n "$namespace" logs "$pod_name" -c "$container_name" \
          --tail=500 --limit-bytes=262144 2>&1 | kodex_e2e_sanitize_diagnostic_stream >"$log_file" || true
        chmod 0600 -- "$log_file"
      done < <(jq -r '.items['"$pod_index"'].spec.initContainers[]?.name,
        .items['"$pod_index"'].spec.containers[]?.name' <<<"$pods")

      events_file="$bundle_directory/events/$pod_name.json"
      kubectl -n "$namespace" get events --field-selector "involvedObject.uid=$(jq -r --argjson index "$pod_index" '.items[$index].metadata.uid' <<<"$pods")" -o json 2>/dev/null | \
        jq '{items:[.items[]? | {
          type:(.type // null), reason:(.reason // null), action:(.action // null),
          count:(.count // 0), firstTimestamp:(.firstTimestamp // null),
          lastTimestamp:(.lastTimestamp // null), eventTime:(.eventTime // null)
        }]}' >"$events_file" || printf '%s\n' '{"items":[]}' >"$events_file"
      chmod 0600 -- "$events_file"
    done
  done

  temporary_file=$(mktemp "$bundle_directory/.inventory.XXXXXX") || return 1
  jq --arg namespace "$namespace" --arg selector "$selector" --arg collected_at "$collected_at" '
    {
      version:1,
      namespace:$namespace,
      selector:$selector,
      collectedAt:$collected_at,
      retentionSecondsAfterFinished:1800,
      jobs:[.items[] | {
        name:.metadata.name,
        uid:.metadata.uid,
        status:{
          active:(.status.active // 0), failed:(.status.failed // 0),
          succeeded:(.status.succeeded // 0),
          conditions:[.status.conditions[]? | {type,status,reason}]
        }
      }]
    }
  ' <<<"$terminal_inventory" >"$temporary_file" || return 1
  chmod 0600 -- "$temporary_file"
  mv -- "$temporary_file" "$bundle_directory/inventory.json"
  printf 'Private sanitized Kubernetes diagnostics: %s\n' "$bundle_directory" >&2
}
