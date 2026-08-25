#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex release orchestration failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --context <exact-context> --owner-pat-file <path> [--profile web-only] [--public-tls-mode deferred|enabled]\n' \
    "$0" >&2
}

context=""
owner_pat_file=""
profile=web-only
public_tls_mode=enabled
while (($# > 0)); do
  case "$1" in
    --context) context="${2:-}"; shift 2 ;;
    --owner-pat-file) owner_pat_file="${2:-}"; shift 2 ;;
    --profile) profile="${2:-}"; shift 2 ;;
    --public-tls-mode) public_tls_mode="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail 'exact Kubernetes context is required'
[[ "$profile" == web-only || "$profile" == web-with-mattermost ]] || fail 'profile is invalid'
[[ "$public_tls_mode" == deferred || "$public_tls_mode" == enabled ]] ||
  fail 'public TLS mode is invalid'
[[ -f "$owner_pat_file" && -s "$owner_pat_file" && ! -L "$owner_pat_file" ]] ||
  fail 'owner PAT file is invalid'
owner_pat_mode=$(stat -c '%a' "$owner_pat_file")
(((8#$owner_pat_mode & 0077) == 0)) || fail 'owner PAT file permissions are too broad'
for command_name in gh git jq kubectl sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'current Kubernetes context mismatch'

repository=codex-k8s/kodex
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
source_sha=$(git -C "$repository_root" rev-parse HEAD)
[[ "$source_sha" =~ ^[a-f0-9]{40}$ ]] || fail 'source SHA is invalid'
[[ -z "$(git -C "$repository_root" status --porcelain --untracked-files=no)" ]] ||
  fail 'tracked worktree changes are forbidden for a release'
export GH_TOKEN
GH_TOKEN=$(<"$owner_pat_file")
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

find_run() {
  local workflow=$1 title=$2 started_at=$3 deadline=$((SECONDS + 180)) run_id
  while ((SECONDS < deadline)); do
    run_id=$(gh run list --repo "$repository" --workflow "$workflow" \
      --event workflow_dispatch --limit 30 \
      --json databaseId,displayTitle,headSha,createdAt | jq -r \
      --arg title "$title" --arg sha "$source_sha" --arg started_at "$started_at" '
        [.[] | select(.displayTitle == $title and .headSha == $sha and
          .createdAt >= $started_at)] | sort_by(.databaseId) | reverse | .[0].databaseId // empty
      ')
    if [[ "$run_id" =~ ^[1-9][0-9]*$ ]]; then
      printf '%s' "$run_id"
      return
    fi
    sleep 2
  done
  fail "GitHub Actions run was not registered: $workflow"
}

build_title="Build Kodex $source_sha"
build_started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
gh workflow run build-release.yml --repo "$repository" --ref main \
  -f "source_sha=$source_sha" -f "deployment_profile=$profile"
build_run_id=$(find_run build-release.yml "$build_title" "$build_started_at")
gh run watch "$build_run_id" --repo "$repository" --exit-status --interval 10 ||
  fail "release build failed: $build_run_id"
gh run download "$build_run_id" --repo "$repository" \
  --name "release-lock-$source_sha" --dir "$temporary_directory/release-lock"
release_lock="$temporary_directory/release-lock/release-lock.json"
release_lock_sha_file="$temporary_directory/release-lock/release-lock.sha256"
[[ -s "$release_lock" && -s "$release_lock_sha_file" ]] || fail 'release lock artifact is incomplete'
release_lock_sha256=$(<"$release_lock_sha_file")
[[ "$release_lock_sha256" =~ ^[a-f0-9]{64}$ ]] || fail 'release lock SHA-256 is invalid'
[[ "$(sha256sum "$release_lock" | awk '{print $1}')" == "$release_lock_sha256" ]] ||
  fail 'release lock digest mismatch'

render_title="Render Kodex $source_sha from $build_run_id"
render_started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
gh workflow run deploy-production.yml --repo "$repository" --ref main \
  -f "source_sha=$source_sha" -f "build_run_id=$build_run_id" \
  -f "release_lock_sha256=$release_lock_sha256" -f "deployment_profile=$profile"
render_run_id=$(find_run deploy-production.yml "$render_title" "$render_started_at")
gh run watch "$render_run_id" --repo "$repository" --exit-status --interval 10 ||
  fail "release render failed: $render_run_id"
gh run download "$render_run_id" --repo "$repository" \
  --name "$profile-render-$source_sha" --dir "$temporary_directory/render"
render_file="$temporary_directory/render/release.yaml"
render_sha_file="$temporary_directory/render/release.sha256"
[[ -s "$render_file" && -s "$render_sha_file" ]] || fail 'release render artifact is incomplete'
[[ "$(sha256sum "$render_file" | awk '{print $1}')" == "$(<"$render_sha_file")" ]] ||
  fail 'release render digest mismatch'

if [[ "$public_tls_mode" == deferred ]]; then
  "$repository_root/tools/install/deploy-platform.sh" --context "$context" \
    --mode defer-public-tls --render "$render_file" --public-tls-mode deferred
fi
"$repository_root/tools/install/deploy-platform.sh" --context "$context" \
  --mode preflight --render "$render_file" --public-tls-mode "$public_tls_mode"
"$repository_root/tools/install/deploy-platform.sh" --context "$context" \
  --mode apply --render "$render_file" --public-tls-mode "$public_tls_mode"
"$repository_root/tools/install/deploy-platform.sh" --context "$context" \
  --mode readback --render "$render_file" --public-tls-mode "$public_tls_mode"
printf 'Kodex exact release completed: source_sha=%s build_run_id=%s render_run_id=%s\n' \
  "$source_sha" "$build_run_id" "$render_run_id"
