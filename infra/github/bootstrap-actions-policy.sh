#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'GitHub Actions policy bootstrap failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s --mode preflight|apply|readback|retire-invalid-registration-variable --workflow-sha-file <path> --build-owner-actor-id-file <path> --deploy-owner-actor-id-file <path>\n' "$0" >&2
}

mode=""
workflow_sha_file=""
build_owner_actor_file=""
deploy_owner_actor_file=""
while (($# > 0)); do
  case "$1" in
    --mode) mode="${2:-}"; shift 2 ;;
    --workflow-sha-file) workflow_sha_file="${2:-}"; shift 2 ;;
    --build-owner-actor-id-file) build_owner_actor_file="${2:-}"; shift 2 ;;
    --deploy-owner-actor-id-file) deploy_owner_actor_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
case "$mode" in preflight|apply|readback|retire-invalid-registration-variable) ;;
  *) fail "mode must be preflight, apply, readback or retire-invalid-registration-variable" ;;
esac
[[ -r "$workflow_sha_file" && -r "$build_owner_actor_file" && -r "$deploy_owner_actor_file" ]] ||
  fail "workflow SHA and owner actor ID files are required"
grep -Eq '^[a-f0-9]{40}$' "$workflow_sha_file" || fail "workflow SHA is invalid"
grep -Eq '^[1-9][0-9]*$' "$build_owner_actor_file" || fail "build owner actor ID is invalid"
grep -Eq '^[1-9][0-9]*$' "$deploy_owner_actor_file" || fail "deploy owner actor ID is invalid"
command -v gh >/dev/null 2>&1 || fail "gh is required"
command -v jq >/dev/null 2>&1 || fail "jq is required"
[[ -n "${GH_TOKEN:-}" ]] || fail "GH_TOKEN is required"

repository=codex-k8s/matter-codex
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
gh api "repos/$repository" >"$temporary_directory/repository.json"
jq -e '.id | type == "number" and . > 0' "$temporary_directory/repository.json" >/dev/null ||
  fail "repository ID readback is invalid"
jq -e '.permissions.admin == true' "$temporary_directory/repository.json" >/dev/null ||
  fail "authenticated owner lacks repository administration"
authenticated_owner_actor_id=$(gh api user --jq '.id')
[[ "$authenticated_owner_actor_id" =~ ^[1-9][0-9]*$ ]] || fail "authenticated owner actor ID is invalid"
[[ "$(<"$build_owner_actor_file")" == "$authenticated_owner_actor_id" &&
   "$(<"$deploy_owner_actor_file")" == "$authenticated_owner_actor_id" ]] ||
  fail "owner actor ID files differ from the authenticated GitHub API actor"
workflow_sha=$(<"$workflow_sha_file")

configure_environment() {
  local environment=$1 body branch_body
  body="$temporary_directory/environment-$environment.json"
  jq -n '{wait_timer:0,
    deployment_branch_policy:{protected_branches:false,custom_branch_policies:true}}' >"$body"
  if [[ "$mode" == apply ]]; then
    gh api --method PUT "repos/$repository/environments/$environment" --input "$body" >/dev/null
  fi
  gh api "repos/$repository/environments/$environment" >"$temporary_directory/environment-readback-$environment.json"
  jq -e '
    .deployment_branch_policy.custom_branch_policies == true and
    .deployment_branch_policy.protected_branches == false
  ' "$temporary_directory/environment-readback-$environment.json" >/dev/null ||
    fail "environment protection readback mismatch: $environment"

  gh api "repos/$repository/environments/$environment/deployment-branch-policies" \
    >"$temporary_directory/branches-$environment.json"
  if [[ "$mode" == apply ]] && ! jq -e '
    (.branch_policies | length) == 1 and
    .branch_policies[0].name == "main" and .branch_policies[0].type == "branch"
  ' "$temporary_directory/branches-$environment.json" >/dev/null; then
    while IFS= read -r policy_id; do
      [[ "$policy_id" =~ ^[1-9][0-9]*$ ]] || fail "environment branch policy ID is invalid"
      gh api --method DELETE \
        "repos/$repository/environments/$environment/deployment-branch-policies/$policy_id" >/dev/null
    done < <(jq -r '.branch_policies[].id' "$temporary_directory/branches-$environment.json")
    branch_body="$temporary_directory/branch-$environment.json"
    jq -n '{name:"main",type:"branch"}' >"$branch_body"
    gh api --method POST "repos/$repository/environments/$environment/deployment-branch-policies" \
      --input "$branch_body" >/dev/null
    gh api "repos/$repository/environments/$environment/deployment-branch-policies" \
      >"$temporary_directory/branches-$environment.json"
  fi
  jq -e '.branch_policies as $policies | ($policies | length) == 1 and
    $policies[0].name == "main" and $policies[0].type == "branch"' \
    "$temporary_directory/branches-$environment.json" >/dev/null ||
    fail "environment branch policy readback mismatch: $environment"
}

configure_repository_variable() {
  local variable_name=$1 value_file=$2 body value
  value=$(<"$value_file")
  body="$temporary_directory/variable-$variable_name.json"
  jq -n --arg name "$variable_name" --arg value "$value" '{name:$name,value:$value}' >"$body"
  if [[ "$mode" == apply ]]; then
    if gh api "repos/$repository/actions/variables/$variable_name" >/dev/null 2>&1; then
      gh api --method PATCH "repos/$repository/actions/variables/$variable_name" --input "$body" >/dev/null
    else
      gh api --method POST "repos/$repository/actions/variables" --input "$body" >/dev/null
    fi
  fi
  gh api "repos/$repository/actions/variables/$variable_name" >"$temporary_directory/variable-readback-$variable_name.json"
  jq -e --arg name "$variable_name" --arg value "$value" '.name == $name and .value == $value' \
    "$temporary_directory/variable-readback-$variable_name.json" >/dev/null ||
    fail "repository variable readback mismatch: $variable_name"
}

if [[ "$mode" == preflight ]]; then
  gh api "repos/$repository/environments" >/dev/null
  gh api "repos/$repository/actions/runners?per_page=1" >/dev/null
  gh api "repos/$repository/actions/variables?per_page=1" >/dev/null
  printf 'GitHub Actions policy bootstrap preflight completed\n'
  exit 0
fi

configure_environment production-build
configure_environment production
configure_repository_variable MATTERCODEX_PRODUCTION_WORKFLOW_SHA "$workflow_sha_file"
configure_repository_variable MATTERCODEX_BUILD_OWNER_ACTOR_ID "$build_owner_actor_file"
configure_repository_variable MATTERCODEX_DEPLOY_OWNER_ACTOR_ID "$deploy_owner_actor_file"
if [[ "$mode" == retire-invalid-registration-variable ]]; then
  if gh api "repos/$repository/actions/variables/GH_ARC_TOKEN" \
    >"$temporary_directory/invalid-registration-variable.json" 2>/dev/null; then
    jq -e '.name == "GH_ARC_TOKEN" and
      (.value | type == "string" and length == 29)' \
      "$temporary_directory/invalid-registration-variable.json" >/dev/null ||
      fail "GH_ARC_TOKEN no longer matches the invalid registration-token contract"
    gh api --method DELETE "repos/$repository/actions/variables/GH_ARC_TOKEN" >/dev/null
  fi
  if gh api "repos/$repository/actions/variables/GH_ARC_TOKEN" >/dev/null 2>&1; then
    fail "GH_ARC_TOKEN still exists after retirement"
  fi
fi
printf 'GitHub Actions policy bootstrap %s completed\n' "$mode"
