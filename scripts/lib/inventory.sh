#!/usr/bin/env bash

if [[ -z "${PROJECT_ROOT:-}" ]]; then
  echo "scripts/lib/inventory.sh: PROJECT_ROOT is required before sourcing" >&2
  exit 1
fi

# Minimal shell fallback for legacy smoke/build wrappers. The authoritative
# parser and template helpers live in libs/go/stackinventory and
# cmd/manifest-render; keep this file limited to pre-Go-wrapper image strings.
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

kodex_internal_registry_host() {
  printf '%s\n' "${KODEX_INTERNAL_REGISTRY_HOST:-127.0.0.1:5000}"
}

kodex_version() {
  local env_name="$1"
  local inventory_key="$2"
  local override="${!env_name:-}"
  if [[ -n "$override" ]]; then
    printf '%s\n' "$override"
    return
  fi
  inventory_version "$inventory_key"
}

kodex_image_from_repo() {
  local image_env="$1"
  local repository_env="$2"
  local default_repository="$3"
  local version_env="$4"
  local version_key="$5"
  local image="${!image_env:-}"
  if [[ -n "$image" ]]; then
    printf '%s\n' "$image"
    return
  fi
  local repository="${!repository_env:-$default_repository}"
  local version
  version="$(kodex_version "$version_env" "$version_key")"
  printf '%s/%s:%s\n' "$(kodex_internal_registry_host)" "$repository" "$version"
}

kodex_golang_image() {
  local image="${KODEX_BUILD_GOLANG_IMAGE:-}"
  if [[ -n "$image" ]]; then
    printf '%s\n' "$image"
    return
  fi
  local version
  version="$(kodex_version KODEX_GOLANG_ALPINE_VERSION golang-alpine)"
  printf '%s/kodex/mirror/golang:%s\n' "$(kodex_internal_registry_host)" "$version"
}

kodex_postgres_image() {
  local image="${KODEX_POSTGRES_IMAGE:-}"
  if [[ -n "$image" ]]; then
    printf '%s\n' "$image"
    return
  fi
  local version
  version="$(kodex_version KODEX_PGVECTOR_VERSION pgvector)"
  printf '%s/kodex/mirror/pgvector:%s\n' "$(kodex_internal_registry_host)" "$version"
}
