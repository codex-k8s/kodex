#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/smoke.sh"

kodex_smoke_init "smoke-interaction-hub"
kodex_smoke_require_commands go kubectl curl

KODEX_POSTGRES_IMAGE="$(kodex_postgres_image)"
KODEX_INTERACTION_HUB_IMAGE="$(kodex_image_from_repo KODEX_INTERACTION_HUB_IMAGE KODEX_INTERACTION_HUB_INTERNAL_IMAGE_REPOSITORY kodex/interaction-hub KODEX_INTERACTION_HUB_VERSION interaction-hub)"
KODEX_INTERACTION_HUB_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_INTERACTION_HUB_MIGRATIONS_IMAGE KODEX_INTERACTION_HUB_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/interaction-hub-migrations KODEX_INTERACTION_HUB_VERSION interaction-hub)"
KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/platform-event-log-migrations KODEX_PLATFORM_EVENT_LOG_VERSION platform-event-log)"

required_runtime_values=(
  KODEX_POSTGRES_PASSWORD
  KODEX_INTERACTION_HUB_DATABASE_DSN
  KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN
  KODEX_INTERACTION_HUB_EVENT_LOG_DATABASE_DSN
)
if [[ "${KODEX_INTERACTION_HUB_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_INTERACTION_HUB_GRPC_AUTH_TOKEN)
fi
kodex_smoke_require_values "${required_runtime_values[@]}"

kodex_smoke_require_images \
  "KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE, KODEX_INTERACTION_HUB_MIGRATIONS_IMAGE, KODEX_INTERACTION_HUB_IMAGE" \
  "$KODEX_POSTGRES_IMAGE" \
  "$KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE" \
  "$KODEX_INTERACTION_HUB_MIGRATIONS_IMAGE" \
  "$KODEX_INTERACTION_HUB_IMAGE"

kodex_smoke_render \
  KODEX_POSTGRES_IMAGE \
  KODEX_INTERACTION_HUB_IMAGE \
  KODEX_INTERACTION_HUB_MIGRATIONS_IMAGE \
  KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE

kodex_smoke_apply_foundation
kodex_smoke_apply_migrations interaction-hub interaction-hub-migrations
kodex_smoke_apply_deployment interaction-hub interaction-hub/interaction-hub.yaml
kodex_smoke_check_readyz interaction-hub "${KODEX_INTERACTION_HUB_SMOKE_PORT:-18087}"
