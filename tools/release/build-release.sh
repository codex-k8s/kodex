#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Release build failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --source-sha <40-hex> --output <release-lock.json>" \
    '  --registry-push <host[:port][/prefix]> --node-pull <host[:port][/prefix]>' \
    '  [--repository-prefix <path>] [--build-proxy <url> --build-no-proxy <list>]' \
    '  [--profile <web-only|web-with-mattermost>] [--build-run-id <digits>]' >&2
}

source_sha=""
output=""
registry_push=""
node_pull=""
repository_prefix="kodex"
build_proxy=""
build_no_proxy=""
build_run_id="${GITHUB_RUN_ID:-local}"
profile="web-only"

while (($# > 0)); do
  case "$1" in
    --source-sha) source_sha="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    --registry-push) registry_push="${2:-}"; shift 2 ;;
    --node-pull) node_pull="${2:-}"; shift 2 ;;
    --repository-prefix) repository_prefix="${2:-}"; shift 2 ;;
    --build-proxy) build_proxy="${2:-}"; shift 2 ;;
    --build-no-proxy) build_no_proxy="${2:-}"; shift 2 ;;
    --build-run-id) build_run_id="${2:-}"; shift 2 ;;
    --profile) profile="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

valid_registry_path() {
  [[ "$1" =~ ^[a-z0-9][a-z0-9.:-]*(/[a-z0-9][a-z0-9._/-]*)?$ ]] &&
    [[ "$1" != *://* && "$1" != *@* && "$1" != */ && "$1" != *//* ]]
}

[[ "$source_sha" =~ ^[a-f0-9]{40}$ ]] || fail 'source SHA must be exact lowercase 40-hex'
[[ "$profile" == "web-only" || "$profile" == "web-with-mattermost" ]] || fail 'release profile is invalid'
[[ -n "$output" ]] || fail 'output path is required'
valid_registry_path "$registry_push" || fail 'registry push path is invalid'
valid_registry_path "$node_pull" || fail 'node pull path is invalid'
registry_push_host=${registry_push%%/*}
[[ "$registry_push_host" == *.* && ("$registry_push_host" != *:* || "$registry_push_host" == *:443) ]] ||
  fail 'registry push endpoint must use external HTTPS port 443'
[[ "$repository_prefix" =~ ^[a-z0-9][a-z0-9._/-]*$ && "$repository_prefix" != */ && "$repository_prefix" != *//* ]] ||
  fail 'repository prefix is invalid'
[[ "$build_run_id" == local || "$build_run_id" =~ ^[0-9]+$ ]] || fail 'build run ID is invalid'
if [[ -n "$build_proxy" || -n "$build_no_proxy" ]]; then
  [[ "$build_proxy" =~ ^https?://[^[:space:]]+$ && -n "$build_no_proxy" ]] ||
    fail 'build proxy and no-proxy must be configured together'
  [[ "${HTTPS_PROXY:-}" == "$build_proxy" && "${HTTP_PROXY:-}" == "$build_proxy" &&
     "${NO_PROXY:-}" == "$build_no_proxy" ]] ||
    fail 'build proxy environment does not match the explicit release configuration'
fi

for command_name in git go jq sha256sum tar; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

repository_root=$(git rev-parse --show-toplevel)
[[ "$(git -C "$repository_root" rev-parse HEAD)" == "$source_sha" ]] || fail 'HEAD does not match source SHA'
[[ -z "$(git -C "$repository_root" status --porcelain --untracked-files=no)" ]] ||
  fail 'tracked worktree changes are forbidden'
"$repository_root/scripts/tests/go-toolchain-contract-test.sh" >/dev/null ||
  fail 'Go toolchain and Docker build context contract failed'
manifest="$repository_root/tools/release/images.json"
jq -e '.schema_version == 2 and (.images | length > 0)' "$manifest" >/dev/null || fail 'image manifest is invalid'

buildctl_path=${BUILDCTL_PATH:-/var/run/kodex-tools/buildctl}
buildkit_host=${BUILDKIT_HOST:-unix:///var/run/buildkit/buildkitd.sock}
[[ -x "$buildctl_path" ]] || fail 'buildctl is not executable'
[[ "$buildkit_host" == unix:///* ]] || fail 'BuildKit must use a local Unix socket'

proxy_frontend_options=()
if [[ -n "$build_proxy" ]]; then
  proxy_frontend_options=(
    --opt "build-arg:HTTPS_PROXY=$build_proxy"
    --opt "build-arg:HTTP_PROXY=$build_proxy"
    --opt "build-arg:https_proxy=$build_proxy"
    --opt "build-arg:http_proxy=$build_proxy"
    --opt "build-arg:NO_PROXY=$build_no_proxy"
    --opt "build-arg:no_proxy=$build_no_proxy"
  )
fi

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
metadata_directory="$temporary_directory/metadata"
source_context="$temporary_directory/source"
mkdir -p "$metadata_directory" "$source_context"
git -C "$repository_root" archive "$source_sha" | tar -x -C "$source_context"

admission_tools_component=image-admission-tools
admission_tools_destination="$registry_push/$repository_prefix/$admission_tools_component:$source_sha"
admission_tools_dockerfile=infra/admission-tools/Dockerfile
[[ -f "$source_context/$admission_tools_dockerfile" ]] || fail 'admission tools Dockerfile is missing'
"$buildctl_path" --addr "$buildkit_host" build \
  --frontend dockerfile.v0 \
  --local context="$source_context" \
  --local dockerfile="$source_context/$(dirname -- "$admission_tools_dockerfile")" \
  --opt filename="$(basename -- "$admission_tools_dockerfile")" \
  --opt platform=linux/amd64 \
  --opt "build-arg:SOURCE_SHA=$source_sha" \
  "${proxy_frontend_options[@]}" \
  --output "type=image,name=$admission_tools_destination,push=true" \
  --metadata-file "$metadata_directory/$admission_tools_component.json"
admission_tools_digest=$(jq -r '."containerimage.digest" // empty' \
  "$metadata_directory/$admission_tools_component.json")
[[ "$admission_tools_digest" =~ ^sha256:[a-f0-9]{64}$ && \
  "$admission_tools_digest" != sha256:0000000000000000000000000000000000000000000000000000000000000000 ]] ||
  fail 'BuildKit returned an invalid admission tools digest'
admission_tools_build_ref="$registry_push/$repository_prefix/$admission_tools_component@$admission_tools_digest"
admission_tools_pull_ref="$node_pull/$repository_prefix/$admission_tools_component@$admission_tools_digest"

while IFS=$'\t' read -r component dockerfile target; do
  [[ "$component" =~ ^[a-z0-9-]+$ ]] || fail 'invalid component name'
  "$repository_root/tools/release/validate-image-dockerfile-path.sh" "$dockerfile" ||
    fail "invalid Dockerfile path for $component"
  [[ -f "$repository_root/$dockerfile" ]] || fail "Dockerfile is missing for $component"
  case "$component:$target" in
    role-image-builder:runtime|image-admission:admission-runtime|*:) ;;
    *) fail "unexpected Dockerfile target for $component" ;;
  esac

  destination="$registry_push/$repository_prefix/$component:$source_sha"
  if [[ "$component" == agent-runner ]]; then
    env -u BASH_ENV -u ENV \
      PATH="$repository_root/tools/release/shims:$PATH" \
      BUILDCTL_PATH="$buildctl_path" BUILDKIT_HOST="$buildkit_host" \
      KODEX_BUILDKIT_METADATA_FILE="$metadata_directory/$component.json" \
      KODEX_AGENT_RUNNER_DESTINATION="$destination" \
      KODEX_RELEASE_BUILD_PROXY="$build_proxy" \
      KODEX_RELEASE_BUILD_NO_PROXY="$build_no_proxy" \
      "$repository_root/scripts/build-agent-runner-image.sh" \
        --builder docker --context "$source_context" --dockerfile "$source_context/$dockerfile" \
        --tag "$destination" --network host
    continue
  fi

  target_options=()
  [[ -z "$target" ]] || target_options=(--opt "target=$target")
  component_options=()
  if [[ "$component" == image-admission ]]; then
    component_options=(--opt "build-arg:ADMISSION_TOOLS_IMAGE=$admission_tools_build_ref")
  elif [[ "$component" == role-base-documents ]]; then
    agent_runner_digest=$(jq -r '."containerimage.digest" // empty' "$metadata_directory/agent-runner.json")
    [[ "$agent_runner_digest" =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'agent-runner digest is unavailable for role base'
    component_options=(--opt "build-arg:AGENT_RUNNER_IMAGE=$registry_push/$repository_prefix/agent-runner@$agent_runner_digest")
  fi
  "$buildctl_path" --addr "$buildkit_host" build \
    --frontend dockerfile.v0 \
    --local context="$source_context" \
    --local dockerfile="$source_context/$(dirname -- "$dockerfile")" \
    --opt filename="$(basename -- "$dockerfile")" \
    --opt "build-arg:SOURCE_SHA=$source_sha" \
    --opt "build-arg:VERSION=$source_sha" \
    "${target_options[@]}" \
    "${component_options[@]}" \
    "${proxy_frontend_options[@]}" \
    --output "type=image,name=$destination,push=true" \
    --metadata-file "$metadata_directory/$component.json"
done < <(jq -r '.images[] | [.component, .dockerfile, (.target // "")] | @tsv' "$manifest")

role_image_input_result="$temporary_directory/role-image-input.json"
(
  cd -- "$repository_root/tools/release/role-image-input-publisher"
  GOWORK=off go run . \
    --repository "$registry_push/$repository_prefix/role-image-inputs" \
    --source-sha "$source_sha" >"$role_image_input_result"
)
role_image_input_manifest_digest=$(jq -er '.manifestDigest' "$role_image_input_result")
role_image_input_payload_sha256=$(jq -er '.payloadSha256' "$role_image_input_result")
role_image_input_source_sha256=$(jq -er '.sourceSha256' "$role_image_input_result")
[[ "$role_image_input_manifest_digest" =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'role image input manifest digest is invalid'
[[ "$role_image_input_payload_sha256" =~ ^[a-f0-9]{64}$ ]] || fail 'role image input payload digest is invalid'
[[ "$role_image_input_source_sha256" =~ ^[a-f0-9]{64}$ ]] || fail 'role image input source digest is invalid'

images_json="$temporary_directory/images.json"
printf '[]' >"$images_json"
while IFS= read -r component; do
  digest=$(jq -r '."containerimage.digest" // empty' "$metadata_directory/$component.json")
  [[ "$digest" =~ ^sha256:[a-f0-9]{64}$ && "$digest" != sha256:0000000000000000000000000000000000000000000000000000000000000000 ]] ||
    fail "BuildKit returned an invalid digest for $component"
  jq --arg component "$component" \
    --arg repository "$repository_prefix/$component" \
    --arg digest "$digest" \
    --arg pull_ref "$node_pull/$repository_prefix/$component@$digest" \
    '. + [{component:$component,repository:$repository,digest:$digest,pull_ref:$pull_ref}]' \
    "$images_json" >"$images_json.next"
  mv "$images_json.next" "$images_json"
done < <(jq -r '.images[].component' "$manifest")

jq -n \
  --arg profile "$profile" \
  --arg source_sha "$source_sha" \
  --arg build_run_id "$build_run_id" \
  --arg registry_push "$registry_push" \
  --arg node_pull "$node_pull" \
  --arg repository_prefix "$repository_prefix" \
  --arg admission_tools_ref "$admission_tools_pull_ref" \
  --arg admission_tools_digest "$admission_tools_digest" \
  --arg role_input_repository "$repository_prefix/role-image-inputs" \
  --arg role_input_manifest_digest "$role_image_input_manifest_digest" \
  --arg role_input_payload_sha256 "$role_image_input_payload_sha256" \
  --arg role_input_source_sha256 "$role_image_input_source_sha256" \
  --arg role_input_pull_ref "$node_pull/$repository_prefix/role-image-inputs@$role_image_input_manifest_digest" \
  --slurpfile images "$images_json" \
  '{schema_version:2,profile:$profile,source_sha:$source_sha,build_run_id:$build_run_id,
    registry:{push:$registry_push,node_pull:$node_pull,repository_prefix:$repository_prefix},
    external_images:[{component:"admission-tools",pull_ref:$admission_tools_ref,digest:$admission_tools_digest}],
    role_image_input:{repository:$role_input_repository,manifest_digest:$role_input_manifest_digest,
      payload_sha256:$role_input_payload_sha256,source_sha256:$role_input_source_sha256,pull_ref:$role_input_pull_ref},
    images:$images[0]}' | jq -S . >"$output"
printf 'Release lock created: %s\n' "$output"
