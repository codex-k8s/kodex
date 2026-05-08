#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${KODEX_BUILD_ENV_FILE:-${PROJECT_ROOT}/bootstrap/host/config.env}"
IMAGE_TAR="${KODEX_BUILD_IMAGE_TAR:-${PROJECT_ROOT}/.local/build/runtime-manager-smoke-images.tar}"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "build-runtime-manager-images: env file not found: $ENV_FILE" >&2
  exit 1
fi

# shellcheck disable=SC1090
source "$ENV_FILE"
# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/inventory.sh"

access_image="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_IMAGE KODEX_ACCESS_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/access-manager KODEX_ACCESS_MANAGER_VERSION access-manager)"
access_migrations_image="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE KODEX_ACCESS_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/access-manager-migrations KODEX_ACCESS_MANAGER_VERSION access-manager)"
runtime_image="$(kodex_image_from_repo KODEX_RUNTIME_MANAGER_IMAGE KODEX_RUNTIME_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/runtime-manager KODEX_RUNTIME_MANAGER_VERSION runtime-manager)"
runtime_migrations_image="$(kodex_image_from_repo KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE KODEX_RUNTIME_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/runtime-manager-migrations KODEX_RUNTIME_MANAGER_VERSION runtime-manager)"
event_log_migrations_image="$(kodex_image_from_repo KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/platform-event-log-migrations KODEX_PLATFORM_EVENT_LOG_VERSION platform-event-log)"
golang_image="$(kodex_golang_image)"

docker build \
  --build-arg "GOLANG_IMAGE=${golang_image}" \
  --target prod \
  --tag "$access_image" \
  --file "${PROJECT_ROOT}/services/internal/access-manager/Dockerfile" \
  "$PROJECT_ROOT"

docker build \
  --build-arg "GOLANG_IMAGE=${golang_image}" \
  --target migrations \
  --tag "$access_migrations_image" \
  --file "${PROJECT_ROOT}/services/internal/access-manager/Dockerfile" \
  "$PROJECT_ROOT"

docker build \
  --build-arg "GOLANG_IMAGE=${golang_image}" \
  --target prod \
  --tag "$runtime_image" \
  --file "${PROJECT_ROOT}/services/internal/runtime-manager/Dockerfile" \
  "$PROJECT_ROOT"

docker build \
  --build-arg "GOLANG_IMAGE=${golang_image}" \
  --target migrations \
  --tag "$runtime_migrations_image" \
  --file "${PROJECT_ROOT}/services/internal/runtime-manager/Dockerfile" \
  "$PROJECT_ROOT"

docker build \
  --build-arg "GOLANG_IMAGE=${golang_image}" \
  --target prod \
  --tag "$event_log_migrations_image" \
  --file "${PROJECT_ROOT}/services/internal/platform-event-log/Dockerfile" \
  "$PROJECT_ROOT"

mkdir -p "$(dirname "$IMAGE_TAR")"
docker save \
  --output "$IMAGE_TAR" \
  "$access_image" \
  "$access_migrations_image" \
  "$runtime_image" \
  "$runtime_migrations_image" \
  "$event_log_migrations_image"

echo "build-runtime-manager-images: saved $IMAGE_TAR"
