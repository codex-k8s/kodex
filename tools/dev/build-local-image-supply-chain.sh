#!/usr/bin/env bash
set -euo pipefail
set +m

fail() {
  printf 'Kodex local image supply-chain build failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --source-root <path> --state-directory <path> [--component all|image-admission|authority-security] [--context <exact-staging-context>] [--build-jobs 1..4]\n' "$0" >&2
}

source_root=""
state_directory=""
component=all
context=""
build_jobs=1
while (($# > 0)); do
  case "$1" in
    --source-root) source_root=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --component) component=${2:-}; shift 2 ;;
    --context) context=${2:-}; shift 2 ;;
    --build-jobs) build_jobs=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$component" == all || "$component" == image-admission || "$component" == authority-security ]] || fail 'component is invalid'
[[ "$build_jobs" =~ ^[1-4]$ ]] || fail 'build jobs must be between 1 and 4'

[[ "$source_root" == /* && -f "$source_root/tools/dev/Dockerfile.local-image-supply-chain" &&
  -f "$source_root/tools/dev/Dockerfile.local-image-supply-chain.dockerignore" &&
  -f "$source_root/services/jobs/role-image-builder/Dockerfile" &&
  -f "$source_root/services/internal/internal-rpc-authority/Dockerfile" ]] ||
  fail 'source root is invalid'
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" ]] ||
  fail 'state directory is invalid'
[[ -n "$context" && "$(kubectl config current-context)" == "$context" ]] ||
  fail 'exact staging context is required'
for command_name in docker git jq kubectl sha256sum tar flock setsid; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
docker buildx version >/dev/null 2>&1 || fail 'docker buildx is required'

if [[ "$component" == authority-security ]]; then
  [[ "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
    fail 'exact staging context is required'
  kubectl --context "$context" get namespace kodex-system -o json | jq -e '
    .metadata.labels."app.kubernetes.io/part-of" == "kodex" and
    .metadata.labels."kodex.dev/environment" == "staging"' >/dev/null || fail 'staging namespace is required'
  [[ "$state_directory" != "$source_root" && "$state_directory" != "$source_root/"* && ! -L "$state_directory" ]] ||
    fail 'private state must be outside source'
  node --input-type=module - "$source_root" <<'JS'
import {pathToFileURL} from 'node:url';
const root=process.argv[2];
const {inspectSource}=await import(pathToFileURL(root+'/tools/release/application-source.mjs'));
try {inspectSource(root);} catch {process.stderr.write('Exact clean application source required\n');process.exit(1);}
JS
fi

install -d -m 0700 "$state_directory/cache/image-supply-chain"
# Один writer защищает одинаковые cache keys и указатели от повторного запуска.
exec {build_lock_fd}>"$state_directory/cache/image-supply-chain/build.lock"
flock -n "$build_lock_fd" || fail 'another supply-chain build owns this state directory'
builder=kodex-local-dev
"$source_root/tools/dev/ensure-local-buildx-builder.sh" "$builder"

build_work_directory=$(mktemp -d "$state_directory/cache/image-supply-chain/build.XXXXXXXX")
declare -a build_pids=() build_names=() build_archives=() build_next_archives=() build_status_files=() build_logs=()
declare -a image_names=() image_repositories=()
cleanup_builds() {
  local status=$? pid deadline running
  trap - EXIT INT TERM
  for pid in "${build_pids[@]}"; do
    kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
  done
  deadline=$((SECONDS + 3))
  while ((${#build_pids[@]} > 0 && SECONDS < deadline)); do
    running=false
    for pid in "${build_pids[@]}"; do
      if kill -0 -- "-$pid" 2>/dev/null; then running=true; fi
    done
    [[ "$running" == true ]] || break
    sleep 0.1
  done
  for pid in "${build_pids[@]}"; do
    kill -KILL -- "-$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  done
  rm -rf -- "$build_work_directory"
  return "$status"
}
trap cleanup_builds EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
source_revision=$(git -C "$source_root" rev-parse HEAD)
[[ "$source_revision" =~ ^[a-f0-9]{40}$ ]] || fail 'source revision is invalid'
compute_input_digest() {
  local digest
  digest=$(
    tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner \
      -C "$source_root" -cf - \
      tools/dev/Dockerfile.local-image-supply-chain \
      tools/dev/Dockerfile.local-image-supply-chain.dockerignore \
      infra/dockerfile-frontend/Dockerfile \
      tools/render-image-admission-job.sh \
      infra/admission-tools/Dockerfile \
      services/jobs/role-image-builder \
      services/internal/internal-rpc-authority \
      libs/go |
      sha256sum | awk '{print $1}'
  ) || fail 'supply-chain inputs cannot be read'
  [[ "$digest" =~ ^[a-f0-9]{64}$ ]] || fail 'supply-chain input digest is invalid'
  # VERSION/SOURCE_SHA входят во все recipes: новый commit с тем же деревом
  # не должен возвращать прежнюю binary или OCI revision label из кэша.
  digest=$(printf '%s\n%s\n%s\n' "$digest" "$source_revision" "$component" | sha256sum | awk '{print $1}')
  printf '%s' "$digest"
}
input_digest=$(compute_input_digest)

import_oci() {
  local archive=$1 tag=$2 repository=$3 manifest_digest exact_reference
  manifest_digest=$(tar -xOf "$archive" index.json | jq -er '
    if (.manifests | length) != 1 then error("one image manifest is required")
    else .manifests[0].digest end
  ') || fail "OCI manifest digest is unavailable: $repository"
  [[ "$manifest_digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
    fail "OCI manifest digest is invalid: $repository"
  exact_reference="$repository@$manifest_digest"
  "$source_root/tools/dev/import-local-image.sh" --context "$context" --archive "$archive" \
    --repository "$repository" --tag "$tag" --exact-reference "$exact_reference" >/dev/null ||
    fail "OCI digest readback failed: $repository"
  printf '%s' "$exact_reference"
}

wait_for_build() {
  local index status
  while :; do
    for index in "${!build_pids[@]}"; do
      if [[ ! -f "${build_status_files[$index]}" ]] && kill -0 "${build_pids[$index]}" 2>/dev/null; then
        continue
      fi
      status=0
      wait "${build_pids[$index]}" || status=$?
      unset 'build_pids[index]'
      cat -- "${build_logs[$index]}" >&2
      ((status == 0)) || fail "OCI build failed: ${build_names[$index]} (exit $status)"
      if [[ -n "${build_archives[$index]}" ]]; then
        [[ -s "${build_next_archives[$index]}" ]] || fail "OCI archive was not produced: ${build_names[$index]}"
      fi
      return 0
    done
    sleep 0.1
  done
}

start_build() {
  local name=$1 archive=$2 next_archive=$3 index status_file log_file
  shift 3
  while ((${#build_pids[@]} >= build_jobs)); do wait_for_build; done
  status_file="$build_work_directory/$name.status"
  log_file="$build_work_directory/$name.log"
  # Отдельная группа охватывает CLI, plugin и потомков; отмена не оставляет
  # фоновые сборки. Completion-файл устраняет гонку wait -n с быстрым cache hit.
  # Переменные этой программы раскрываются только внутри дочернего bash.
  # shellcheck disable=SC2016
  setsid --wait bash -c '
    status_file=$1; shift
    status=0
    "$@" || status=$?
    printf "%s\n" "$status" >"$status_file"
    exit "$status"
  ' kodex-local-build "$status_file" "$@" >"$log_file" 2>&1 &
  index=${#build_names[@]}
  build_pids[index]=$!
  build_names+=("$name")
  build_archives+=("$archive")
  build_next_archives+=("$next_archive")
  build_status_files+=("$status_file")
  build_logs+=("$log_file")
}

build_target() {
  local name=$1 dockerfile=$2 target=$3 repository=$4
  shift 4
  local tag archive next_archive
  image_names+=("$name")
  image_repositories+=("$repository")
  tag="$repository:local-$input_digest"
  archive="$state_directory/cache/image-supply-chain/$name-$input_digest.oci.tar"
  if [[ ! -s "$archive" ]]; then
    next_archive="$build_work_directory/$name.oci.tar"
    start_build "$name" "$archive" "$next_archive" docker buildx build --builder "$builder" \
      --file "$source_root/$dockerfile" --target "$target" \
      --platform linux/amd64 --provenance=false --sbom=false \
      --tag "$tag" --output "type=oci,dest=$next_archive" \
      "$@" "$source_root"
  fi
}

import_targets() {
  local index name repository exact_reference
  # Ни один import/readback или новый указатель не предшествует общему barrier.
  while ((${#build_pids[@]} > 0)); do wait_for_build; done
  [[ "$(git -C "$source_root" rev-parse HEAD)" == "$source_revision" &&
    "$(compute_input_digest)" == "$input_digest" ]] || fail 'source changed during supply-chain build'
  for index in "${!build_archives[@]}"; do
    [[ -z "${build_archives[$index]}" ]] ||
      mv -- "${build_next_archives[$index]}" "${build_archives[$index]}"
  done
  for index in "${!image_names[@]}"; do
    name=${image_names[$index]}
    repository=${image_repositories[$index]}
    exact_reference=$(import_oci "$state_directory/cache/image-supply-chain/$name-$input_digest.oci.tar" \
      "$repository:local-$input_digest" "$repository")
    printf '%s\n' "$exact_reference" >"$build_work_directory/$name-image"
    chmod 0600 "$build_work_directory/$name-image"
  done
  # Все digest readback завершены до публикации первого указателя.
  for name in "${image_names[@]}"; do
    mv -- "$build_work_directory/$name-image" "$state_directory/$name-image"
  done
}

# Узкая поставка security binaries не меняет policy или работающие workloads.
if [[ "$component" == authority-security ]]; then
  build_target internal-rpc-authority services/internal/internal-rpc-authority/Dockerfile \
    runtime registry.local.kodex/kodex/internal-rpc-authority \
    --build-arg "VERSION=$source_revision"
  build_target image-admission tools/dev/Dockerfile.local-image-supply-chain \
    image-admission registry.local.kodex/kodex/image-admission \
    --build-arg "SOURCE_SHA=$source_revision"
  import_targets
  image_store_profile=single-host-k3s-image-store
  [[ "$context" != k3d-* ]] || image_store_profile=multi-node-k3d-image-store
  jq -n --arg revision "$source_revision" \
    --arg authority "$(<"$state_directory/internal-rpc-authority-image")" \
    --arg admission "$(<"$state_directory/image-admission-image")" \
    --arg profile "$image_store_profile" \
    '{version:1,profile:$profile,revision:$revision,authorityImage:$authority,imageAdmissionImage:$admission,digestReadback:true}' \
    >"$state_directory/authority-security-images.json"
  chmod 0600 "$state_directory/authority-security-images.json"
  printf 'Authority security images imported with exact digest readback for source %s\n' "$source_revision"
  exit 0
fi

# Совместимый reader обновляется отдельно от signer/registry и других runtime images.
if [[ "$component" == image-admission ]]; then
  build_target image-admission tools/dev/Dockerfile.local-image-supply-chain \
    image-admission registry.local.kodex/kodex/image-admission \
    --build-arg "SOURCE_SHA=$source_revision"
  import_targets
  printf 'Kodex local image admission image is ready for source %s\n' "$source_revision"
  exit 0
fi

build_target image-admission-tools tools/dev/Dockerfile.local-image-supply-chain \
  admission-tools registry.local.kodex/kodex/image-admission-tools \
  --build-arg "SOURCE_SHA=$source_revision"
build_target image-admission tools/dev/Dockerfile.local-image-supply-chain \
  image-admission registry.local.kodex/kodex/image-admission \
  --build-arg "SOURCE_SHA=$source_revision"
build_target role-image-builder services/jobs/role-image-builder/Dockerfile \
  runtime registry.local.kodex/kodex/role-image-builder \
  --build-arg "VERSION=local-$source_revision"
build_target internal-rpc-authority services/internal/internal-rpc-authority/Dockerfile \
  runtime registry.local.kodex/kodex/internal-rpc-authority \
  --build-arg "VERSION=local-$source_revision"

tools_tag="kodex-local/image-admission-tools:$input_digest"
# --load использует тот же builder, лимит и cancel/join barrier.
start_build image-admission-tools-load "" "" docker buildx build --builder "$builder" \
  --file "$source_root/tools/dev/Dockerfile.local-image-supply-chain" \
  --target admission-tools --platform linux/amd64 --provenance=false --sbom=false \
  --build-arg "SOURCE_SHA=$source_revision" --tag "$tools_tag" --load "$source_root" >/dev/null

import_targets
printf '%s\n' "$tools_tag" >"$state_directory/image-supply-chain-tools-docker-tag"

role_input_directory="$state_directory/cache/image-supply-chain/role-input-$source_revision"
role_input_archive="$state_directory/cache/image-supply-chain/role-input-$source_revision.oci.tar"
role_input_metadata="$state_directory/role-image-input.json"
if [[ ! -s "$role_input_archive" || ! -s "$role_input_metadata" ]]; then
  rm -rf -- "$role_input_directory"
  install -d -m 0700 "$role_input_directory/payload/.kodex" "$role_input_directory/layout"
  source_sha256=$(printf '%s' "$source_revision" | sha256sum | awk '{print $1}')
  printf '%s' "$source_sha256" >"$role_input_directory/payload/.kodex/source.sha256"
  tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner \
    -C "$role_input_directory/payload" -cf "$role_input_directory/payload.tar" .kodex/source.sha256
  payload_sha256=$(sha256sum "$role_input_directory/payload.tar" | awk '{print $1}')
  printf '{}' >"$role_input_directory/config.json"
  # The image is non-root by default. Root inside the rootless Docker user
  # namespace maps to the daemon owner and can safely write this private bind
  # mount; forcing the host numeric UID maps to an unwritable subordinate UID.
  manifest_digest=$(docker run --rm --user 0:0 \
    -v "$role_input_directory:/work" "$tools_tag" \
    regctl artifact put \
      --config-type application/vnd.kodex.role-image-input.config.v1+json \
      --config-file /work/config.json \
      --file-media-type application/vnd.kodex.role-image-input.v1 \
      --file /work/payload.tar \
      --format '{{ .Manifest.GetDescriptor.Digest }}' \
      "ocidir:///work/layout:$source_revision")
  [[ "$manifest_digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
    fail 'role image input manifest digest is invalid'
  docker run --rm --user 0:0 \
    -v "$role_input_directory:/work" "$tools_tag" \
    regctl image export "ocidir:///work/layout:$source_revision" /work/role-input.oci.tar
  install -m 0600 "$role_input_directory/role-input.oci.tar" "$role_input_archive"
  jq -n --arg manifest_digest "$manifest_digest" --arg payload_sha256 "$payload_sha256" \
    --arg source_sha256 "$source_sha256" --arg source_revision "$source_revision" '
      {version:1,manifestDigest:$manifest_digest,payloadSha256:$payload_sha256,
       sourceSha256:$source_sha256,sourceRevision:$source_revision}
    ' >"$role_input_metadata"
  chmod 0600 "$role_input_metadata"
fi

printf 'Kodex local image supply-chain images are ready for source %s\n' "$source_revision"
