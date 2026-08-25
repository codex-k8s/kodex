#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'GitHub build run verification failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s --build-run-id <digits> --source-sha <40-hex> --owner-actor-id <numeric-id> --output <path>\n' "$0" >&2
}

build_run_id=""
source_sha=""
owner_actor_id=""
output=""
while (($# > 0)); do
  case "$1" in
    --build-run-id) build_run_id="${2:-}"; shift 2 ;;
    --source-sha) source_sha="${2:-}"; shift 2 ;;
    --owner-actor-id) owner_actor_id="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$build_run_id" =~ ^[1-9][0-9]*$ ]] || fail "build run ID is invalid"
[[ "$source_sha" =~ ^[a-f0-9]{40}$ ]] || fail "source SHA must be exact lowercase 40-hex"
[[ "$owner_actor_id" =~ ^[1-9][0-9]*$ ]] || fail "owner actor ID is invalid"
[[ -n "$output" ]] || fail "output path is required"
for variable_name in GH_TOKEN GITHUB_REPOSITORY GITHUB_API_URL; do
  [[ -n "${!variable_name:-}" ]] || fail "$variable_name is required"
done
for command_name in curl jq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$GITHUB_REPOSITORY" == codex-k8s/kodex ]] || fail "repository mismatch"

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
umask 077
curl_config="$temporary_directory/curl.conf"
printf 'header = "Authorization: Bearer %s"\nheader = "Accept: application/vnd.github+json"\n' \
  "$GH_TOKEN" >"$curl_config"
unset GH_TOKEN
github_api_get() {
  local path=$1 destination=$2
  curl --config "$curl_config" --fail --silent --show-error \
    "$GITHUB_API_URL/$path" >"$destination"
}

run_file="$temporary_directory/run.json"
jobs_file="$temporary_directory/jobs.json"
github_api_get "repos/$GITHUB_REPOSITORY/actions/runs/$build_run_id" "$run_file"
github_api_get "repos/$GITHUB_REPOSITORY/actions/runs/$build_run_id/jobs?filter=latest&per_page=100" "$jobs_file"

# GitHub can leave the aggregate workflow status stale after the sole ARC job and
# its Environment deployment have both completed. The job and Environment
# records are the authoritative bounded fallback; a terminal failed aggregate
# run is never accepted.
jq -e --arg repository "$GITHUB_REPOSITORY" --arg source_sha "$source_sha" \
  --argjson owner_actor_id "$owner_actor_id" '
    .event == "workflow_dispatch" and .head_branch == "main" and
    .head_sha == $source_sha and .head_repository.full_name == $repository and
    .path == ".github/workflows/build-release.yml" and
    .actor.id == $owner_actor_id and .triggering_actor.id == $owner_actor_id and
    (
      (.status == "completed" and .conclusion == "success") or
      ((.status == "queued" or .status == "in_progress") and .conclusion == null)
    )
  ' "$run_file" >/dev/null || fail "workflow run provenance mismatch"

jq -e --arg run_id "$build_run_id" '
    .total_count == 1 and (.jobs | length) == 1 and
    .jobs[0].name == "build" and
    .jobs[0].status == "completed" and .jobs[0].conclusion == "success" and
    (.jobs[0].labels | index("kodex-build") != null) and
    (.jobs[0].runner_name | type == "string" and startswith("kodex-build-")) and
    (.jobs[0].html_url | type == "string" and contains("/actions/runs/" + $run_id + "/job/"))
  ' "$jobs_file" >/dev/null || fail "exact successful build job was not found"
build_job_id=$(jq -r '.jobs[0].id' "$jobs_file")
[[ "$build_job_id" =~ ^[1-9][0-9]*$ ]] || fail "build job ID is invalid"

deployments_file="$temporary_directory/deployments.json"
github_api_get "repos/$GITHUB_REPOSITORY/deployments?environment=production-build&per_page=100" \
  "$deployments_file"
matching_deployments=0
while IFS= read -r deployment_id; do
  [[ "$deployment_id" =~ ^[1-9][0-9]*$ ]] || fail "build deployment ID is invalid"
  statuses_file="$temporary_directory/deployment-statuses-$deployment_id.json"
  github_api_get "repos/$GITHUB_REPOSITORY/deployments/$deployment_id/statuses?per_page=100" \
    "$statuses_file"
  if jq -e --arg run_path "/actions/runs/$build_run_id/job/$build_job_id" '
      any(.[];
        .environment == "production-build" and .state == "success" and
        (.log_url | type == "string" and contains($run_path)))
    ' "$statuses_file" >/dev/null; then
    matching_deployments=$((matching_deployments + 1))
  fi
done < <(jq -r --arg source_sha "$source_sha" --argjson owner_actor_id "$owner_actor_id" '
  .[] | select(
    .environment == "production-build" and .ref == "main" and .sha == $source_sha and
    .task == "deploy" and .creator.id == $owner_actor_id) | .id
' "$deployments_file")
[[ $matching_deployments -eq 1 ]] || fail "exact successful production-build deployment readback mismatch"

jq -n \
  --arg repository "$GITHUB_REPOSITORY" \
  --arg source_sha "$source_sha" \
  --arg build_run_id "$build_run_id" \
  --arg build_job_id "$build_job_id" \
  --arg aggregate_status "$(jq -r '.status' "$run_file")" \
  '{schema_version:1,repository:$repository,workflow:".github/workflows/build-release.yml",
    environment:"production-build",source_sha:$source_sha,build_run_id:$build_run_id,
    build_job_id:$build_job_id,aggregate_status:$aggregate_status,
    owner_actor_verified:true,job_success_verified:true,environment_success_verified:true}' >"$output"
printf 'GitHub build run verified for production-build\n'
