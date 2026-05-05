#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${KODEX_BUILD_ENV_FILE:-${PROJECT_ROOT}/bootstrap/host/config.env}"
IMAGE_TAR="${KODEX_BUILD_IMAGE_TAR:-${PROJECT_ROOT}/.local/build/access-manager-smoke-images.tar}"

inventory_version() {
  local key="$1"
  awk -v key="$key" '
    $0 ~ "^    " key ":" { found = 1; next }
    found && $1 == "value:" {
      value = $2
      gsub(/"/, "", value)
      print value
      exit
    }
    found && $0 ~ "^    [A-Za-z0-9_-]+:" { exit }
  ' "${PROJECT_ROOT}/services.yaml"
}

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
access_version="${KODEX_ACCESS_MANAGER_VERSION:-$(inventory_version access-manager)}"
event_log_version="${KODEX_PLATFORM_EVENT_LOG_VERSION:-$(inventory_version platform-event-log)}"
golang_version="${KODEX_GOLANG_ALPINE_VERSION:-$(inventory_version golang-alpine)}"

access_image="${KODEX_ACCESS_MANAGER_IMAGE:-${internal_registry_host}/${access_repo}:${access_version}}"
access_migrations_image="${KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE:-${internal_registry_host}/${access_migrations_repo}:${access_version}}"
event_log_migrations_image="${KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE:-${internal_registry_host}/${event_log_migrations_repo}:${event_log_version}}"
golang_image="${KODEX_BUILD_GOLANG_IMAGE:-golang:${golang_version}}"

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
