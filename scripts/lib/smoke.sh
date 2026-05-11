#!/usr/bin/env bash

if [[ -z "${PROJECT_ROOT:-}" ]]; then
  echo "scripts/lib/smoke.sh: PROJECT_ROOT is required before sourcing" >&2
  exit 1
fi

kodex_smoke_escape_squote() {
  printf "%s" "$1" | sed "s/'/'\\\\''/g"
}

kodex_smoke_init() {
  KODEX_SMOKE_SCRIPT_NAME="$1"
  ENV_FILE="${KODEX_SMOKE_ENV_FILE:-${PROJECT_ROOT}/bootstrap/host/config.env}"
  RENDER_DIR="${KODEX_SMOKE_RENDER_DIR:-}"
  KUBECONFIG_PATH="${KUBECONFIG:-}"
  ROLL_OUT_TIMEOUT="${KODEX_ROLLOUT_TIMEOUT:-300s}"
  RESTART_DEPLOYMENT="${KODEX_SMOKE_RESTART_DEPLOYMENT:-true}"
  KEEP_RENDER_DIR="${KODEX_SMOKE_KEEP_RENDER_DIR:-false}"

  if [[ ! -f "$ENV_FILE" ]]; then
    echo "${KODEX_SMOKE_SCRIPT_NAME}: env file not found: ${ENV_FILE}" >&2
    exit 1
  fi

  # shellcheck disable=SC1090
  source "$ENV_FILE"
  # shellcheck disable=SC1091
  source "${PROJECT_ROOT}/scripts/lib/inventory.sh"

  if [[ -z "$RENDER_DIR" ]]; then
    RENDER_DIR="$(mktemp -d)"
    KODEX_SMOKE_RENDER_DIR_IS_TEMP="true"
  else
    KODEX_SMOKE_RENDER_DIR_IS_TEMP="false"
  fi

  KODEX_SMOKE_RENDER_ENV_FILE="$(mktemp)"
  KODEX_SMOKE_PORT_FORWARD_LOGS=()
  KODEX_SMOKE_PORT_FORWARD_PIDS=()
  KODEX_SMOKE_NAMESPACE="${KODEX_PRODUCTION_NAMESPACE:-kodex-prod}"
  KODEX_SMOKE_KUBECTL_ARGS=()
  if [[ -n "$KUBECONFIG_PATH" ]]; then
    KODEX_SMOKE_KUBECTL_ARGS+=(--kubeconfig "$KUBECONFIG_PATH")
  fi

  trap kodex_smoke_cleanup EXIT
}

kodex_smoke_cleanup() {
  local pid
  for pid in "${KODEX_SMOKE_PORT_FORWARD_PIDS[@]:-}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
  local log_file
  for log_file in "${KODEX_SMOKE_PORT_FORWARD_LOGS[@]:-}"; do
    rm -f "$log_file"
  done
  rm -f "${KODEX_SMOKE_RENDER_ENV_FILE:-}"
  if [[ "${KODEX_SMOKE_RENDER_DIR_IS_TEMP:-false}" == "true" && "${KEEP_RENDER_DIR:-false}" != "true" ]]; then
    rm -rf "${RENDER_DIR:-}"
  fi
}

kodex_smoke_require_commands() {
  local command_name
  for command_name in "$@"; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
      echo "${KODEX_SMOKE_SCRIPT_NAME}: ${command_name} is required" >&2
      exit 1
    fi
  done
}

kodex_smoke_require_values() {
  local missing_values=()
  local name
  for name in "$@"; do
    if [[ -z "${!name:-}" ]]; then
      missing_values+=("$name")
    fi
  done
  if (( ${#missing_values[@]} > 0 )); then
    echo "${KODEX_SMOKE_SCRIPT_NAME}: normalized bootstrap env is required before render" >&2
    echo "missing values: ${missing_values[*]}" >&2
    echo "use KODEX_SMOKE_ENV_FILE with generated bootstrap.env from bootstrap/host/bootstrap_remote_production.sh" >&2
    exit 1
  fi
}

kodex_smoke_require_images() {
  local required_names="$1"
  shift

  local image
  for image in "$@"; do
    if [[ -z "$image" ]]; then
      echo "${KODEX_SMOKE_SCRIPT_NAME}: image variables must be populated before apply" >&2
      echo "required: ${required_names}" >&2
      exit 1
    fi
  done
}

kodex_smoke_render() {
  cp "$ENV_FILE" "$KODEX_SMOKE_RENDER_ENV_FILE"

  local name
  for name in "$@"; do
    printf "%s='%s'\n" "$name" "$(kodex_smoke_escape_squote "${!name:-}")" >>"$KODEX_SMOKE_RENDER_ENV_FILE"
  done

  if [[ "$KODEX_SMOKE_RENDER_DIR_IS_TEMP" != "true" ]]; then
    rm -rf "$RENDER_DIR"
  fi
  go run "${PROJECT_ROOT}/cmd/manifest-render" \
    --env-file "$KODEX_SMOKE_RENDER_ENV_FILE" \
    --source "${PROJECT_ROOT}/deploy/base" \
    --output "$RENDER_DIR"
}

kodex_kubectl() {
  kubectl "${KODEX_SMOKE_KUBECTL_ARGS[@]}" "$@"
}

kodex_smoke_delete_jobs() {
  local job_name
  for job_name in "$@"; do
    kodex_kubectl -n "$KODEX_SMOKE_NAMESPACE" delete job "$job_name" --ignore-not-found
  done
}

kodex_smoke_apply_foundation() {
  kodex_kubectl create namespace "$KODEX_SMOKE_NAMESPACE" --dry-run=client -o yaml | kodex_kubectl apply -f -
  kodex_smoke_delete_jobs kodex-postgres-bootstrap-databases platform-event-log-migrations
  kodex_kubectl apply -k "${RENDER_DIR}/postgres"
  kodex_kubectl -n "$KODEX_SMOKE_NAMESPACE" rollout status statefulset/postgres --timeout="$ROLL_OUT_TIMEOUT"
  kodex_kubectl -n "$KODEX_SMOKE_NAMESPACE" wait --for=condition=complete job/kodex-postgres-bootstrap-databases --timeout="$ROLL_OUT_TIMEOUT"
  kodex_kubectl apply -f "${RENDER_DIR}/platform-event-log/migrations.yaml"
  kodex_kubectl -n "$KODEX_SMOKE_NAMESPACE" wait --for=condition=complete job/platform-event-log-migrations --timeout="$ROLL_OUT_TIMEOUT"
}

kodex_smoke_apply_migrations() {
  local service_dir="$1"
  local job_name="$2"

  kodex_smoke_delete_jobs "$job_name"
  kodex_kubectl apply -f "${RENDER_DIR}/${service_dir}/migrations.yaml"
  kodex_kubectl -n "$KODEX_SMOKE_NAMESPACE" wait --for=condition=complete "job/${job_name}" --timeout="$ROLL_OUT_TIMEOUT"
}

kodex_smoke_apply_deployment() {
  local service_name="$1"
  local manifest_path="$2"

  kodex_kubectl apply -f "${RENDER_DIR}/${manifest_path}"
  if [[ "$RESTART_DEPLOYMENT" == "true" ]]; then
    kodex_kubectl -n "$KODEX_SMOKE_NAMESPACE" rollout restart "deployment/${service_name}"
  fi
  kodex_kubectl -n "$KODEX_SMOKE_NAMESPACE" rollout status "deployment/${service_name}" --timeout="$ROLL_OUT_TIMEOUT"
}

kodex_smoke_start_port_forward() {
  local resource="$1"
  local forward="$2"
  local log_file
  log_file="$(mktemp)"
  kodex_kubectl -n "$KODEX_SMOKE_NAMESPACE" port-forward "$resource" "$forward" >"$log_file" 2>&1 &
  KODEX_SMOKE_LAST_PORT_FORWARD_PID="$!"
  KODEX_SMOKE_LAST_PORT_FORWARD_LOG="$log_file"
  KODEX_SMOKE_PORT_FORWARD_PIDS+=("$KODEX_SMOKE_LAST_PORT_FORWARD_PID")
  KODEX_SMOKE_PORT_FORWARD_LOGS+=("$KODEX_SMOKE_LAST_PORT_FORWARD_LOG")
}

kodex_smoke_check_readyz() {
  local service_name="$1"
  local local_port="$2"

  kodex_smoke_start_port_forward "svc/${service_name}" "${local_port}:8080"
  local attempt
  for attempt in $(seq 1 30); do
    if curl -fsS "http://127.0.0.1:${local_port}/health/readyz" >/dev/null; then
      echo "${KODEX_SMOKE_SCRIPT_NAME}: readyz OK"
      return
    fi
    sleep 1
  done

  cat "$KODEX_SMOKE_LAST_PORT_FORWARD_LOG" >&2 || true
  echo "${KODEX_SMOKE_SCRIPT_NAME}: readyz did not become healthy" >&2
  exit 1
}
