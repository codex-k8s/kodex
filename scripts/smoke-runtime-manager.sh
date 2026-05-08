#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${KODEX_SMOKE_ENV_FILE:-${PROJECT_ROOT}/bootstrap/host/config.env}"
RENDER_DIR="${KODEX_SMOKE_RENDER_DIR:-}"
KUBECONFIG_PATH="${KUBECONFIG:-}"
ROLL_OUT_TIMEOUT="${KODEX_ROLLOUT_TIMEOUT:-300s}"
RESTART_DEPLOYMENT="${KODEX_SMOKE_RESTART_DEPLOYMENT:-true}"
KEEP_RENDER_DIR="${KODEX_SMOKE_KEEP_RENDER_DIR:-false}"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "smoke-runtime-manager: env file not found: $ENV_FILE" >&2
  exit 1
fi
if ! command -v grpcurl >/dev/null 2>&1; then
  echo "smoke-runtime-manager: grpcurl is required for the gRPC boundary check" >&2
  exit 1
fi

# shellcheck disable=SC1090
source "$ENV_FILE"
# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/inventory.sh"

if [[ -z "$RENDER_DIR" ]]; then
  RENDER_DIR="$(mktemp -d)"
  render_dir_is_temp="true"
else
  render_dir_is_temp="false"
fi
render_env_file="$(mktemp)"
http_port_forward_log=""
http_port_forward_pid=""
grpc_port_forward_log=""
grpc_port_forward_pid=""

cleanup() {
  if [[ -n "$http_port_forward_pid" ]]; then
    kill "$http_port_forward_pid" >/dev/null 2>&1 || true
  fi
  if [[ -n "$grpc_port_forward_pid" ]]; then
    kill "$grpc_port_forward_pid" >/dev/null 2>&1 || true
  fi
  if [[ -n "$http_port_forward_log" ]]; then
    rm -f "$http_port_forward_log"
  fi
  if [[ -n "$grpc_port_forward_log" ]]; then
    rm -f "$grpc_port_forward_log"
  fi
  rm -f "$render_env_file"
  if [[ "$render_dir_is_temp" == "true" && "$KEEP_RENDER_DIR" != "true" ]]; then
    rm -rf "$RENDER_DIR"
  fi
}
trap cleanup EXIT

namespace="${KODEX_PRODUCTION_NAMESPACE:-kodex-prod}"
KODEX_POSTGRES_IMAGE="$(kodex_postgres_image)"
KODEX_ACCESS_MANAGER_IMAGE="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_IMAGE KODEX_ACCESS_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/access-manager KODEX_ACCESS_MANAGER_VERSION access-manager)"
KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE KODEX_ACCESS_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/access-manager-migrations KODEX_ACCESS_MANAGER_VERSION access-manager)"
KODEX_RUNTIME_MANAGER_IMAGE="$(kodex_image_from_repo KODEX_RUNTIME_MANAGER_IMAGE KODEX_RUNTIME_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/runtime-manager KODEX_RUNTIME_MANAGER_VERSION runtime-manager)"
KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE KODEX_RUNTIME_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/runtime-manager-migrations KODEX_RUNTIME_MANAGER_VERSION runtime-manager)"
KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/platform-event-log-migrations KODEX_PLATFORM_EVENT_LOG_VERSION platform-event-log)"

required_runtime_values=(
  KODEX_POSTGRES_PASSWORD
  KODEX_ACCESS_MANAGER_DATABASE_DSN
  KODEX_RUNTIME_MANAGER_DATABASE_DSN
  KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN
  KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_DSN
)
if [[ "${KODEX_ACCESS_MANAGER_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_RUNTIME_MANAGER_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_RUNTIME_MANAGER_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_RUNTIME_MANAGER_ACCESS_CHECK_ENABLED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_RUNTIME_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN)
fi
missing_runtime_values=()
for name in "${required_runtime_values[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    missing_runtime_values+=("$name")
  fi
done
if (( ${#missing_runtime_values[@]} > 0 )); then
  echo "smoke-runtime-manager: normalized bootstrap env is required before render" >&2
  echo "missing values: ${missing_runtime_values[*]}" >&2
  echo "use KODEX_SMOKE_ENV_FILE with generated bootstrap.env from bootstrap/host/bootstrap_remote_production.sh" >&2
  exit 1
fi

required_images=(
  "$KODEX_POSTGRES_IMAGE"
  "$KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE"
  "$KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE"
  "$KODEX_ACCESS_MANAGER_IMAGE"
  "$KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE"
  "$KODEX_RUNTIME_MANAGER_IMAGE"
)

for image in "${required_images[@]}"; do
  if [[ -z "$image" ]]; then
    echo "smoke-runtime-manager: image variables must be populated before apply" >&2
    echo "required: KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_IMAGE, KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE, KODEX_RUNTIME_MANAGER_IMAGE" >&2
    exit 1
  fi
done

kubectl_args=()
if [[ -n "$KUBECONFIG_PATH" ]]; then
  kubectl_args+=(--kubeconfig "$KUBECONFIG_PATH")
fi

cp "$ENV_FILE" "$render_env_file"
{
  printf "KODEX_POSTGRES_IMAGE='%s'\n" "$KODEX_POSTGRES_IMAGE"
  printf "KODEX_ACCESS_MANAGER_IMAGE='%s'\n" "$KODEX_ACCESS_MANAGER_IMAGE"
  printf "KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE='%s'\n" "$KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE"
  printf "KODEX_RUNTIME_MANAGER_IMAGE='%s'\n" "$KODEX_RUNTIME_MANAGER_IMAGE"
  printf "KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE='%s'\n" "$KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE"
  printf "KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE='%s'\n" "$KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE"
} >>"$render_env_file"

if [[ "$render_dir_is_temp" != "true" ]]; then
  rm -rf "$RENDER_DIR"
fi
go run "${PROJECT_ROOT}/cmd/manifest-render" \
  --env-file "$render_env_file" \
  --source "${PROJECT_ROOT}/deploy/base" \
  --output "$RENDER_DIR"

kubectl "${kubectl_args[@]}" create namespace "$namespace" --dry-run=client -o yaml | kubectl "${kubectl_args[@]}" apply -f -
kubectl "${kubectl_args[@]}" -n "$namespace" delete job kodex-postgres-bootstrap-databases --ignore-not-found
kubectl "${kubectl_args[@]}" -n "$namespace" delete job platform-event-log-migrations --ignore-not-found
kubectl "${kubectl_args[@]}" -n "$namespace" delete job access-manager-migrations --ignore-not-found
kubectl "${kubectl_args[@]}" -n "$namespace" delete job runtime-manager-migrations --ignore-not-found
kubectl "${kubectl_args[@]}" apply -k "${RENDER_DIR}/postgres"
kubectl "${kubectl_args[@]}" -n "$namespace" rollout status statefulset/postgres --timeout="$ROLL_OUT_TIMEOUT"
kubectl "${kubectl_args[@]}" -n "$namespace" wait --for=condition=complete job/kodex-postgres-bootstrap-databases --timeout="$ROLL_OUT_TIMEOUT"
kubectl "${kubectl_args[@]}" apply -f "${RENDER_DIR}/platform-event-log/migrations.yaml"
kubectl "${kubectl_args[@]}" -n "$namespace" wait --for=condition=complete job/platform-event-log-migrations --timeout="$ROLL_OUT_TIMEOUT"

kubectl "${kubectl_args[@]}" apply -f "${RENDER_DIR}/access-manager/migrations.yaml"
kubectl "${kubectl_args[@]}" -n "$namespace" wait --for=condition=complete job/access-manager-migrations --timeout="$ROLL_OUT_TIMEOUT"
kubectl "${kubectl_args[@]}" apply -f "${RENDER_DIR}/access-manager/access-manager.yaml"
if [[ "$RESTART_DEPLOYMENT" == "true" ]]; then
  kubectl "${kubectl_args[@]}" -n "$namespace" rollout restart deployment/access-manager
fi
kubectl "${kubectl_args[@]}" -n "$namespace" rollout status deployment/access-manager --timeout="$ROLL_OUT_TIMEOUT"

kubectl "${kubectl_args[@]}" apply -f "${RENDER_DIR}/runtime-manager/migrations.yaml"
kubectl "${kubectl_args[@]}" -n "$namespace" wait --for=condition=complete job/runtime-manager-migrations --timeout="$ROLL_OUT_TIMEOUT"
kubectl "${kubectl_args[@]}" apply -f "${RENDER_DIR}/runtime-manager/runtime-manager.yaml"
if [[ "$RESTART_DEPLOYMENT" == "true" ]]; then
  kubectl "${kubectl_args[@]}" -n "$namespace" rollout restart deployment/runtime-manager
fi
kubectl "${kubectl_args[@]}" -n "$namespace" rollout status deployment/runtime-manager --timeout="$ROLL_OUT_TIMEOUT"

http_port_forward_log="$(mktemp)"
kubectl "${kubectl_args[@]}" -n "$namespace" port-forward svc/runtime-manager 18082:8080 >"$http_port_forward_log" 2>&1 &
http_port_forward_pid="$!"

for _ in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:18082/health/readyz >/dev/null; then
    echo "smoke-runtime-manager: readyz OK"
    break
  fi
  sleep 1
done

if ! curl -fsS http://127.0.0.1:18082/health/readyz >/dev/null; then
  cat "$http_port_forward_log" >&2 || true
  echo "smoke-runtime-manager: readyz did not become healthy" >&2
  exit 1
fi

grpc_port_forward_log="$(mktemp)"
kubectl "${kubectl_args[@]}" -n "$namespace" port-forward svc/runtime-manager 19092:9090 >"$grpc_port_forward_log" 2>&1 &
grpc_port_forward_pid="$!"

grpc_payload='{"slot_id":"00000000-0000-0000-0000-000000000001","meta":{"actor":{"type":"service","id":"smoke-runtime-manager"},"request_id":"smoke-runtime-manager","request_context":{"source":"smoke-runtime-manager"}}}'
grpc_status=""
for _ in $(seq 1 30); do
  grpc_output="$(
    grpcurl \
      -plaintext \
      -proto "${PROJECT_ROOT}/proto/kodex/runtime/v1/runtime_manager.proto" \
      -H "authorization: Bearer invalid-smoke-token" \
      -H "x-kodex-caller-type: service" \
      -H "x-kodex-caller-id: smoke-runtime-manager" \
      -d "$grpc_payload" \
      127.0.0.1:19092 \
      kodex.runtime.v1.RuntimeManagerService/GetSlot 2>&1
  )" && grpc_status="ok" || grpc_status="$?"
  if [[ "$grpc_status" == "ok" ]] || grep -Eq "Code: (Unauthenticated|PermissionDenied|NotFound|InvalidArgument)" <<<"$grpc_output"; then
    echo "smoke-runtime-manager: gRPC boundary OK"
    exit 0
  fi
  sleep 1
done

cat "$grpc_port_forward_log" >&2 || true
printf '%s\n' "$grpc_output" >&2
echo "smoke-runtime-manager: gRPC boundary did not respond with an application status" >&2
exit 1
