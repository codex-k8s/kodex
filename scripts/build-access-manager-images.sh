#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${KODEX_BUILD_ENV_FILE:-${PROJECT_ROOT}/bootstrap/host/config.env}"
GOLANG_IMAGE="${KODEX_BUILD_GOLANG_IMAGE:-golang:1.25.8-alpine}"
IMAGE_TAR="${KODEX_BUILD_IMAGE_TAR:-${PROJECT_ROOT}/.local/build/access-manager-smoke-images.tar}"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "build-access-manager-images: env file not found: $ENV_FILE" >&2
  exit 1
fi

# shellcheck disable=SC1090
source "$ENV_FILE"

internal_registry_host="${KODEX_INTERNAL_REGISTRY_HOST:-127.0.0.1:5000}"
access_repo="${KODEX_ACCESS_MANAGER_INTERNAL_IMAGE_REPOSITORY:-kodex/access-manager}"
access_migrations_repo="${KODEX_ACCESS_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY:-kodex/access-manager-migrations}"
event_log_migrations_repo="${KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY:-kodex/platform-event-log-migrations}"

access_image="${KODEX_ACCESS_MANAGER_IMAGE:-${internal_registry_host}/${access_repo}:latest}"
access_migrations_image="${KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE:-${internal_registry_host}/${access_migrations_repo}:latest}"
event_log_migrations_image="${KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE:-${internal_registry_host}/${event_log_migrations_repo}:latest}"

docker build \
  --build-arg "GOLANG_IMAGE=${GOLANG_IMAGE}" \
  --target prod \
  --tag "$access_image" \
  --file "${PROJECT_ROOT}/services/internal/access-manager/Dockerfile" \
  "$PROJECT_ROOT"

docker build \
  --build-arg "GOLANG_IMAGE=${GOLANG_IMAGE}" \
  --target migrations \
  --tag "$access_migrations_image" \
  --file "${PROJECT_ROOT}/services/internal/access-manager/Dockerfile" \
  "$PROJECT_ROOT"

docker build \
  --build-arg "GOLANG_IMAGE=${GOLANG_IMAGE}" \
  --target prod \
  --tag "$event_log_migrations_image" \
  --file "${PROJECT_ROOT}/services/internal/platform-event-log/Dockerfile" \
  "$PROJECT_ROOT"

mkdir -p "$(dirname "$IMAGE_TAR")"
docker save \
  --output "$IMAGE_TAR" \
  "$access_image" \
  "$access_migrations_image" \
  "$event_log_migrations_image"

echo "build-access-manager-images: saved $IMAGE_TAR"
