#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)

fail() {
  printf 'Local image cache import contract failed: %s\n' "$*" >&2
  exit 1
}

for builder in build-local-runner.sh build-local-session-archive.sh build-local-backup-controller.sh build-local-stt.sh; do
  path="$root/tools/dev/$builder"
  grep -Fq 'images import \' "$path" || fail "$builder does not import its cached OCI archive"
  grep -Fq -- '--base-name "$repository" "$archive"' "$path" ||
    fail "$builder does not restore the exact repository tag from cache"
  import_line=$(grep -n -F 'images import \' "$path" | cut -d: -f1)
  tag_line=$(grep -n -F 'images tag --force' "$path" | cut -d: -f1)
  [[ "$import_line" =~ ^[0-9]+$ && "$tag_line" =~ ^[0-9]+$ && "$import_line" -lt "$tag_line" ]] ||
    fail "$builder tags the exact digest before restoring its source tag"
done

for builder in build-local-runner.sh build-local-session-archive.sh build-local-backup-controller.sh build-local-stt.sh build-local-image-supply-chain.sh; do
  path="$root/tools/dev/$builder"
  grep -Fq '"$source_root/tools/dev/ensure-local-buildx-builder.sh" "$builder"' "$path" ||
    fail "$builder does not repair the Buildx builder for the current Docker context"
done

buildx_bootstrap="$root/tools/dev/ensure-local-buildx-builder.sh"
grep -Fq "grep -Eq '^Status:[[:space:]]+running$'" "$buildx_bootstrap" ||
  fail 'Buildx bootstrap does not verify the effective running status'
grep -Fq 'docker context show' "$buildx_bootstrap" ||
  fail 'Buildx bootstrap does not bind a replacement to the current Docker context'

dev_script="$root/dev.sh"
[[ $(grep -Fc '"$repository_root/tools/dev/build-local-stt.sh"' "$dev_script") -eq 2 ]] ||
  fail 'dev.sh must restore the STT image during up and e2e'
[[ $(grep -Fc '"$repository_root/tools/dev/build-local-session-archive.sh"' "$dev_script") -eq 2 ]] ||
  fail 'dev.sh must restore the session archive worker image during up and e2e'
e2e_restore_line=$(grep -Fn '"$repository_root/tools/dev/build-local-session-archive.sh"' "$dev_script" | head -1 | cut -d: -f1)
deployment_readback_line=$(grep -Fn '"$repository_root/tools/dev/deploy-local.sh" --context "$context" --mode readback' "$dev_script" | head -1 | cut -d: -f1)
[[ "$e2e_restore_line" =~ ^[0-9]+$ && "$deployment_readback_line" =~ ^[0-9]+$ &&
  "$e2e_restore_line" -lt "$deployment_readback_line" ]] ||
  fail 'dev.sh does not restore the session archive worker image before E2E deployment readback'
e2e_guard_line=$(grep -Fn 'if [[ "$command_name" == e2e ]]; then' "$dev_script" |
  awk -F: -v restore="$e2e_restore_line" '$1 < restore { line = $1 } END { print line }')
e2e_guard_end_line=$(awk -v start="$e2e_guard_line" \
  'NR > start && /^[[:space:]]*fi[[:space:]]*$/ { print NR; exit }' "$dev_script")
[[ "$e2e_guard_line" =~ ^[0-9]+$ && "$e2e_guard_end_line" =~ ^[0-9]+$ &&
  "$e2e_guard_line" -lt "$e2e_restore_line" && "$e2e_restore_line" -lt "$e2e_guard_end_line" ]] ||
  fail 'session archive E2E image restore is not guarded by the e2e command'

runner_builder="$root/tools/dev/build-local-runner.sh"
runner_verify_line=$(grep -n -F 'python3 -B "$verifier" "$provenance_phase"' "$runner_builder" | cut -d: -f1)
runner_import_line=$(grep -n -F 'images import \' "$runner_builder" | cut -d: -f1)
[[ "$runner_verify_line" =~ ^[0-9]+$ && "$runner_verify_line" -lt "$runner_import_line" ]] ||
  fail 'runner import must follow exact OCI provenance verification'
grep -Fq 'input_digest=$(python3 -B "$verifier" input' "$runner_builder" ||
  fail 'runner builder must share the verifier input digest algorithm'
grep -Fq 'then provenance_phase=check; fi' "$runner_builder" ||
  fail 'runner cache hit must check existing immutable provenance'
runner_cache_line=$(grep -n -F 'if [[ ! -s "$archive" ]]; then' "$runner_builder" | cut -d: -f1)
runner_bootstrap_line=$(grep -n -F '"$source_root/tools/dev/ensure-local-buildx-builder.sh" "$builder"' "$runner_builder" | cut -d: -f1)
runner_build_line=$(grep -n -F 'docker buildx build --builder' "$runner_builder" | cut -d: -f1)
runner_cache_end=$(awk -v start="$runner_cache_line" 'NR > start && /^fi$/ { print NR; exit }' "$runner_builder")
[[ "$runner_cache_line" -lt "$runner_bootstrap_line" && "$runner_bootstrap_line" -lt "$runner_build_line" &&
   "$runner_build_line" -lt "$runner_cache_end" && "$runner_cache_end" -lt "$runner_verify_line" ]] ||
  fail 'runner cache hit must not bootstrap or rebuild the unchanged image'

printf 'Local image cache import contract passed.\n'
