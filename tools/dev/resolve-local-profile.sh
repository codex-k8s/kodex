#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local profile selection failed: %s\n' "$*" >&2
  exit 1
}

requested=${1:-}
render=${2:?render path is required}
case "$requested" in
  ''|web-only|web-with-mattermost) ;;
  *) fail 'deployment profile is invalid' ;;
esac
if [[ -e "$render" || -L "$render" ]]; then
  [[ -f "$render" && -s "$render" && ! -L "$render" ]] || fail 'existing render is unsafe'
  stored=$(yq -o=json -I=0 '
    select(.kind == "ConfigMap" and .metadata.namespace == "kodex-system" and
      .metadata.name == "kodex-dev-source-provenance")
  ' "$render" | jq -s -er '
    select(length == 1) | .[0].data |
    (if has("deploymentProfile") then .deploymentProfile else "web-only" end) |
    select(. == "web-only" or . == "web-with-mattermost")
  ') || fail 'existing deployment profile is invalid'
  [[ -z "$requested" || "$requested" == "$stored" ]] ||
    fail 'changing deployment profile requires an explicit disposable reset'
  printf '%s\n' "$stored"
else
  printf '%s\n' "${requested:-web-only}"
fi
