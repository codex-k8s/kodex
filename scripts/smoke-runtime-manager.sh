#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/smoke.sh"

kodex_smoke_init "smoke-runtime-manager"
kodex_smoke_require_commands go kubectl curl grpcurl

KODEX_POSTGRES_IMAGE="$(kodex_postgres_image)"
KODEX_ACCESS_MANAGER_IMAGE="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_IMAGE KODEX_ACCESS_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/access-manager KODEX_ACCESS_MANAGER_VERSION access-manager)"
KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE KODEX_ACCESS_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/access-manager-migrations KODEX_ACCESS_MANAGER_VERSION access-manager)"
KODEX_RUNTIME_MANAGER_IMAGE="$(kodex_image_from_repo KODEX_RUNTIME_MANAGER_IMAGE KODEX_RUNTIME_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/runtime-manager KODEX_RUNTIME_MANAGER_VERSION runtime-manager)"
KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE KODEX_RUNTIME_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/runtime-manager-migrations KODEX_RUNTIME_MANAGER_VERSION runtime-manager)"
KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/platform-event-log-migrations KODEX_PLATFORM_EVENT_LOG_VERSION platform-event-log)"

required_runtime_values=(
  KODEX_POSTGRES_PASSWORD
  KODEX_ACCESS_MANAGER_DATABASE_DSN
  KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN
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
required_runtime_values+=(KODEX_RUNTIME_MANAGER_FLEET_MANAGER_GRPC_AUTH_TOKEN)
kodex_smoke_require_values "${required_runtime_values[@]}"

kodex_smoke_require_images \
  "KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_IMAGE, KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE, KODEX_RUNTIME_MANAGER_IMAGE" \
  "$KODEX_POSTGRES_IMAGE" \
  "$KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE" \
  "$KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE" \
  "$KODEX_ACCESS_MANAGER_IMAGE" \
  "$KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE" \
  "$KODEX_RUNTIME_MANAGER_IMAGE"

kodex_smoke_render \
  KODEX_POSTGRES_IMAGE \
  KODEX_ACCESS_MANAGER_IMAGE \
  KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE \
  KODEX_RUNTIME_MANAGER_IMAGE \
  KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE \
  KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE

kodex_smoke_apply_foundation
kodex_smoke_apply_migrations access-manager access-manager-migrations
kodex_smoke_apply_deployment access-manager access-manager/access-manager.yaml
kodex_smoke_apply_migrations runtime-manager runtime-manager-migrations
kodex_smoke_apply_deployment runtime-manager runtime-manager/runtime-manager.yaml
kodex_smoke_check_readyz runtime-manager 18082

kodex_smoke_start_port_forward "svc/runtime-manager" "19092:9090"
grpc_payload='{"slot_id":"00000000-0000-0000-0000-000000000001","meta":{"actor":{"type":"service","id":"smoke-runtime-manager"},"request_id":"smoke-runtime-manager","request_context":{"source":"smoke-runtime-manager"}}}'
grpc_output=""
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

cat "$KODEX_SMOKE_LAST_PORT_FORWARD_LOG" >&2 || true
printf '%s\n' "$grpc_output" >&2
echo "smoke-runtime-manager: gRPC boundary did not respond with an application status" >&2
exit 1
