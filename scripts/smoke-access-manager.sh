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
  echo "smoke-access-manager: env file not found: $ENV_FILE" >&2
  exit 1
fi

# shellcheck disable=SC1090
source "$ENV_FILE"

if [[ -z "$RENDER_DIR" ]]; then
  RENDER_DIR="$(mktemp -d)"
  render_dir_is_temp="true"
else
  render_dir_is_temp="false"
fi
render_env_file="$(mktemp)"
port_forward_log=""
port_forward_pid=""

cleanup() {
  if [[ -n "$port_forward_pid" ]]; then
    kill "$port_forward_pid" >/dev/null 2>&1 || true
  fi
  if [[ -n "$port_forward_log" ]]; then
    rm -f "$port_forward_log"
  fi
  rm -f "$render_env_file"
  if [[ "$render_dir_is_temp" == "true" && "$KEEP_RENDER_DIR" != "true" ]]; then
    rm -rf "$RENDER_DIR"
  fi
}
trap cleanup EXIT

namespace="${KODEX_PRODUCTION_NAMESPACE:-kodex-prod}"
internal_registry_host="${KODEX_INTERNAL_REGISTRY_HOST:-127.0.0.1:5000}"
access_repo="${KODEX_ACCESS_MANAGER_INTERNAL_IMAGE_REPOSITORY:-kodex/access-manager}"
access_migrations_repo="${KODEX_ACCESS_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY:-kodex/access-manager-migrations}"
event_log_migrations_repo="${KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY:-kodex/platform-event-log-migrations}"

KODEX_ACCESS_MANAGER_IMAGE="${KODEX_ACCESS_MANAGER_IMAGE:-${internal_registry_host}/${access_repo}:latest}"
KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE="${KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE:-${internal_registry_host}/${access_migrations_repo}:latest}"
KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE="${KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE:-${internal_registry_host}/${event_log_migrations_repo}:latest}"

required_runtime_values=(
  KODEX_POSTGRES_PASSWORD
  KODEX_ACCESS_MANAGER_DATABASE_DSN
  KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN
  KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN
)
if [[ "${KODEX_ACCESS_MANAGER_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN)
fi
missing_runtime_values=()
for name in "${required_runtime_values[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    missing_runtime_values+=("$name")
  fi
done
if (( ${#missing_runtime_values[@]} > 0 )); then
  echo "smoke-access-manager: normalized bootstrap env is required before render" >&2
  echo "missing values: ${missing_runtime_values[*]}" >&2
  echo "use KODEX_SMOKE_ENV_FILE with generated bootstrap.env from bootstrap/host/bootstrap_remote_production.sh" >&2
  exit 1
fi

required_images=(
  "${KODEX_POSTGRES_IMAGE:-pgvector/pgvector:pg16}"
  "$KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE"
  "$KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE"
  "$KODEX_ACCESS_MANAGER_IMAGE"
)

for image in "${required_images[@]}"; do
  if [[ -z "$image" ]]; then
    echo "smoke-access-manager: image variables must be populated before apply" >&2
    echo "required: KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_IMAGE" >&2
    exit 1
  fi
done

kubectl_args=()
if [[ -n "$KUBECONFIG_PATH" ]]; then
  kubectl_args+=(--kubeconfig "$KUBECONFIG_PATH")
fi

cp "$ENV_FILE" "$render_env_file"
{
  printf "KODEX_ACCESS_MANAGER_IMAGE='%s'\n" "$KODEX_ACCESS_MANAGER_IMAGE"
  printf "KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE='%s'\n" "$KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE"
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

port_forward_log="$(mktemp)"
kubectl "${kubectl_args[@]}" -n "$namespace" port-forward svc/access-manager 18080:8080 >"$port_forward_log" 2>&1 &
port_forward_pid="$!"

for _ in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:18080/health/readyz >/dev/null; then
    echo "smoke-access-manager: readyz OK"
    exit 0
  fi
  sleep 1
done

cat "$port_forward_log" >&2 || true
echo "smoke-access-manager: readyz did not become healthy" >&2
exit 1
