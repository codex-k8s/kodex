#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex pull Docker config reconciliation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'Usage: reconcile-pull-docker-config.sh --material-directory <path>' \
    '  --promoted-pull-host <dns> [--include-buildkit-base]' >&2
}

material_directory=""
promoted_pull_host=""
include_buildkit_base=false
while (($# > 0)); do
  case "$1" in
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --promoted-pull-host) promoted_pull_host="${2:-}"; shift 2 ;;
    --include-buildkit-base) include_buildkit_base=true; shift ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -d "$material_directory" && ! -L "$material_directory" ]] ||
  fail 'material directory is invalid'
[[ "$promoted_pull_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ &&
  "$promoted_pull_host" == *.* ]] || fail 'promoted pull host is invalid'
command -v jq >/dev/null 2>&1 || fail 'jq is required'
command -v sha256sum >/dev/null 2>&1 || fail 'sha256sum is required'

internal_pull_host=kodex-image-registry.kodex-system.svc.cluster.local:5000
source_file="$material_directory/registry/pull/dockerconfig.json"
targets=(
  "$source_file"
  "$material_directory/material/kodex/image-registry/pull/dockerconfigjson"
  "$material_directory/projections/kodex-image-registry-pull/pull-dockerconfigjson"
  "$material_directory/projections/kodex-image-registry-pull/probe-dockerconfig.json"
)
pull_count=${#targets[@]}
if [[ "$include_buildkit_base" == true ]]; then
  targets+=(
    "$material_directory/registry/buildkit-base-pull/dockerconfig.json"
    "$material_directory/material/kodex/image-registry/buildkit-base-pull/dockerconfigjson"
    "$material_directory/projections/kodex-buildkit-tls/config.json"
    "$material_directory/projections/role-image-builder-base-pull/config.json"
    "$material_directory/projections/kodex-image-registry-pull/buildkit-dockerconfig.json"
  )
fi
for target in "${targets[@]}"; do
  [[ -f "$target" && -s "$target" && ! -L "$target" ]] ||
    fail 'pull Docker config material is incomplete'
done

sources=("$source_file")
[[ "$include_buildkit_base" != true ]] || sources+=("${targets[$pull_count]}")
for source in "${sources[@]}"; do
  jq -e --arg host "$internal_pull_host" '
    .auths[$host].auth |
    select(type == "string" and length > 0 and length <= 1024)
  ' "$source" >/dev/null || fail 'internal pull identity is absent'
done

temporary_file=$(mktemp "$material_directory/.pull-dockerconfig.XXXXXX")
cleanup() { rm -f -- "$temporary_file"; }
trap cleanup EXIT
umask 077
for group in "${!sources[@]}"; do
  source=${sources[$group]}
  jq --arg internal "$internal_pull_host" --arg promoted "$promoted_pull_host" \
    '.auths[$internal].auth as $auth | {auths:{($internal):{auth:$auth},($promoted):{auth:$auth}}}' \
    "$source" >"$temporary_file"
  chmod 0600 "$temporary_file"
  expected_sha256=$(sha256sum "$temporary_file" | awk '{print $1}')
  if [[ "$group" == 0 ]]; then
    selected=("${targets[@]:0:$pull_count}")
  else
    selected=("${targets[@]:$pull_count}")
  fi
  for target in "${selected[@]}"; do
    install -m 0600 "$temporary_file" "$target"
    [[ "$(sha256sum "$target" | awk '{print $1}')" == "$expected_sha256" ]] ||
      fail 'pull Docker config material readback mismatch'
  done
done

printf 'Kodex pull Docker config material reconciled\n'
