#!/usr/bin/env bash
set -euo pipefail

readonly EXIT_USAGE=2
readonly EXIT_ENVIRONMENT=3
readonly EXIT_VERIFICATION=4
readonly EXIT_INTERNAL=5
readonly NAMESPACE=kodex-system
readonly POSTGRES_POD=kodex-postgresql-0

unexpected_error() {
  local original_exit=$?
  trap - ERR
  printf 'Kodex provider affinity verifier internal error: unexpected command failure (source exit %s)\n' \
    "$original_exit" >&2
  exit "$EXIT_INTERNAL"
}

trap unexpected_error ERR

usage() {
  cat <<'EOF'
Usage:
  verify-provider-affinity.sh --context <exact-context> \
    --expect-run <run-ref>=<provider-account-ref> [--expect-run ...] \
    [--expect-same-session <original-run-ref>=<continuation-run-ref> ...] \
    [--require-distinct-accounts <count>]

Read-only local E2E verification:
  * every expected run has an executed runtime revision on the expected account;
  * every original/continuation pair keeps one logical session and account;
  * the expected run set proves use of at least two distinct accounts by default.

Options:
  --context <name>                    Required exact local Kubernetes context.
  --kubeconfig <path>                 Optional kubeconfig; defaults to KUBECONFIG.
  --expect-run <run>=<account-ref>    Repeatable expected run/provider account ref binding.
  --expect-same-session <run>=<run>   Repeatable original/continuation pair.
  --require-distinct-accounts <n>     Minimum observed accounts; default: 2, minimum: 1.
  --help                              Show this help.

Exit codes:
  0  Verification passed (or help was shown).
  2  Invalid command line or unsafe input.
  3  Kubernetes or PostgreSQL environment is unavailable or not local.
  4  Provider/session/run affinity verification failed.
  5  Internal verifier/parser failure.
EOF
}

fail_usage() {
  printf 'Kodex provider affinity verifier usage error: %s\n' "$*" >&2
  exit "$EXIT_USAGE"
}

fail_environment() {
  printf 'Kodex provider affinity verifier environment error: %s\n' "$*" >&2
  exit "$EXIT_ENVIRONMENT"
}

fail_internal() {
  printf 'Kodex provider affinity verifier internal error: %s\n' "$*" >&2
  exit "$EXIT_INTERNAL"
}

context=""
kubeconfig="${KUBECONFIG:-}"
required_distinct_accounts=2
declare -A expected_account_ref_by_run=()
declare -a expected_run_order=()
declare -a same_session_pairs=()

validate_run_ref() {
  [[ "$1" =~ ^[A-Za-z0-9_-]{8,96}$ ]]
}

validate_account_ref() {
  [[ "$1" =~ ^pacc_[A-Za-z0-9_-]{8,88}$ ]]
}

add_expected_run() {
  local value=$1 run_ref account_ref
  [[ "$value" == *=* ]] || fail_usage '--expect-run must use <run-ref>=<provider-account-ref>'
  run_ref=${value%%=*}
  account_ref=${value#*=}
  validate_run_ref "$run_ref" || fail_usage "invalid run ref: $run_ref"
  validate_account_ref "$account_ref" || fail_usage "invalid provider account ref for run $run_ref"
  [[ -z "${expected_account_ref_by_run[$run_ref]+present}" ]] ||
    fail_usage "duplicate expected run: $run_ref"
  expected_account_ref_by_run[$run_ref]=$account_ref
  expected_run_order+=("$run_ref")
}

add_same_session_pair() {
  local value=$1 original continuation
  [[ "$value" == *=* ]] || fail_usage '--expect-same-session must use <original-run-ref>=<continuation-run-ref>'
  original=${value%%=*}
  continuation=${value#*=}
  validate_run_ref "$original" || fail_usage "invalid original run ref: $original"
  validate_run_ref "$continuation" || fail_usage "invalid continuation run ref: $continuation"
  [[ "$original" != "$continuation" ]] || fail_usage 'original and continuation run refs must differ'
  same_session_pairs+=("$original=$continuation")
}

while (($# > 0)); do
  case "$1" in
    --context)
      (($# >= 2)) || fail_usage '--context requires a value'
      context=$2
      shift 2
      ;;
    --kubeconfig)
      (($# >= 2)) || fail_usage '--kubeconfig requires a value'
      kubeconfig=$2
      shift 2
      ;;
    --expect-run)
      (($# >= 2)) || fail_usage '--expect-run requires a value'
      add_expected_run "$2"
      shift 2
      ;;
    --expect-same-session)
      (($# >= 2)) || fail_usage '--expect-same-session requires a value'
      add_same_session_pair "$2"
      shift 2
      ;;
    --require-distinct-accounts)
      (($# >= 2)) || fail_usage '--require-distinct-accounts requires a value'
      required_distinct_accounts=$2
      shift 2
      ;;
    --help)
      usage
      exit 0
      ;;
    *)
      fail_usage "unsupported argument: $1"
      ;;
  esac
done

[[ -n "$context" ]] || fail_usage 'exact Kubernetes context is required'
[[ "$context" =~ ^[A-Za-z0-9._@/-]{1,253}$ ]] || fail_usage 'Kubernetes context is invalid'
[[ "$context" != *prod* && "$context" != *production* ]] || fail_usage 'production context is forbidden'
[[ "$required_distinct_accounts" =~ ^[0-9]+$ ]] || fail_usage 'distinct account count must be an integer'
((required_distinct_accounts >= 1)) || fail_usage 'distinct account count must be at least 1'
((${#expected_run_order[@]} >= required_distinct_accounts)) ||
  fail_usage 'expected run set is smaller than the required distinct account count'

for pair in "${same_session_pairs[@]}"; do
  original=${pair%%=*}
  continuation=${pair#*=}
  [[ -n "${expected_account_ref_by_run[$original]+present}" ]] ||
    fail_usage "session pair run is absent from --expect-run: $original"
  [[ -n "${expected_account_ref_by_run[$continuation]+present}" ]] ||
    fail_usage "session pair run is absent from --expect-run: $continuation"
done

for command_name in jq kubectl mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || fail_environment "$command_name is required"
done
if [[ -n "$kubeconfig" ]]; then
  [[ -f "$kubeconfig" && -r "$kubeconfig" && ! -L "$kubeconfig" ]] ||
    fail_environment 'Kubernetes configuration is not a readable regular file'
  export KUBECONFIG=$kubeconfig
fi

current_context=$(kubectl config current-context 2>/dev/null) ||
  fail_environment 'Kubernetes current context is unavailable'
[[ "$current_context" == "$context" ]] || fail_environment 'Kubernetes context mismatch'
kubectl --context "$context" get --raw=/readyz >/dev/null 2>&1 ||
  fail_environment 'Kubernetes API is unavailable'

namespace_state=$(kubectl --context "$context" get "namespace/$NAMESPACE" -o json 2>/dev/null) ||
  fail_environment 'local Kodex namespace is absent'
jq -e '
  .metadata.name == "kodex-system" and
  .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
  .metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
  .metadata.labels["kodex.dev/profile"] == "web-only"
' <<<"$namespace_state" >/dev/null || fail_environment 'namespace is not an exact Kodex local profile'

postgres_state=$(kubectl --context "$context" -n "$NAMESPACE" get "pod/$POSTGRES_POD" -o json 2>/dev/null) ||
  fail_environment 'local PostgreSQL pod is absent'
jq -e '
  .metadata.namespace == "kodex-system" and
  .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
  .metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
  .status.phase == "Running" and
  any(.status.containerStatuses[]?; .name == "postgresql" and .ready == true)
' <<<"$postgres_state" >/dev/null || fail_environment 'PostgreSQL pod is not an exact ready local Kodex workload'

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
chmod 0700 "$temporary_directory"
query_output="$temporary_directory/readback.jsonl"
query_errors="$temporary_directory/readback.err"

expected_runs_json='{}'
run_refs_json='[]'
for run_ref in "${expected_run_order[@]}"; do
  account_ref=${expected_account_ref_by_run[$run_ref]}
  expected_runs_json=$(jq -cn \
    --argjson current "$expected_runs_json" \
    --arg run_ref "$run_ref" \
    --arg account_ref "$account_ref" \
    '$current + {($run_ref): $account_ref}') || fail_internal 'cannot build expected run map'
  run_refs_json=$(jq -cn \
    --argjson current "$run_refs_json" \
    --arg run_ref "$run_ref" \
    '$current + [$run_ref]') || fail_internal 'cannot build requested run list'
done

same_sessions_json='[]'
for pair in "${same_session_pairs[@]}"; do
  original=${pair%%=*}
  continuation=${pair#*=}
  same_sessions_json=$(jq -cn \
    --argjson current "$same_sessions_json" \
    --arg original "$original" \
    --arg continuation "$continuation" \
    '$current + [{original: $original, continuation: $continuation}]') ||
    fail_internal 'cannot build session pair list'
done

if ! kubectl --context "$context" -n "$NAMESPACE" exec -i "$POSTGRES_POD" -- \
  psql -X -qAt -v ON_ERROR_STOP=1 -U postgres -d control_plane \
    -v requested_run_refs="$run_refs_json" >"$query_output" 2>"$query_errors" <<'SQL'
BEGIN TRANSACTION READ ONLY;
SET LOCAL statement_timeout = '10s';
WITH requested AS (
    SELECT DISTINCT value AS run_ref
    FROM jsonb_array_elements_text(:'requested_run_refs'::jsonb)
)
SELECT jsonb_build_object(
           'run_ref', requested.run_ref,
           'found', run.id IS NOT NULL,
           'session_ref', COALESCE(session.ref, ''),
           'session_account_ref', COALESCE(session_account.ref, ''),
           'runtime_revision_count', count(runtime_revision.id),
           'runtime_boundary_consistent', COALESCE(
               bool_and(
                   session.organization_id = run.organization_id
                   AND session_account.organization_id = run.organization_id
                   AND runtime_revision.organization_id = run.organization_id
                   AND runtime_revision.session_id = run.session_id
                   AND runtime_account.organization_id = run.organization_id
               ) FILTER (WHERE runtime_revision.id IS NOT NULL),
               false
           ),
           'runtime_account_refs', COALESCE(
               jsonb_agg(DISTINCT runtime_account.ref)
                   FILTER (WHERE runtime_account.ref IS NOT NULL),
               '[]'::jsonb
           )
       )::text
FROM requested
LEFT JOIN control_plane.runs run ON run.ref = requested.run_ref
LEFT JOIN control_plane.sessions session ON session.id = run.session_id
LEFT JOIN control_plane.provider_accounts session_account
       ON session_account.id = session.provider_account_id
LEFT JOIN control_plane.runtime_revisions runtime_revision
       ON runtime_revision.run_id = run.id
LEFT JOIN control_plane.provider_accounts runtime_account
       ON runtime_account.id = runtime_revision.provider_account_id
GROUP BY requested.run_ref, run.id, session.ref, session_account.ref
ORDER BY requested.run_ref;
COMMIT;
SQL
then
  fail_environment 'PostgreSQL read-only affinity query failed'
fi

[[ -s "$query_output" ]] || fail_internal 'PostgreSQL readback is empty'
readback_json=$(jq -s -e '
  if all(.[];
    type == "object" and
    (.run_ref | type) == "string" and
    (.found | type) == "boolean" and
    (.session_ref | type) == "string" and
    (.session_account_ref | type) == "string" and
    (.runtime_revision_count | type) == "number" and
    (.runtime_boundary_consistent | type) == "boolean" and
    (.runtime_account_refs | type) == "array")
  then . else error("invalid readback schema") end
' "$query_output" 2>/dev/null) || fail_internal 'PostgreSQL readback has an invalid safe schema'

verifier="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)/verify-provider-affinity.jq"
[[ -f "$verifier" && -r "$verifier" && ! -L "$verifier" ]] || fail_internal 'affinity parser is unavailable'
report=$(jq -c \
  --argjson expected_runs "$expected_runs_json" \
  --argjson same_sessions "$same_sessions_json" \
  --argjson required_distinct_accounts "$required_distinct_accounts" \
  -f "$verifier" <<<"$readback_json" 2>/dev/null) || fail_internal 'affinity parser failed'

if ! jq -e '.ok == true' <<<"$report" >/dev/null; then
  jq -r '.errors[] | "Kodex provider affinity verification failed: \(.)"' <<<"$report" >&2
  exit "$EXIT_VERIFICATION"
fi

jq -r '
  "Kodex provider affinity verified: runs=\(.checked_runs) session_pairs=\(.checked_session_pairs) distinct_account_refs=\(.observed_account_refs | length) account_refs=\(.observed_account_refs | join(","))"
' <<<"$report"
