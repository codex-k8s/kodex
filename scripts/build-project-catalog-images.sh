#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${KODEX_BUILD_ENV_FILE:-${PROJECT_ROOT}/bootstrap/host/config.env}"
IMAGE_TAR="${KODEX_BUILD_IMAGE_TAR:-${PROJECT_ROOT}/.local/build/project-catalog-smoke-images.tar}"

# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/build_images.sh"

kodex_build_require_env_file "build-project-catalog-images" "$ENV_FILE"

# shellcheck disable=SC1090
source "$ENV_FILE"
# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/inventory.sh"

access_image="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_IMAGE KODEX_ACCESS_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/access-manager KODEX_ACCESS_MANAGER_VERSION access-manager)"
access_migrations_image="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE KODEX_ACCESS_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/access-manager-migrations KODEX_ACCESS_MANAGER_VERSION access-manager)"
project_image="$(kodex_image_from_repo KODEX_PROJECT_CATALOG_IMAGE KODEX_PROJECT_CATALOG_INTERNAL_IMAGE_REPOSITORY kodex/project-catalog KODEX_PROJECT_CATALOG_VERSION project-catalog)"
project_migrations_image="$(kodex_image_from_repo KODEX_PROJECT_CATALOG_MIGRATIONS_IMAGE KODEX_PROJECT_CATALOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/project-catalog-migrations KODEX_PROJECT_CATALOG_VERSION project-catalog)"
event_log_migrations_image="$(kodex_image_from_repo KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/platform-event-log-migrations KODEX_PLATFORM_EVENT_LOG_VERSION platform-event-log)"
golang_image="$(kodex_golang_image)"

kodex_build_go_images "build-project-catalog-images" "$IMAGE_TAR" "$golang_image" \
  "$access_image" prod services/internal/access-manager/Dockerfile \
  "$access_migrations_image" migrations services/internal/access-manager/Dockerfile \
  "$project_image" prod services/internal/project-catalog/Dockerfile \
  "$project_migrations_image" migrations services/internal/project-catalog/Dockerfile \
  "$event_log_migrations_image" prod services/internal/platform-event-log/Dockerfile
