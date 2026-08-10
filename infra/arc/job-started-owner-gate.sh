#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Runner owner gate failed: %s\n' "$*" >&2; exit 1; }
gate_directory=/var/run/mattercodex-owner-gate

read_gate_value() {
  local name=$1 value
  [[ -r "$gate_directory/$name" ]] || fail "required gate input is absent"
  IFS= read -r value <"$gate_directory/$name"
  [[ -n "$value" ]] || fail "required gate input is empty"
  printf '%s' "$value"
}

expected_workflow_ref=$(read_gate_value expected-workflow-ref)
expected_workflow_sha=$(read_gate_value expected-workflow-sha)
expected_owner_actor_id=$(read_gate_value expected-owner-actor-id)
expected_job=$(read_gate_value expected-job)

[[ "${GITHUB_REPOSITORY:-}" == codex-k8s/matter-codex ]] || fail "repository mismatch"
[[ "${GITHUB_EVENT_NAME:-}" == workflow_dispatch ]] || fail "event mismatch"
[[ "${GITHUB_REF:-}" == refs/heads/main ]] || fail "ref mismatch"
[[ "${GITHUB_WORKFLOW_REF:-}" == "$expected_workflow_ref" ]] || fail "workflow ref mismatch"
[[ "${GITHUB_WORKFLOW_SHA:-}" == "$expected_workflow_sha" ]] || fail "workflow SHA mismatch"
[[ "${GITHUB_SHA:-}" == "$expected_workflow_sha" ]] || fail "source SHA mismatch"
[[ "${GITHUB_ACTOR_ID:-}" == "$expected_owner_actor_id" ]] || fail "owner actor mismatch"
[[ "${GITHUB_JOB:-}" == "$expected_job" ]] || fail "job mismatch"
printf 'Runner owner gate passed\n'
