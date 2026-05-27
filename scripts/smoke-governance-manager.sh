#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/smoke.sh"

kodex_smoke_init "smoke-governance-manager"
kodex_smoke_require_commands go kubectl curl

KODEX_POSTGRES_IMAGE="$(kodex_postgres_image)"
KODEX_ACCESS_MANAGER_IMAGE="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_IMAGE KODEX_ACCESS_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/access-manager KODEX_ACCESS_MANAGER_VERSION access-manager)"
KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE KODEX_ACCESS_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/access-manager-migrations KODEX_ACCESS_MANAGER_VERSION access-manager)"
KODEX_GOVERNANCE_MANAGER_IMAGE="$(kodex_image_from_repo KODEX_GOVERNANCE_MANAGER_IMAGE KODEX_GOVERNANCE_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/governance-manager KODEX_GOVERNANCE_MANAGER_VERSION governance-manager)"
KODEX_GOVERNANCE_MANAGER_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_GOVERNANCE_MANAGER_MIGRATIONS_IMAGE KODEX_GOVERNANCE_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/governance-manager-migrations KODEX_GOVERNANCE_MANAGER_VERSION governance-manager)"
KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/platform-event-log-migrations KODEX_PLATFORM_EVENT_LOG_VERSION platform-event-log)"

required_runtime_values=(
  KODEX_POSTGRES_PASSWORD
  KODEX_ACCESS_MANAGER_DATABASE_DSN
  KODEX_GOVERNANCE_MANAGER_DATABASE_DSN
  KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN
  KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN
  KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_DSN
)
if [[ "${KODEX_ACCESS_MANAGER_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_GOVERNANCE_MANAGER_ACCESS_CHECK_ENABLED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_GOVERNANCE_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN)
fi
kodex_smoke_require_values "${required_runtime_values[@]}"

kodex_smoke_require_images \
  "KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_IMAGE, KODEX_GOVERNANCE_MANAGER_MIGRATIONS_IMAGE, KODEX_GOVERNANCE_MANAGER_IMAGE" \
  "$KODEX_POSTGRES_IMAGE" \
  "$KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE" \
  "$KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE" \
  "$KODEX_ACCESS_MANAGER_IMAGE" \
  "$KODEX_GOVERNANCE_MANAGER_MIGRATIONS_IMAGE" \
  "$KODEX_GOVERNANCE_MANAGER_IMAGE"

kodex_smoke_render \
  KODEX_POSTGRES_IMAGE \
  KODEX_ACCESS_MANAGER_IMAGE \
  KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE \
  KODEX_GOVERNANCE_MANAGER_IMAGE \
  KODEX_GOVERNANCE_MANAGER_MIGRATIONS_IMAGE \
  KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE

kodex_smoke_apply_foundation
kodex_smoke_apply_migrations access-manager access-manager-migrations
kodex_smoke_apply_deployment access-manager access-manager/access-manager.yaml
kodex_smoke_apply_migrations governance-manager governance-manager-migrations
kodex_smoke_apply_deployment governance-manager governance-manager/governance-manager.yaml
kodex_smoke_check_readyz governance-manager 18086
