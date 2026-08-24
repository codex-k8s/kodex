#!/bin/sh
set -eu

fail() {
  printf 'Release artifact materializer failed: %s\n' "$*" >&2
  exit 1
}

for command_name in base64 jq regctl; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is unavailable"
done

case "${RELEASE_SOURCE_REGISTRY:-}" in
  *://*|*/*|*@*|*' '*) fail 'release source registry is invalid' ;;
esac
release_source_hostname=${RELEASE_SOURCE_REGISTRY%:443}
case "$release_source_hostname" in
  *.*) ;;
  *) fail 'release source registry is invalid' ;;
esac
case "$RELEASE_SOURCE_REGISTRY" in
  "$release_source_hostname"|"$release_source_hostname:443") ;;
  *) fail 'release source registry must use HTTPS port 443' ;;
esac

destination_registry=mattercodex-image-registry-promotion.mattercodex-system.svc.cluster.local:5003
work=/work/registry
mkdir -p "$work/docker"
umask 077
export REGCTL_CONFIG="$work/regctl.json"
export DOCKER_CONFIG="$work/docker"

credential() {
  config_file=$1
  registry=$2
  encoded=$(jq -er --arg registry "$registry" '
    . as $root |
    ($root.credsStore // "") == "" and (($root.credHelpers // {}) | length) == 0 and
    ($root.auths | length) == 1 and ($root.auths[$registry].auth | type == "string") |
    if . then $root.auths[$registry].auth else error("invalid registry credential") end
  ' "$config_file") || fail 'registry credential is invalid'
  decoded=$(printf '%s' "$encoded" | base64 -d 2>/dev/null) || fail 'registry credential is invalid'
  case "$decoded" in
    *:*) ;;
    *) fail 'registry credential is invalid' ;;
  esac
  username=${decoded%%:*}
  password=${decoded#*:}
  test -n "$username" && test -n "$password" || fail 'registry credential is empty'
}

credential /identity/source/config.json "$RELEASE_SOURCE_REGISTRY"
credential /identity/destination/config.json "$destination_registry"
jq -es '
  {auths: (reduce .[] as $config ({}; . + $config.auths))}
' /identity/source/config.json /identity/destination/config.json >"$DOCKER_CONFIG/config.json" ||
  fail 'merge registry credentials'
jq -e --arg source "$RELEASE_SOURCE_REGISTRY" --arg destination "$destination_registry" '
  (.auths | keys | sort) == ([$source, $destination] | sort)
' "$DOCKER_CONFIG/config.json" >/dev/null || fail 'merged registry credential scope is invalid'

regctl registry set "$RELEASE_SOURCE_REGISTRY" --tls enabled >/dev/null
regctl registry set "$destination_registry" --tls enabled \
  --cacert /identity/destination/ca.pem \
  --cert /identity/destination/tls.crt \
  --key /identity/destination/tls.key >/dev/null

copy_exact() {
  source_ref=$1
  destination_repository=$2
  expected_digest=$3
  case "$source_ref" in
    "$RELEASE_SOURCE_REGISTRY"/*@"$expected_digest") ;;
    *) fail 'release source reference is outside the exact lock' ;;
  esac
  case "$expected_digest" in
    sha256:????????????????????????????????????????????????????????????????) ;;
    *) fail 'release source digest is invalid' ;;
  esac
  destination_tag="$destination_registry/$destination_repository:release-${RELEASE_SOURCE_SHA}"
  regctl image copy "$source_ref" "$destination_tag" >/dev/null || fail 'copy exact release artifact'
  actual_digest=$(regctl image digest "$destination_tag") || fail 'read back release artifact'
  test "$actual_digest" = "$expected_digest" || fail 'release artifact digest mismatch'
}

copy_exact "$AGENT_RUNNER_SOURCE_REF" mattercodex/agent-runner "$AGENT_RUNNER_DIGEST"
copy_exact "$ROLE_BASE_DOCUMENTS_SOURCE_REF" mattercodex/role-base-documents "$ROLE_BASE_DOCUMENTS_DIGEST"
copy_exact "$ROLE_IMAGE_INPUT_SOURCE_REF" mattercodex/role-image-inputs "$ROLE_IMAGE_INPUT_MANIFEST_DIGEST"

rm -f "$REGCTL_CONFIG" "$DOCKER_CONFIG/config.json"
printf 'Release artifact materialization completed\n'
