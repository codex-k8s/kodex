#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${KODEX_BUILD_ENV_FILE:-${PROJECT_ROOT}/bootstrap/host/config.env}"
IMAGE_TAR="${KODEX_BUILD_IMAGE_TAR:-${PROJECT_ROOT}/.local/build/agent-manager-smoke-images.tar}"

# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/build_images.sh"

kodex_build_require_env_file "build-agent-manager-images" "$ENV_FILE"

# shellcheck disable=SC1090
source "$ENV_FILE"
# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/inventory.sh"

access_image="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_IMAGE KODEX_ACCESS_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/access-manager KODEX_ACCESS_MANAGER_VERSION access-manager)"
access_migrations_image="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE KODEX_ACCESS_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/access-manager-migrations KODEX_ACCESS_MANAGER_VERSION access-manager)"
project_image="$(kodex_image_from_repo KODEX_PROJECT_CATALOG_IMAGE KODEX_PROJECT_CATALOG_INTERNAL_IMAGE_REPOSITORY kodex/project-catalog KODEX_PROJECT_CATALOG_VERSION project-catalog)"
project_migrations_image="$(kodex_image_from_repo KODEX_PROJECT_CATALOG_MIGRATIONS_IMAGE KODEX_PROJECT_CATALOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/project-catalog-migrations KODEX_PROJECT_CATALOG_VERSION project-catalog)"
package_image="$(kodex_image_from_repo KODEX_PACKAGE_HUB_IMAGE KODEX_PACKAGE_HUB_INTERNAL_IMAGE_REPOSITORY kodex/package-hub KODEX_PACKAGE_HUB_VERSION package-hub)"
package_migrations_image="$(kodex_image_from_repo KODEX_PACKAGE_HUB_MIGRATIONS_IMAGE KODEX_PACKAGE_HUB_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/package-hub-migrations KODEX_PACKAGE_HUB_VERSION package-hub)"
provider_image="$(kodex_image_from_repo KODEX_PROVIDER_HUB_IMAGE KODEX_PROVIDER_HUB_INTERNAL_IMAGE_REPOSITORY kodex/provider-hub KODEX_PROVIDER_HUB_VERSION provider-hub)"
provider_migrations_image="$(kodex_image_from_repo KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE KODEX_PROVIDER_HUB_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/provider-hub-migrations KODEX_PROVIDER_HUB_VERSION provider-hub)"
fleet_image="$(kodex_image_from_repo KODEX_FLEET_MANAGER_IMAGE KODEX_FLEET_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/fleet-manager KODEX_FLEET_MANAGER_VERSION fleet-manager)"
fleet_migrations_image="$(kodex_image_from_repo KODEX_FLEET_MANAGER_MIGRATIONS_IMAGE KODEX_FLEET_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/fleet-manager-migrations KODEX_FLEET_MANAGER_VERSION fleet-manager)"
runtime_image="$(kodex_image_from_repo KODEX_RUNTIME_MANAGER_IMAGE KODEX_RUNTIME_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/runtime-manager KODEX_RUNTIME_MANAGER_VERSION runtime-manager)"
runtime_migrations_image="$(kodex_image_from_repo KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE KODEX_RUNTIME_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/runtime-manager-migrations KODEX_RUNTIME_MANAGER_VERSION runtime-manager)"
agent_image="$(kodex_image_from_repo KODEX_AGENT_MANAGER_IMAGE KODEX_AGENT_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/agent-manager KODEX_AGENT_MANAGER_VERSION agent-manager)"
agent_migrations_image="$(kodex_image_from_repo KODEX_AGENT_MANAGER_MIGRATIONS_IMAGE KODEX_AGENT_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/agent-manager-migrations KODEX_AGENT_MANAGER_VERSION agent-manager)"
event_log_migrations_image="$(kodex_image_from_repo KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/platform-event-log-migrations KODEX_PLATFORM_EVENT_LOG_VERSION platform-event-log)"
golang_image="$(kodex_golang_image)"

kodex_build_go_images "build-agent-manager-images" "$IMAGE_TAR" "$golang_image" \
  "$access_image" prod services/internal/access-manager/Dockerfile \
  "$access_migrations_image" migrations services/internal/access-manager/Dockerfile \
  "$project_image" prod services/internal/project-catalog/Dockerfile \
  "$project_migrations_image" migrations services/internal/project-catalog/Dockerfile \
  "$package_image" prod services/internal/package-hub/Dockerfile \
  "$package_migrations_image" migrations services/internal/package-hub/Dockerfile \
  "$provider_image" prod services/internal/provider-hub/Dockerfile \
  "$provider_migrations_image" migrations services/internal/provider-hub/Dockerfile \
  "$fleet_image" prod services/internal/fleet-manager/Dockerfile \
  "$fleet_migrations_image" migrations services/internal/fleet-manager/Dockerfile \
  "$runtime_image" prod services/internal/runtime-manager/Dockerfile \
  "$runtime_migrations_image" migrations services/internal/runtime-manager/Dockerfile \
  "$agent_image" prod services/internal/agent-manager/Dockerfile \
  "$agent_migrations_image" migrations services/internal/agent-manager/Dockerfile \
  "$event_log_migrations_image" prod services/internal/platform-event-log/Dockerfile
