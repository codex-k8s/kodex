#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/smoke.sh"

kodex_smoke_init "smoke-package-hub"
kodex_smoke_require_commands go kubectl curl grpcurl

KODEX_POSTGRES_IMAGE="$(kodex_postgres_image)"
KODEX_ACCESS_MANAGER_IMAGE="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_IMAGE KODEX_ACCESS_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/access-manager KODEX_ACCESS_MANAGER_VERSION access-manager)"
KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE KODEX_ACCESS_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/access-manager-migrations KODEX_ACCESS_MANAGER_VERSION access-manager)"
KODEX_PACKAGE_HUB_IMAGE="$(kodex_image_from_repo KODEX_PACKAGE_HUB_IMAGE KODEX_PACKAGE_HUB_INTERNAL_IMAGE_REPOSITORY kodex/package-hub KODEX_PACKAGE_HUB_VERSION package-hub)"
KODEX_PACKAGE_HUB_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_PACKAGE_HUB_MIGRATIONS_IMAGE KODEX_PACKAGE_HUB_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/package-hub-migrations KODEX_PACKAGE_HUB_VERSION package-hub)"
KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/platform-event-log-migrations KODEX_PLATFORM_EVENT_LOG_VERSION platform-event-log)"

required_runtime_values=(
  KODEX_POSTGRES_PASSWORD
  KODEX_ACCESS_MANAGER_DATABASE_DSN
  KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN
  KODEX_PACKAGE_HUB_DATABASE_DSN
  KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN
  KODEX_PACKAGE_HUB_EVENT_LOG_DATABASE_DSN
)
if [[ "${KODEX_ACCESS_MANAGER_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_PACKAGE_HUB_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_PACKAGE_HUB_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_PACKAGE_HUB_ACCESS_CHECK_ENABLED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_PACKAGE_HUB_ACCESS_MANAGER_GRPC_AUTH_TOKEN)
fi
kodex_smoke_require_values "${required_runtime_values[@]}"

kodex_smoke_require_images \
  "KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_IMAGE, KODEX_PACKAGE_HUB_MIGRATIONS_IMAGE, KODEX_PACKAGE_HUB_IMAGE" \
  "$KODEX_POSTGRES_IMAGE" \
  "$KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE" \
  "$KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE" \
  "$KODEX_ACCESS_MANAGER_IMAGE" \
  "$KODEX_PACKAGE_HUB_MIGRATIONS_IMAGE" \
  "$KODEX_PACKAGE_HUB_IMAGE"

kodex_smoke_render \
  KODEX_POSTGRES_IMAGE \
  KODEX_ACCESS_MANAGER_IMAGE \
  KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE \
  KODEX_PACKAGE_HUB_IMAGE \
  KODEX_PACKAGE_HUB_MIGRATIONS_IMAGE \
  KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE

kodex_smoke_apply_foundation
kodex_smoke_apply_migrations access-manager access-manager-migrations
kodex_smoke_apply_deployment access-manager access-manager/access-manager.yaml
kodex_smoke_apply_migrations package-hub package-hub-migrations
kodex_smoke_apply_deployment package-hub package-hub/package-hub.yaml
kodex_smoke_check_readyz package-hub 18083

kodex_smoke_start_port_forward "svc/package-hub" "19093:9090"
grpc_payload='{"meta":{"actor":{"type":"service","id":"smoke-package-hub"},"request_id":"smoke-package-hub","request_context":{"source":"smoke-package-hub"}},"page":{"page_size":1}}'
call_package_hub() {
  local token="$1"
  local grpc_headers=(
    -H "x-kodex-caller-type: service"
    -H "x-kodex-caller-id: smoke-package-hub"
  )
  if [[ -n "$token" ]]; then
    grpc_headers+=(-H "authorization: Bearer ${token}")
  fi

  grpcurl \
    -plaintext \
    -proto "${PROJECT_ROOT}/proto/kodex/packages/v1/package_hub.proto" \
    "${grpc_headers[@]}" \
    -d "$grpc_payload" \
    127.0.0.1:19093 \
    kodex.packages.v1.PackageHubService/ListPackages
}

positive_output=""
for _ in $(seq 1 30); do
  positive_output="$(call_package_hub "${KODEX_PACKAGE_HUB_GRPC_AUTH_TOKEN:-}" 2>&1)" && positive_status="ok" || positive_status="$?"
  if [[ "$positive_status" == "ok" ]] || grep -Eq "Code: PermissionDenied" <<<"$positive_output"; then
    positive_status="accepted"
    break
  fi
  sleep 1
done

if [[ "$positive_status" != "accepted" ]]; then
  cat "$KODEX_SMOKE_LAST_PORT_FORWARD_LOG" >&2 || true
  printf '%s\n' "$positive_output" >&2
  echo "smoke-package-hub: gRPC boundary did not accept the configured package-hub token" >&2
  exit 1
fi

if [[ "${KODEX_PACKAGE_HUB_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  negative_output="$(call_package_hub "invalid-smoke-token" 2>&1)" && negative_status="ok" || negative_status="$?"
  if [[ "$negative_status" == "ok" ]] || ! grep -Eq "Code: Unauthenticated" <<<"$negative_output"; then
    printf '%s\n' "$negative_output" >&2
    echo "smoke-package-hub: invalid package-hub token must be rejected with Unauthenticated" >&2
    exit 1
  fi
fi

echo "smoke-package-hub: gRPC boundary OK"
