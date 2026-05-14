#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/smoke.sh"

kodex_smoke_init "smoke-provider-hub"
kodex_smoke_require_commands go kubectl curl grpcurl

KODEX_POSTGRES_IMAGE="$(kodex_postgres_image)"
KODEX_ACCESS_MANAGER_IMAGE="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_IMAGE KODEX_ACCESS_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/access-manager KODEX_ACCESS_MANAGER_VERSION access-manager)"
KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE KODEX_ACCESS_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/access-manager-migrations KODEX_ACCESS_MANAGER_VERSION access-manager)"
KODEX_PROVIDER_HUB_IMAGE="$(kodex_image_from_repo KODEX_PROVIDER_HUB_IMAGE KODEX_PROVIDER_HUB_INTERNAL_IMAGE_REPOSITORY kodex/provider-hub KODEX_PROVIDER_HUB_VERSION provider-hub)"
KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE KODEX_PROVIDER_HUB_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/provider-hub-migrations KODEX_PROVIDER_HUB_VERSION provider-hub)"
KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/platform-event-log-migrations KODEX_PLATFORM_EVENT_LOG_VERSION platform-event-log)"

required_runtime_values=(
  KODEX_POSTGRES_PASSWORD
  KODEX_ACCESS_MANAGER_DATABASE_DSN
  KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN
  KODEX_PROVIDER_HUB_DATABASE_DSN
  KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN
  KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_DSN
  KODEX_PROVIDER_HUB_ACCESS_MANAGER_GRPC_AUTH_TOKEN
)
if [[ "${KODEX_ACCESS_MANAGER_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_PROVIDER_HUB_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN)
fi
kodex_smoke_require_values "${required_runtime_values[@]}"

kodex_smoke_require_images \
  "KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_IMAGE, KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE, KODEX_PROVIDER_HUB_IMAGE" \
  "$KODEX_POSTGRES_IMAGE" \
  "$KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE" \
  "$KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE" \
  "$KODEX_ACCESS_MANAGER_IMAGE" \
  "$KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE" \
  "$KODEX_PROVIDER_HUB_IMAGE"

kodex_smoke_render \
  KODEX_POSTGRES_IMAGE \
  KODEX_ACCESS_MANAGER_IMAGE \
  KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE \
  KODEX_PROVIDER_HUB_IMAGE \
  KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE \
  KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE

kodex_smoke_apply_foundation
kodex_smoke_apply_migrations access-manager access-manager-migrations
kodex_smoke_apply_deployment access-manager access-manager/access-manager.yaml
kodex_smoke_apply_migrations provider-hub provider-hub-migrations
kodex_smoke_apply_deployment provider-hub provider-hub/provider-hub.yaml
kodex_smoke_check_readyz provider-hub 18085

kodex_smoke_start_port_forward "svc/provider-hub" "19095:9090"
grpc_payload='{"meta":{"actor":{"type":"service","id":"smoke-provider-hub"},"request_id":"smoke-provider-hub","request_context":{"source":"smoke-provider-hub"}},"page":{"page_size":1}}'
call_provider_hub() {
  local token="$1"
  local grpc_headers=(
    -H "x-kodex-caller-type: service"
    -H "x-kodex-caller-id: smoke-provider-hub"
  )
  if [[ -n "$token" ]]; then
    grpc_headers+=(-H "authorization: Bearer ${token}")
  fi

  grpcurl \
    -plaintext \
    -proto "${PROJECT_ROOT}/proto/kodex/providers/v1/provider_hub.proto" \
    "${grpc_headers[@]}" \
    -d "$grpc_payload" \
    127.0.0.1:19095 \
    kodex.providers.v1.ProviderHubService/ListProviderOperations
}

positive_output=""
for _ in $(seq 1 30); do
  positive_output="$(call_provider_hub "${KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN:-}" 2>&1)" && positive_status="ok" || positive_status="$?"
  if [[ "$positive_status" == "ok" ]] || grep -Eq "Code: PermissionDenied" <<<"$positive_output"; then
    positive_status="accepted"
    break
  fi
  sleep 1
done

if [[ "$positive_status" != "accepted" ]]; then
  cat "$KODEX_SMOKE_LAST_PORT_FORWARD_LOG" >&2 || true
  printf '%s\n' "$positive_output" >&2
  echo "smoke-provider-hub: gRPC boundary did not accept the configured provider-hub token" >&2
  exit 1
fi

if [[ "${KODEX_PROVIDER_HUB_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  negative_output="$(call_provider_hub "invalid-smoke-token" 2>&1)" && negative_status="ok" || negative_status="$?"
  if [[ "$negative_status" == "ok" ]] || ! grep -Eq "Code: Unauthenticated" <<<"$negative_output"; then
    printf '%s\n' "$negative_output" >&2
    echo "smoke-provider-hub: invalid provider-hub token must be rejected with Unauthenticated" >&2
    exit 1
  fi
fi

echo "smoke-provider-hub: gRPC boundary OK"
