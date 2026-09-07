#!/usr/bin/env bash
set -euo pipefail

readonly namespace=kodex-system
readonly postgres_pod=kodex-postgresql-0

fail() {
  printf 'Kodex discovery readback failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: verify-discovery-readback.sh --context <name> --state <path> \
  [--kubeconfig <path>]
EOF
}

context=""
kubeconfig="${KUBECONFIG:-}"
state_file=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --state) state_file=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$context" =~ ^[A-Za-z0-9._@/-]{1,253}$ ]] || fail 'exact Kubernetes context is required'
[[ "$context" != *prod* && "$context" != *production* ]] || fail 'production context is forbidden'
[[ -n "$kubeconfig" && -f "$kubeconfig" && -r "$kubeconfig" && ! -L "$kubeconfig" ]] ||
  fail 'Kubernetes configuration is invalid'
[[ -f "$state_file" && -r "$state_file" && ! -L "$state_file" ]] || fail 'E2E state file is invalid'
[[ $(stat -c '%a' "$state_file") == 600 ]] || fail 'E2E state file permissions must be 0600'
readonly readback_filter="$(dirname -- "${BASH_SOURCE[0]}")/discovery-readback.jq"

export KUBECONFIG=$kubeconfig
[[ $(kubectl config current-context) == "$context" ]] || fail 'Kubernetes context mismatch'
kubectl --context "$context" get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'
namespace_state=$(kubectl --context "$context" get "namespace/$namespace" -o json) || fail 'local Kodex namespace is absent'
jq -e '
  .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
  .metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
  .metadata.labels["kodex.dev/profile"] == "web-only"
' <<<"$namespace_state" >/dev/null || fail 'namespace is not an exact local Kodex profile'

state=$(jq -c . "$state_file") || fail 'E2E state is invalid'
state=$(jq -nce --argjson state "$state" --arg mode state -f "$readback_filter") ||
  fail 'E2E state is incomplete or contains invalid fixture refs'

run_refs=$(jq -c '[.refs.firstRunRef, .refs.continuationRunRef, .refs.workflowRunRef,
  .refs.scheduledRunRef, .refs.instructionRunRef]' <<<"$state")
first_run=$(jq -r '.refs.firstRunRef' <<<"$state")
continuation_run=$(jq -r '.refs.continuationRunRef' <<<"$state")
instruction_run=$(jq -r '.refs.instructionRunRef' <<<"$state")
instruction_ref=$(jq -r '.refs.publishedInstructionRef' <<<"$state")

expected_accounts_json=$(jq -c '[.refs.coordinatorProviderAccountRef,.refs.analystProviderAccountRef] | sort' <<<"$state")
expected_run_accounts=$(jq -nc --argjson state "$state" --arg mode expected -f "$readback_filter")
readback=$(kubectl --context "$context" -n "$namespace" exec -i "$postgres_pod" -- \
  psql -X -qAt -v ON_ERROR_STOP=1 -U postgres -d control_plane \
    -v requested_run_refs="$run_refs" -v instruction_run_ref="$instruction_run" \
    -v instruction_ref="$instruction_ref" -v selected_account_refs="$expected_accounts_json" <<'SQL'
BEGIN TRANSACTION READ ONLY;
SET LOCAL statement_timeout = '10s';
WITH requested AS (
    SELECT DISTINCT value AS run_ref
    FROM jsonb_array_elements_text(:'requested_run_refs'::jsonb)
), run_accounts AS (
    SELECT requested.run_ref, account.ref AS account_ref
    FROM requested
    JOIN control_plane.runs run ON run.ref = requested.run_ref
    JOIN control_plane.sessions session ON session.id = run.session_id
    JOIN control_plane.provider_accounts account ON account.id = session.provider_account_id
), instruction_runtime AS (
    SELECT count(*) AS revision_count,
           COALESCE(bool_and(
               revision.instruction_ref = :'instruction_ref'
               AND revision.instruction_digest = instruction.digest
               AND instruction.ref = :'instruction_ref'
               AND instruction.state = 'PUBLISHED'
           ), false) AS valid
    FROM control_plane.runs run
    JOIN control_plane.runtime_revisions revision ON revision.run_id = run.id
    JOIN control_plane.instruction_versions instruction ON instruction.ref = revision.instruction_ref
    WHERE run.ref = :'instruction_run_ref'
)
SELECT jsonb_build_object(
    'run_accounts', COALESCE(
        (SELECT jsonb_object_agg(run_ref, account_ref ORDER BY run_ref) FROM run_accounts),
        '{}'::jsonb
    ),
    'selected_accounts', COALESCE((
        SELECT jsonb_object_agg(account.ref, account.stable_key ORDER BY account.ref)
        FROM control_plane.provider_accounts account
        WHERE account.definition_key = 'openai-codex'
          AND account.ref IN (SELECT value FROM jsonb_array_elements_text(:'selected_account_refs'::jsonb))
          AND account.state = 'AUTHORIZED'
          AND account.enabled
          AND account.current_credential_revision_id IS NOT NULL
    ), '{}'::jsonb),
    'instruction_runtime_count', (SELECT revision_count FROM instruction_runtime),
    'instruction_runtime_valid', (SELECT valid FROM instruction_runtime)
)::text;
COMMIT;
SQL
) || fail 'PostgreSQL read-only discovery query failed'
readback=$(jq -ce '.' <<<"$readback") || fail 'PostgreSQL discovery readback is invalid'

jq -e --argjson state "$state" --arg mode readback -f "$readback_filter" <<<"$readback" >/dev/null ||
  fail 'provider fixture set, run mapping, or instruction revision is inconsistent'

declare -a affinity_args=(
  --context "$context"
  --kubeconfig "$kubeconfig"
  --expect-same-session "$first_run=$continuation_run"
  --require-distinct-accounts 2
)
while IFS=$'\t' read -r run_ref account_ref; do
  affinity_args+=(--expect-run "$run_ref=$account_ref")
done < <(jq -r 'to_entries[] | [.key, .value] | @tsv' <<<"$expected_run_accounts")

"$(dirname -- "${BASH_SOURCE[0]}")/verify-provider-affinity.sh" "${affinity_args[@]}"
printf 'Kodex discovery readback verified: accounts=%s instruction_run=%s\n' \
  "$(jq -r '.selected_accounts | keys | join(",")' <<<"$readback")" "$instruction_run"
