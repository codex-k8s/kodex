#!/usr/bin/env bash
set -euo pipefail

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
verifier="$script_directory/verify-provider-affinity.jq"
verifier_script="$script_directory/verify-provider-affinity.sh"

help_output=$("$verifier_script" --help)
grep -Fq -- '--expect-run <run-ref>=<provider-account-ref>' <<<"$help_output" || {
  printf 'Provider affinity help does not expose the provider account ref contract\n' >&2
  exit 1
}
if grep -Fq -- 'provider-account-key' <<<"$help_output"; then
  printf 'Provider affinity help still exposes the internal provider account key\n' >&2
  exit 1
fi

set +e
invalid_ref_output=$("$verifier_script" \
  --context default \
  --expect-run run_original=default-openai-codex \
  --require-distinct-accounts 1 2>&1)
invalid_ref_status=$?
set -e
[[ "$invalid_ref_status" -eq 2 ]] || {
  printf 'Provider affinity verifier accepted an internal provider account key\n' >&2
  exit 1
}
grep -Fq 'invalid provider account ref for run run_original' <<<"$invalid_ref_output" || {
  printf 'Provider affinity verifier did not report an invalid provider account ref\n' >&2
  exit 1
}

expected_runs='{
  "run_original":"pacc_default1234",
  "run_continue":"pacc_default1234",
  "run_second":"pacc_secondary12"
}'
same_sessions='[{"original":"run_original","continuation":"run_continue"}]'

row() {
  jq -cn \
    --arg run_ref "$1" \
    --arg session_ref "$2" \
    --arg account_ref "$3" \
    --argjson runtime_revision_count "$4" \
    --argjson runtime_account_refs "$5" \
    --argjson runtime_boundary_consistent "${6:-true}" \
    '{
      run_ref: $run_ref,
      found: true,
      session_ref: $session_ref,
      session_account_ref: $account_ref,
      runtime_revision_count: $runtime_revision_count,
      runtime_boundary_consistent: $runtime_boundary_consistent,
      runtime_account_refs: $runtime_account_refs
    }'
}

evaluate() {
  jq -s \
    --argjson expected_runs "$expected_runs" \
    --argjson same_sessions "$same_sessions" \
    --argjson required_distinct_accounts 2 \
    -f "$verifier"
}

assert_rejected() {
  local report=$1 expected_error=$2
  jq -e --arg expected_error "$expected_error" '
    .ok == false and any(.errors[]; contains($expected_error))
  ' <<<"$report" >/dev/null || {
    printf 'Expected affinity rejection was absent: %s\n' "$expected_error" >&2
    exit 1
  }
}

valid_report=$({
  row run_original session_shared pacc_default1234 1 '["pacc_default1234"]'
  row run_continue session_shared pacc_default1234 2 '["pacc_default1234"]'
  row run_second session_second pacc_secondary12 1 '["pacc_secondary12"]'
} | evaluate)
jq -e '
  .ok == true and
  .checked_runs == 3 and
  .checked_session_pairs == 1 and
  (.observed_account_refs | length) == 2
' <<<"$valid_report" >/dev/null

single_account_report=$({
  row run_api_key session_api_key pacc_api_key1234 1 '["pacc_api_key1234"]'
} | jq -s \
  --argjson expected_runs '{"run_api_key":"pacc_api_key1234"}' \
  --argjson same_sessions '[]' \
  --argjson required_distinct_accounts 1 \
  -f "$verifier")
jq -e '
  .ok == true and
  .checked_runs == 1 and
  .checked_session_pairs == 0 and
  .observed_account_refs == ["pacc_api_key1234"]
' <<<"$single_account_report" >/dev/null

missing_run_report=$({
  row run_original session_shared pacc_default1234 1 '["pacc_default1234"]'
  row run_continue session_shared pacc_default1234 1 '["pacc_default1234"]'
} | evaluate)
assert_rejected "$missing_run_report" 'is absent from the database readback'

missing_runtime_report=$({
  row run_original session_shared pacc_default1234 1 '["pacc_default1234"]'
  row run_continue session_shared pacc_default1234 0 '[]'
  row run_second session_second pacc_secondary12 1 '["pacc_secondary12"]'
} | evaluate)
assert_rejected "$missing_runtime_report" 'has no materialized runtime revision'

runtime_account_mismatch_report=$({
  row run_original session_shared pacc_default1234 1 '["pacc_default1234"]'
  row run_continue session_shared pacc_default1234 1 '["pacc_secondary12"]'
  row run_second session_second pacc_secondary12 1 '["pacc_secondary12"]'
} | evaluate)
assert_rejected "$runtime_account_mismatch_report" 'runtime revisions do not exclusively use account ref'

runtime_boundary_report=$({
  row run_original session_shared pacc_default1234 1 '["pacc_default1234"]' false
  row run_continue session_shared pacc_default1234 1 '["pacc_default1234"]'
  row run_second session_second pacc_secondary12 1 '["pacc_secondary12"]'
} | evaluate)
assert_rejected "$runtime_boundary_report" 'cross the run session or organization boundary'

session_drift_report=$({
  row run_original session_original pacc_default1234 1 '["pacc_default1234"]'
  row run_continue session_changed pacc_default1234 1 '["pacc_default1234"]'
  row run_second session_second pacc_secondary12 1 '["pacc_secondary12"]'
} | evaluate)
assert_rejected "$session_drift_report" 'changed logical session'

account_mismatch_report=$({
  row run_original session_shared pacc_secondary12 1 '["pacc_secondary12"]'
  row run_continue session_shared pacc_secondary12 1 '["pacc_secondary12"]'
  row run_second session_second pacc_secondary12 1 '["pacc_secondary12"]'
} | evaluate)
assert_rejected "$account_mismatch_report" 'uses session account ref'
assert_rejected "$account_mismatch_report" 'expected at least 2 distinct provider account refs'

printf 'Provider affinity parser tests passed\n'
