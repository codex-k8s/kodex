#!/usr/bin/env bash
set -euo pipefail

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
verifier="$script_directory/verify-provider-affinity.jq"

expected_runs='{
  "run_original":"default-openai-codex",
  "run_continue":"default-openai-codex",
  "run_second":"openai-codex-account-2"
}'
same_sessions='[{"original":"run_original","continuation":"run_continue"}]'

row() {
  jq -cn \
    --arg run_ref "$1" \
    --arg session_ref "$2" \
    --arg account_key "$3" \
    --argjson runtime_revision_count "$4" \
    --argjson runtime_account_keys "$5" \
    --argjson runtime_boundary_consistent "${6:-true}" \
    '{
      run_ref: $run_ref,
      found: true,
      session_ref: $session_ref,
      session_account_key: $account_key,
      runtime_revision_count: $runtime_revision_count,
      runtime_boundary_consistent: $runtime_boundary_consistent,
      runtime_account_keys: $runtime_account_keys
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
  row run_original session_shared default-openai-codex 1 '["default-openai-codex"]'
  row run_continue session_shared default-openai-codex 2 '["default-openai-codex"]'
  row run_second session_second openai-codex-account-2 1 '["openai-codex-account-2"]'
} | evaluate)
jq -e '
  .ok == true and
  .checked_runs == 3 and
  .checked_session_pairs == 1 and
  (.observed_accounts | length) == 2
' <<<"$valid_report" >/dev/null

missing_run_report=$({
  row run_original session_shared default-openai-codex 1 '["default-openai-codex"]'
  row run_continue session_shared default-openai-codex 1 '["default-openai-codex"]'
} | evaluate)
assert_rejected "$missing_run_report" 'is absent from the database readback'

missing_runtime_report=$({
  row run_original session_shared default-openai-codex 1 '["default-openai-codex"]'
  row run_continue session_shared default-openai-codex 0 '[]'
  row run_second session_second openai-codex-account-2 1 '["openai-codex-account-2"]'
} | evaluate)
assert_rejected "$missing_runtime_report" 'has no materialized runtime revision'

runtime_account_mismatch_report=$({
  row run_original session_shared default-openai-codex 1 '["default-openai-codex"]'
  row run_continue session_shared default-openai-codex 1 '["openai-codex-account-2"]'
  row run_second session_second openai-codex-account-2 1 '["openai-codex-account-2"]'
} | evaluate)
assert_rejected "$runtime_account_mismatch_report" 'runtime revisions do not exclusively use account'

runtime_boundary_report=$({
  row run_original session_shared default-openai-codex 1 '["default-openai-codex"]' false
  row run_continue session_shared default-openai-codex 1 '["default-openai-codex"]'
  row run_second session_second openai-codex-account-2 1 '["openai-codex-account-2"]'
} | evaluate)
assert_rejected "$runtime_boundary_report" 'cross the run session or organization boundary'

session_drift_report=$({
  row run_original session_original default-openai-codex 1 '["default-openai-codex"]'
  row run_continue session_changed default-openai-codex 1 '["default-openai-codex"]'
  row run_second session_second openai-codex-account-2 1 '["openai-codex-account-2"]'
} | evaluate)
assert_rejected "$session_drift_report" 'changed logical session'

account_mismatch_report=$({
  row run_original session_shared openai-codex-account-2 1 '["openai-codex-account-2"]'
  row run_continue session_shared openai-codex-account-2 1 '["openai-codex-account-2"]'
  row run_second session_second openai-codex-account-2 1 '["openai-codex-account-2"]'
} | evaluate)
assert_rejected "$account_mismatch_report" 'uses session account'
assert_rejected "$account_mismatch_report" 'expected at least 2 distinct provider accounts'

printf 'Provider affinity parser tests passed\n'
