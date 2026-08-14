#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Dark render failed: %s\n' "$*" >&2; exit 1; }
lock_file=""
source_sha=""
lock_sha256=""
output=""
while (($# > 0)); do
  case "$1" in
    --lock) lock_file="${2:-}"; shift 2 ;;
    --source-sha) source_sha="${2:-}"; shift 2 ;;
    --sha256) lock_sha256="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    *) fail "unsupported argument: $1" ;;
  esac
done
[[ -n "$output" ]] || fail "output path is required"
script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
"$script_directory/validate-release-lock.sh" --lock "$lock_file" --source-sha "$source_sha" --sha256 "$lock_sha256" >/dev/null
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
applications="$temporary_directory/applications.yaml"
kubectl kustomize "$repository_root/deploy/k8s/base/direct-production-foundation" |
  yq eval-all '
    select(.kind == "ConfigMap" or .kind == "Service" or .kind == "Deployment" or .kind == "StatefulSet" or .kind == "Job") |
    .metadata.labels."mattercodex.dev/release-managed" = "true" |
    with(select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "Job");
      .spec.template.metadata.labels."mattercodex.dev/release-managed" = "true" |
      .spec.template.spec.automountServiceAccountToken = false
    )
  ' >"$output"
mattermost_bridge_config_sha256=$(yq -r '
  select(.kind == "ConfigMap" and .metadata.name == "mattercodex-legacy-transport-bridges") |
  .data."mattermost.yaml"
' "$output" | sha256sum | awk '{print $1}')
[[ "$mattermost_bridge_config_sha256" =~ ^[a-f0-9]{64}$ ]] ||
  fail "legacy Mattermost bridge config digest is invalid"
MATTERMOST_BRIDGE_CONFIG_SHA256="$mattermost_bridge_config_sha256" yq -i '
  with(select(.kind == "Deployment" and .metadata.name == "mattercodex-legacy-mattermost-bridge");
    .spec.template.metadata.annotations."mattercodex.dev/config-sha256" = strenv(MATTERMOST_BRIDGE_CONFIG_SHA256)
  )
' "$output"
"$script_directory/render-direct-production-applications.sh" \
  --lock "$lock_file" --scope release --output "$applications" >/dev/null
printf '%s\n' '---' >>"$output"
cat "$applications" >>"$output"
{
  printf '%s\n' '---' 'apiVersion: v1' 'kind: ConfigMap' 'metadata:' '  name: mattercodex-release-lock' '  namespace: mattercodex-system' '  labels:' '    mattercodex.dev/profile: direct-production-single-node-prototype' '    mattercodex.dev/release-managed: "true"' 'data:'
  printf '  source_sha: "%s"\n  release_lock_sha256: "%s"\n' "$source_sha" "$lock_sha256"
  while IFS=$'\t' read -r component pull_ref; do
    printf '  image.%s: "%s"\n' "$component" "$pull_ref"
  done < <(jq -r '.images[] | [.component,.pull_ref] | @tsv' "$lock_file")
} >>"$output"
grep -Eq '^kind: Ingress$' "$output" && fail "dark render must not contain Ingress"
grep -Eq 'namespace: matter-kodex-prod$' "$output" && fail "dark render must not target the legacy namespace"
printf 'Dark render created: %s\n' "$output"
