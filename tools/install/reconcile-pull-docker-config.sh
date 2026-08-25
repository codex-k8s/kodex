#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex pull Docker config reconciliation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'Usage: reconcile-pull-docker-config.sh --material-directory <path>' \
    '  --promoted-pull-host <dns>' >&2
}

material_directory=""
promoted_pull_host=""
while (($# > 0)); do
  case "$1" in
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --promoted-pull-host) promoted_pull_host="${2:-}"; shift 2 ;;
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
for target in "${targets[@]}"; do
  [[ -f "$target" && -s "$target" && ! -L "$target" ]] ||
    fail 'pull Docker config material is incomplete'
done

auth=$(jq -er --arg host "$internal_pull_host" '
  .auths[$host].auth |
  select(type == "string" and length > 0 and length <= 1024)
' "$source_file") || fail 'internal pull identity is absent'

temporary_file=$(mktemp "$material_directory/.pull-dockerconfig.XXXXXX")
cleanup() { rm -f -- "$temporary_file"; }
trap cleanup EXIT
umask 077
jq -n --arg internal "$internal_pull_host" --arg promoted "$promoted_pull_host" \
  --arg auth "$auth" \
  '{auths:{($internal):{auth:$auth},($promoted):{auth:$auth}}}' >"$temporary_file"
chmod 0600 "$temporary_file"
for target in "${targets[@]}"; do
  install -m 0600 "$temporary_file" "$target"
done

expected_sha256=$(sha256sum "$temporary_file" | awk '{print $1}')
for target in "${targets[@]}"; do
  [[ "$(sha256sum "$target" | awk '{print $1}')" == "$expected_sha256" ]] ||
    fail 'pull Docker config material readback mismatch'
done

printf 'Kodex pull Docker config material reconciled\n'
