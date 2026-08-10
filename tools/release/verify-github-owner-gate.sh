#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'GitHub owner gate verification failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s --workflow <path> --environment <name> --workflow-sha <40-hex> --owner-actor-id <numeric-id> --source-sha <40-hex> --mode build|dark|cutover|rollback --output <path>\n' "$0" >&2
}

workflow=""
environment=""
workflow_sha=""
owner_actor_id=""
source_sha=""
mode=""
output=""
while (($# > 0)); do
  case "$1" in
    --workflow) workflow="${2:-}"; shift 2 ;;
    --environment) environment="${2:-}"; shift 2 ;;
    --workflow-sha) workflow_sha="${2:-}"; shift 2 ;;
    --owner-actor-id) owner_actor_id="${2:-}"; shift 2 ;;
    --source-sha) source_sha="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$workflow" == .github/workflows/build-release.yml || "$workflow" == .github/workflows/deploy-production.yml ]] ||
  fail "workflow path is not allowlisted"
[[ "$environment" == production-build || "$environment" == production ]] || fail "environment is not allowlisted"
[[ "$workflow_sha" =~ ^[a-f0-9]{40}$ ]] || fail "workflow SHA must be exact lowercase 40-hex"
[[ "$owner_actor_id" =~ ^[1-9][0-9]*$ ]] || fail "owner actor ID is invalid"
[[ "$source_sha" =~ ^[a-f0-9]{40}$ ]] || fail "source SHA must be exact lowercase 40-hex"
case "$mode" in build|dark|cutover|rollback) ;; *) fail "mode is invalid" ;; esac
[[ -n "$output" ]] || fail "output path is required"
for variable_name in GH_TOKEN GITHUB_REPOSITORY GITHUB_RUN_ID GITHUB_WORKFLOW_REF GITHUB_API_URL; do
  [[ -n "${!variable_name:-}" ]] || fail "$variable_name is required"
done
[[ "$GITHUB_REPOSITORY" == codex-k8s/matter-codex ]] || fail "repository mismatch"
expected_workflow_ref="$GITHUB_REPOSITORY/$workflow@refs/heads/main"
[[ "$GITHUB_WORKFLOW_REF" == "$expected_workflow_ref" ]] || fail "workflow ref is not exact main"
[[ "$GITHUB_RUN_ID" =~ ^[0-9]+$ ]] || fail "run ID is invalid"

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
umask 077
gh api "repos/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID" >"$temporary_directory/run.json"
gh api "repos/$GITHUB_REPOSITORY" >"$temporary_directory/repository.json"

jq -e --arg repository "$GITHUB_REPOSITORY" --arg workflow "$workflow" '
  .event == "workflow_dispatch" and .head_branch == "main" and
  .head_repository.full_name == $repository and .path == $workflow and
  (.status == "in_progress" or .status == "queued") and
  (.head_sha | test("^[a-f0-9]{40}$")) and
  (.actor.id | type == "number") and (.triggering_actor.id | type == "number")
' "$temporary_directory/run.json" >/dev/null || fail "workflow run provenance mismatch"
jq -e '.default_branch == "main"' "$temporary_directory/repository.json" >/dev/null ||
  fail "repository default branch is not main"
workflow_head_sha=$(jq -r '.head_sha' "$temporary_directory/run.json")
[[ "$workflow_head_sha" == "$workflow_sha" ]] || fail "workflow run is not pinned to the owner-authorized SHA"
run_actor_id=$(jq -r '.actor.id' "$temporary_directory/run.json")
triggering_actor_id=$(jq -r '.triggering_actor.id' "$temporary_directory/run.json")
[[ "$run_actor_id" == "$owner_actor_id" && "$triggering_actor_id" == "$owner_actor_id" ]] ||
  fail "workflow dispatch and rerun actor are not the owner-authorized identity"
if [[ "$mode" != rollback && "$workflow_head_sha" != "$source_sha" ]]; then
  fail "requested source SHA is not the exact workflow main SHA"
fi
gh api "repos/$GITHUB_REPOSITORY/commits/$source_sha" --jq '.sha' | grep -qx "$source_sha" ||
  fail "source SHA is not reachable in the repository"

deployments="$temporary_directory/deployments.json"
gh api "repos/$GITHUB_REPOSITORY/deployments?environment=$environment&per_page=100" >"$deployments"
matching_deployments=0
while IFS= read -r deployment_id; do
  [[ "$deployment_id" =~ ^[1-9][0-9]*$ ]] || fail "environment deployment ID is invalid"
  statuses="$temporary_directory/deployment-statuses-$deployment_id.json"
  gh api "repos/$GITHUB_REPOSITORY/deployments/$deployment_id/statuses?per_page=100" >"$statuses"
  if jq -e --arg environment "$environment" \
    --arg run_path "/actions/runs/$GITHUB_RUN_ID/job/" '
      any(.[];
        .environment == $environment and
        (.state == "waiting" or .state == "queued" or .state == "in_progress") and
        (.log_url | type == "string" and contains($run_path)))
    ' "$statuses" >/dev/null; then
    matching_deployments=$((matching_deployments + 1))
  fi
done < <(jq -r --arg environment "$environment" --arg sha "$workflow_sha" \
  --argjson owner_actor_id "$owner_actor_id" '
    .[] | select(
      .environment == $environment and .ref == "main" and .sha == $sha and
      .task == "deploy" and .creator.id == $owner_actor_id) | .id
  ' "$deployments")
[[ $matching_deployments -eq 1 ]] || fail "exact environment deployment readback mismatch"

jq -n \
  --arg repository "$GITHUB_REPOSITORY" \
  --arg workflow "$workflow" \
  --arg workflow_ref "$expected_workflow_ref" \
  --arg environment "$environment" \
  --arg workflow_sha "$workflow_sha" \
  --arg source_sha "$source_sha" \
  --arg mode "$mode" \
  --arg run_id "$GITHUB_RUN_ID" \
  --arg workflow_head_sha "$workflow_head_sha" \
  '{schema_version:2,repository:$repository,workflow:$workflow,workflow_ref:$workflow_ref,
    environment:$environment,workflow_sha:$workflow_sha,source_sha:$source_sha,mode:$mode,run_id:$run_id,
    workflow_head_sha:$workflow_head_sha,owner_actor_verified:true}' >"$output"
printf 'GitHub owner gate verified for %s\n' "$environment"
