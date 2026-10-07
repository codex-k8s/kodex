#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local runner build failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --source-root <path> --state-directory <path> --context <context> [--image-profile local|full]\n' "$0" >&2
}

source_root=""
state_directory=""
context=""
image_profile=local
image_profile_supplied=false
while (($# > 0)); do
  case "$1" in
    --source-root) source_root=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --context) context=${2:-}; shift 2 ;;
    --image-profile)
      [[ "$image_profile_supplied" == false ]] || fail 'image profile argument is duplicated'
      image_profile=${2:-}
      image_profile_supplied=true
      (($# >= 2)) || fail 'image profile is required'
      shift 2
      ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

case "$image_profile" in
  local) image_target=local-runtime ;;
  full) image_target=full-runtime ;;
  *) fail 'image profile is invalid' ;;
esac

[[ "$source_root" == /* && -f "$source_root/services/jobs/agent-runner/Dockerfile" ]] ||
  fail 'source root is invalid'
[[ "$state_directory" == /* && "$state_directory" != / ]] || fail 'state directory is invalid'
[[ -n "$context" ]] || fail 'exact context is required'
for command_name in docker jq sha256sum tar python3 git; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
docker buildx version >/dev/null 2>&1 || fail 'docker buildx is required'

builder=kodex-local-dev
verifier="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../release" && pwd)/runner-binary-provenance.py"
revision=$(git -C "$source_root" rev-parse HEAD)

install -d -m 0700 "$state_directory/cache"
input_digest=$(python3 -B "$verifier" input --source-root "$source_root" --revision "$revision" --image-profile "$image_profile")
[[ "$input_digest" =~ ^[a-f0-9]{64}$ ]] || fail 'runner input digest is invalid'

repository=registry.local.kodex/kodex/agent-runner
tag="$repository:local-$input_digest"
archive="$state_directory/cache/agent-runner-$input_digest.oci.tar"
if [[ ! -s "$archive" ]]; then
  "$source_root/tools/dev/ensure-local-buildx-builder.sh" "$builder"
  next_archive="$archive.next"
  rm -f "$next_archive"
  docker buildx build --builder "$builder" \
    --file "$source_root/services/jobs/agent-runner/Dockerfile" \
    --target "$image_target" \
    --label "kodex.dev/runner-image-profile=$image_profile" \
    --platform linux/amd64 \
    --provenance=false \
    --sbom=false \
    --tag "$tag" \
    --output "type=oci,dest=$next_archive" \
    "$source_root"
  [[ -s "$next_archive" ]] || fail 'runner OCI archive was not produced'
  mv "$next_archive" "$archive"
fi

manifest_digest=$(tar -xOf "$archive" index.json | jq -er '
  if (.manifests | length) != 1 then error("one image manifest is required")
  else .manifests[0].digest end
') || fail 'runner OCI manifest digest is unavailable'
[[ "$manifest_digest" =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'runner OCI manifest digest is invalid'
exact_reference="$repository@$manifest_digest"

# Проверка одинакова для нового OCI и cache hit; прежний output неизменяем.
provenance="$state_directory/cache/agent-runner-$input_digest-$revision.provenance.json"
provenance_phase=verify
if [[ -e "$provenance" || -L "$provenance" ]]; then provenance_phase=check; fi
python3 -B "$verifier" "$provenance_phase" --source-root "$source_root" \
  --revision "$revision" --archive "$archive" --expected-manifest "$manifest_digest" \
  --expected-input-digest "$input_digest" --image-profile "$image_profile" --repository "$repository" --output "$provenance"

"$source_root/tools/dev/import-local-image.sh" --context "$context" --archive "$archive" \
  --repository "$repository" --tag "$tag" --exact-reference "$exact_reference" >/dev/null

printf '%s\n' "$exact_reference" >"$state_directory/agent-runner-image"
chmod 0600 "$state_directory/agent-runner-image"
printf 'Kodex local runner image ready: %s\n' "$exact_reference"
