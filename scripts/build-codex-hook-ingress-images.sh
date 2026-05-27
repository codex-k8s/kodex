#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${KODEX_BUILD_ENV_FILE:-}"
IMAGE_TAR="${KODEX_BUILD_IMAGE_TAR:-${PROJECT_ROOT}/.local/build/codex-hook-ingress-smoke-images.tar}"

# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/build_images.sh"

if [[ -n "$ENV_FILE" ]]; then
  kodex_build_require_env_file "build-codex-hook-ingress-images" "$ENV_FILE"
  # shellcheck disable=SC1090
  source "$ENV_FILE"
elif [[ -f "${PROJECT_ROOT}/bootstrap/host/config.env" ]]; then
  # shellcheck disable=SC1091
  source "${PROJECT_ROOT}/bootstrap/host/config.env"
fi

# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/inventory.sh"

codex_hook_ingress_image="$(kodex_image_from_repo KODEX_CODEX_HOOK_INGRESS_IMAGE KODEX_CODEX_HOOK_INGRESS_INTERNAL_IMAGE_REPOSITORY kodex/codex-hook-ingress KODEX_CODEX_HOOK_INGRESS_VERSION codex-hook-ingress)"
golang_image="$(kodex_golang_image)"

kodex_build_go_images "build-codex-hook-ingress-images" "$IMAGE_TAR" "$golang_image" \
  "$codex_hook_ingress_image" prod services/internal/codex-hook-ingress/Dockerfile
