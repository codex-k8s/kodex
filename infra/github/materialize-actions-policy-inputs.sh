#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'GitHub Actions policy input materialization failed: %s\n' "$*" >&2; exit 1; }
usage() { printf 'Usage: %s --output-directory <secure-path>\n' "$0" >&2; }

output_directory=""
while (($# > 0)); do
  case "$1" in
    --output-directory) output_directory="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$output_directory" ]] || fail "output directory is required"
for command_name in gh git jq stat; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ -n "${GH_TOKEN:-}" ]] || fail "GH_TOKEN is required"

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
[[ "$(git -C "$repository_root" symbolic-ref --short HEAD)" == main ]] ||
  fail "policy inputs must be materialized from the main branch"
[[ -z "$(git -C "$repository_root" status --porcelain)" ]] ||
  fail "main worktree must be clean"
workflow_sha=$(git -C "$repository_root" rev-parse HEAD)
[[ "$workflow_sha" =~ ^[a-f0-9]{40}$ ]] || fail "current main SHA is invalid"
[[ "$(git -C "$repository_root" rev-parse refs/remotes/origin/main)" == "$workflow_sha" ]] ||
  fail "current main differs from origin/main"

repository=codex-k8s/matter-codex
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
gh api "repos/$repository" >"$temporary_directory/repository.json"
jq -e '.permissions.admin == true' "$temporary_directory/repository.json" >/dev/null ||
  fail "authenticated owner lacks repository administration"
gh api "repos/$repository/commits/$workflow_sha" >"$temporary_directory/commit.json"
jq -e --arg sha "$workflow_sha" '.sha == $sha' "$temporary_directory/commit.json" >/dev/null ||
  fail "current main SHA is not an exact repository commit"
owner_actor_id=$(gh api user --jq '.id')
[[ "$owner_actor_id" =~ ^[1-9][0-9]*$ ]] || fail "authenticated owner actor ID is invalid"

mkdir -p -- "$output_directory"
output_directory=$(cd -- "$output_directory" && pwd -P)
[[ "$output_directory" != "$repository_root" && "$output_directory" != "$repository_root/"* ]] ||
  fail "policy inputs must not be written inside the repository"
output_mode=$(stat -c '%a' "$output_directory")
(( (8#$output_mode & 077) == 0 )) || fail "output directory permissions are too broad"

write_input() {
  local name=$1 value=$2 temporary
  temporary=$(mktemp "$output_directory/.${name}.XXXXXX")
  printf '%s\n' "$value" >"$temporary"
  chmod 0600 "$temporary"
  mv -f -- "$temporary" "$output_directory/$name"
}

write_input workflow-sha "$workflow_sha"
write_input build-owner-actor-id "$owner_actor_id"
write_input deploy-owner-actor-id "$owner_actor_id"
printf 'GitHub Actions policy inputs materialized without values\n'
