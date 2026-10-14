#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Stable local image build failed: %s\n' "$*" >&2; exit 1; }
component="" source_sha="" output=""
while (($#)); do
  case "$1" in
    --component) component=${2:-}; shift 2 ;;
    --source-sha) source_sha=${2:-}; shift 2 ;;
    --output-directory) output=${2:-}; shift 2 ;;
    *) fail 'unsupported argument' ;;
  esac
done
[[ "$source_sha" =~ ^[a-f0-9]{40}$ && "$output" == /* && "$output" != / ]] || fail 'exact source and output directory are required'
case "$component" in
  control-plane|secret-broker|stt-tts-service|email-bridge|runtime-controller|integration-gateway|integration-synthetic|interaction-gateway|control-api-gateway|egress-gateway|automation-scheduler|artifact-retention|session-archive|staff-control-center) ;;
  *) fail 'component is not in the stable platform registry' ;;
esac
for program in docker git jq tar sha256sum stat realpath df; do command -v "$program" >/dev/null || fail 'required program is absent'; done
root=$(git rev-parse --show-toplevel)
[[ "$(git rev-parse HEAD)" == "$source_sha" && -z "$(git status --porcelain --untracked-files=no)" ]] || fail 'clean exact source is required'
[[ ! -e "$output" && ! -L "$output" ]] || fail 'output directory must be new'
builder=kodex-local-dev
# Не пересоздавать существующий builder или Docker network при сборке релиза.
docker buildx inspect "$builder" >/dev/null 2>&1 || fail 'existing local builder is required'
dockerfile=$(jq -er --arg c "$component" '[.images[] | select(.component == $c)] | select(length == 1) | .[0].dockerfile' "$root/tools/release/images.json")
target=$(jq -er --arg c "$component" '.images[] | select(.component == $c) | .target // ""' "$root/tools/release/images.json")
target_options=()
[[ -z "$target" ]] || target_options=(--target "$target")
"$root/tools/release/validate-image-dockerfile-path.sh" "$dockerfile" || fail 'Dockerfile path is invalid'
umask 077
mkdir -m 0700 "$output"
output=$(realpath -e "$output")
[[ "$(stat -c '%u:%a' "$output")" == "$(id -u):700" ]] || fail 'private output directory is required'
available_bytes=$(df -B1 --output=avail "$output" | tail -n 1 | tr -d ' ')
[[ "$available_bytes" =~ ^[0-9]+$ && "$available_bytes" -ge 5368709120 ]] || fail 'at least 5 GiB of build headroom is required'
source_dir=$(mktemp -d "$output/source.XXXXXXXX")
cleanup() { rm -rf -- "$source_dir"; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Ни working tree .env, ни untracked files, ни локальный .git в контекст не входят.
git archive "$source_sha" | tar -x -C "$source_dir"
context_sha=$(git archive "$source_sha" | sha256sum | cut -d ' ' -f 1)
archive="$output/$component.oci.tar"
docker buildx build --builder "$builder" --platform linux/amd64 \
  --file "$source_dir/$dockerfile" "${target_options[@]}" \
  --provenance=false --sbom=false \
  --build-arg "SOURCE_SHA=$source_sha" --build-arg "VERSION=$source_sha" \
  --tag "registry.local.kodex/kodex/$component:$source_sha" \
  --output "type=oci,dest=$archive" "$source_dir" >"$output/build.log" 2>&1 || fail 'image compilation failed; inspect private build.log'
digest=$(tar -xOf "$archive" index.json | jq -er 'select((.manifests | length) == 1) | .manifests[0].digest')
[[ "$digest" =~ ^sha256:[a-f0-9]{64}$ && "$digest" != "sha256:$(printf '%064d' 0)" ]] || fail 'single immutable OCI manifest is required'
archive_sha=$(sha256sum "$archive" | cut -d ' ' -f 1)
jq -n --arg component "$component" --arg source "$source_sha" --arg context "$context_sha" \
  --arg digest "$digest" --arg archive "$archive_sha" --arg dockerfile "$dockerfile" --arg target "$target" \
  '{schema:"kodex.stable-local-build/v1",component:$component,sourceSHA:$source,contextSHA256:$context,
    imageDigest:$digest,archiveSHA256:$archive,dockerfile:$dockerfile,target:$target,
    admission:"NOT_RUN",publication:"NOT_RUN",nodePull:"NOT_RUN"}' >"$output/build.json"
chmod 0600 "$archive" "$output/build.json" "$output/build.log"
printf '{"status":"PASS","phase":"LOCAL_OCI_BUILD_ONLY","component":"%s","digest":"%s"}\n' "$component" "$digest"
