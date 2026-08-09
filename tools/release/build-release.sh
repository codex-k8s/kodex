#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Release build failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --source-sha <40-hex> --output <release-lock.json> [--build-run-id <digits>]\n' "$0" >&2
}

source_sha=""
output=""
build_run_id="${GITHUB_RUN_ID:-local}"
while (($# > 0)); do
  case "$1" in
    --source-sha) source_sha="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    --build-run-id) build_run_id="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$source_sha" =~ ^[a-f0-9]{40}$ ]] || fail "source SHA must be exact lowercase 40-hex"
[[ -n "$output" ]] || fail "output path is required"
[[ "$build_run_id" == local || "$build_run_id" =~ ^[0-9]+$ ]] || fail "build run ID is invalid"
for command_name in git jq sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

repository_root=$(git rev-parse --show-toplevel)
[[ "$(git -C "$repository_root" rev-parse HEAD)" == "$source_sha" ]] || fail "HEAD does not match source SHA"
[[ -z "$(git -C "$repository_root" status --porcelain --untracked-files=no)" ]] || fail "tracked worktree changes are forbidden"
manifest="$repository_root/tools/release/images.json"
jq -e '.schema_version == 1 and (.images | length > 0)' "$manifest" >/dev/null || fail "image manifest is invalid"

buildctl_path=${BUILDCTL_PATH:-/var/run/mattercodex-tools/buildctl}
buildkit_host=${BUILDKIT_HOST:-unix:///var/run/buildkit/buildkitd.sock}
[[ -x "$buildctl_path" ]] || fail "buildctl is not executable"
[[ "$buildkit_host" == unix:///* ]] || fail "BuildKit must use a local Unix socket"

registry_push=matter-codex-registry.matter-kodex-prod.svc.cluster.local:5000
node_pull=localhost:5001
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
metadata_directory="$temporary_directory/metadata"
mkdir -p "$metadata_directory"

while IFS=$'\t' read -r component dockerfile; do
  [[ "$component" =~ ^[a-z0-9-]+$ ]] || fail "invalid component name"
  [[ "$dockerfile" == services/*/Dockerfile || "$dockerfile" == services/*/*/Dockerfile ]] || fail "invalid Dockerfile path"
  [[ -f "$repository_root/$dockerfile" ]] || fail "Dockerfile is missing for $component"
  destination="$registry_push/mattercodex/$component:$source_sha"
  if [[ "$component" == agent-runner ]]; then
    env -u BASH_ENV -u ENV \
      PATH="$repository_root/tools/release/shims:$PATH" \
      BUILDCTL_PATH="$buildctl_path" BUILDKIT_HOST="$buildkit_host" \
      MATTERCODEX_BUILDKIT_METADATA_FILE="$metadata_directory/$component.json" \
      "$repository_root/scripts/build-agent-runner-image.sh" \
        --builder docker --context "$repository_root" --dockerfile "$dockerfile" \
        --tag "$destination" --network host
  else
    "$buildctl_path" --addr "$buildkit_host" build \
      --frontend dockerfile.v0 \
      --local context="$repository_root" \
      --local dockerfile="$repository_root/$(dirname -- "$dockerfile")" \
      --opt filename="$(basename -- "$dockerfile")" \
      --output "type=image,name=$destination,push=true" \
      --metadata-file "$metadata_directory/$component.json"
  fi
done < <(jq -r '.images[] | [.component, .dockerfile] | @tsv' "$manifest")

images_json="$temporary_directory/images.json"
printf '[]' >"$images_json"
while IFS= read -r component; do
  digest=$(jq -r '."containerimage.digest" // empty' "$metadata_directory/$component.json")
  [[ "$digest" =~ ^sha256:[a-f0-9]{64}$ && "$digest" != sha256:0000000000000000000000000000000000000000000000000000000000000000 ]] ||
    fail "BuildKit returned an invalid digest for $component"
  jq --arg component "$component" --arg repository "mattercodex/$component" \
    --arg digest "$digest" --arg pull_ref "$node_pull/mattercodex/$component@$digest" \
    '. + [{component:$component,repository:$repository,digest:$digest,pull_ref:$pull_ref}]' \
    "$images_json" >"$images_json.next"
  mv "$images_json.next" "$images_json"
done < <(jq -r '.images[].component' "$manifest")

jq -n --arg profile "direct-production single-node prototype" \
  --arg source_sha "$source_sha" --arg build_run_id "$build_run_id" \
  --arg registry_push "$registry_push" --arg node_pull "$node_pull" \
  --slurpfile images "$images_json" \
  '{schema_version:1,profile:$profile,source_sha:$source_sha,build_run_id:$build_run_id,registry_push:$registry_push,node_pull:$node_pull,images:$images[0]}' \
  | jq -S . >"$output"
printf 'Release lock created: %s\n' "$output"
