#!/usr/bin/env bash

if [[ -z "${PROJECT_ROOT:-}" ]]; then
  echo "scripts/lib/build_images.sh: PROJECT_ROOT is required before sourcing" >&2
  exit 1
fi

kodex_build_require_env_file() {
  local script_name="$1"
  local env_file="$2"
  if [[ ! -f "$env_file" ]]; then
    echo "${script_name}: env file not found: ${env_file}" >&2
    exit 1
  fi
}

kodex_build_go_images() {
  local script_name="$1"
  local image_tar="$2"
  local golang_image="$3"
  shift 3

  if (( $# == 0 || $# % 3 != 0 )); then
    echo "${script_name}: image build arguments must be passed as image target dockerfile triplets" >&2
    exit 1
  fi

  local built_images=()
  while (( $# > 0 )); do
    local image="$1"
    local target="$2"
    local dockerfile="$3"
    shift 3

    docker build \
      --build-arg "GOLANG_IMAGE=${golang_image}" \
      --target "$target" \
      --tag "$image" \
      --file "${PROJECT_ROOT}/${dockerfile}" \
      "$PROJECT_ROOT"

    built_images+=("$image")
  done

  mkdir -p "$(dirname "$image_tar")"
  docker save --output "$image_tar" "${built_images[@]}"

  echo "${script_name}: saved ${image_tar}"
}
