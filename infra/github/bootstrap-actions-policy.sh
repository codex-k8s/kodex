#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'GitHub Actions policy bootstrap failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s --mode preflight|apply|readback --build-reviewer-team-id-file <path> --deploy-reviewer-team-id-file <path>\n' "$0" >&2
}

mode=""
build_reviewer_file=""
deploy_reviewer_file=""
while (($# > 0)); do
  case "$1" in
    --mode) mode="${2:-}"; shift 2 ;;
    --build-reviewer-team-id-file) build_reviewer_file="${2:-}"; shift 2 ;;
    --deploy-reviewer-team-id-file) deploy_reviewer_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
case "$mode" in preflight|apply|readback) ;; *) fail "mode must be preflight, apply or readback" ;; esac
[[ -r "$build_reviewer_file" && -r "$deploy_reviewer_file" ]] || fail "reviewer team ID files are required"
grep -Eq '^[1-9][0-9]*$' "$build_reviewer_file" || fail "build reviewer team ID is invalid"
grep -Eq '^[1-9][0-9]*$' "$deploy_reviewer_file" || fail "deploy reviewer team ID is invalid"
command -v gh >/dev/null 2>&1 || fail "gh is required"
command -v jq >/dev/null 2>&1 || fail "jq is required"
[[ -n "${GH_TOKEN:-}" ]] || fail "GH_TOKEN is required"

repository=codex-k8s/matter-codex
organization=codex-k8s
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
umask 077
repository_id=$(gh api "repos/$repository" --jq '.id')
[[ "$repository_id" =~ ^[1-9][0-9]*$ ]] || fail "repository ID readback is invalid"

configure_environment() {
  local environment=$1 reviewer_file=$2 reviewer_id body branch_body
  reviewer_id=$(<"$reviewer_file")
  body="$temporary_directory/environment-$environment.json"
  jq -n --argjson reviewer_id "$reviewer_id" '{wait_timer:0,prevent_self_review:true,
    reviewers:[{type:"Team",id:$reviewer_id}],
    deployment_branch_policy:{protected_branches:false,custom_branch_policies:true}}' >"$body"
  if [[ "$mode" == apply ]]; then
    gh api --method PUT "repos/$repository/environments/$environment" --input "$body" >/dev/null
  fi
  gh api "repos/$repository/environments/$environment" >"$temporary_directory/environment-readback-$environment.json"
  jq -e --argjson reviewer_id "$reviewer_id" '
    any(.protection_rules[]?; .type == "required_reviewers" and .prevent_self_review == true and
      ([.reviewers[]?.reviewer.id] == [$reviewer_id])) and
    .deployment_branch_policy.custom_branch_policies == true and
    .deployment_branch_policy.protected_branches == false
  ' "$temporary_directory/environment-readback-$environment.json" >/dev/null ||
    fail "environment protection readback mismatch: $environment"

  gh api "repos/$repository/environments/$environment/deployment-branch-policies" \
    >"$temporary_directory/branches-$environment.json"
  branch_count=$(jq '.branch_policies | length' "$temporary_directory/branches-$environment.json")
  if [[ "$mode" == apply && "$branch_count" == 0 ]]; then
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

runner_group_id() {
  local group_name=$1 groups matches
  groups=$(gh api "orgs/$organization/actions/runner-groups?per_page=100")
  matches=$(jq --arg name "$group_name" '[.runner_groups[] | select(.name == $name)] | length' <<<"$groups")
  ((matches <= 1)) || fail "runner group name is not unique: $group_name"
  if ((matches == 1)); then
    jq -er --arg name "$group_name" '.runner_groups[] | select(.name == $name) | .id' <<<"$groups"
  fi
}

configure_runner_group() {
  local group_name=$1 workflow_path=$2 group_id body
  group_id=$(runner_group_id "$group_name")
  body="$temporary_directory/runner-group-$group_name.json"
  jq -n --arg name "$group_name" --arg workflow "$repository/$workflow_path@refs/heads/main" \
    '{name:$name,visibility:"selected",allows_public_repositories:false,
      restricted_to_workflows:true,selected_workflows:[$workflow]}' >"$body"
  if [[ "$mode" == apply && -z "$group_id" ]]; then
    group_id=$(gh api --method POST "orgs/$organization/actions/runner-groups" --input "$body" --jq '.id')
  elif [[ "$mode" == apply ]]; then
    gh api --method PATCH "orgs/$organization/actions/runner-groups/$group_id" --input "$body" >/dev/null
  fi
  [[ "$group_id" =~ ^[1-9][0-9]*$ ]] || fail "runner group is absent: $group_name"
  if [[ "$mode" == apply ]]; then
    gh api --method PUT "orgs/$organization/actions/runner-groups/$group_id/repositories/$repository_id" >/dev/null
  fi
  gh api "orgs/$organization/actions/runner-groups/$group_id" >"$temporary_directory/group-$group_name.json"
  gh api "orgs/$organization/actions/runner-groups/$group_id/repositories?per_page=100" \
    >"$temporary_directory/group-repositories-$group_name.json"
  jq -e --arg name "$group_name" --arg workflow "$repository/$workflow_path@refs/heads/main" '
    .name == $name and .visibility == "selected" and .allows_public_repositories == false and
    .restricted_to_workflows == true and .selected_workflows == [$workflow]
  ' "$temporary_directory/group-$group_name.json" >/dev/null || fail "runner group policy mismatch: $group_name"
  jq -e --argjson repository_id "$repository_id" '
    [.repositories[].id] == [$repository_id]
  ' "$temporary_directory/group-repositories-$group_name.json" >/dev/null ||
    fail "runner group repository scope mismatch: $group_name"
}

if [[ "$mode" == preflight ]]; then
  gh api "repos/$repository/environments" >/dev/null
  gh api "orgs/$organization/actions/runner-groups" >/dev/null
  printf 'GitHub Actions policy bootstrap preflight completed\n'
  exit 0
fi

configure_environment production-build "$build_reviewer_file"
configure_environment production "$deploy_reviewer_file"
configure_runner_group mattercodex-production-build .github/workflows/build-release.yml
configure_runner_group mattercodex-production-deploy .github/workflows/deploy-production.yml
printf 'GitHub Actions policy bootstrap %s completed\n' "$mode"
