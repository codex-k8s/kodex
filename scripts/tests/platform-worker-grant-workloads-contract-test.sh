#!/usr/bin/env sh
set -eu

repository_root="$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)"
policy="$repository_root/deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json"
# Проверяется актуальная forward-only замена ограничения, а не его прежняя версия.
migration="$repository_root/services/internal/control-plane/cmd/cli/migrations/20260904000617_issue_1046_email_worker_watermark.sql"

temporary_directory="$(mktemp -d)"
trap 'rm -rf "$temporary_directory"' EXIT HUP INT TERM

jq -r '.policy.authority_proof_producers[] | select(.application_credential == "PLATFORM_WORKER_GRANT") | .caller_workload_id' "$policy" \
  | LC_ALL=C sort -u >"$temporary_directory/policy-workloads"

sed -n '1,/-- +goose Down/p' "$migration" \
  | sed -n "/ADD CONSTRAINT worker_grant_high_watermarks_workload_id_check/,/));/p" \
  | awk -F "'" '{ for (i = 2; i <= NF; i += 2) print $i }' \
  | LC_ALL=C sort -u >"$temporary_directory/migration-workloads"

if ! cmp -s "$temporary_directory/policy-workloads" "$temporary_directory/migration-workloads"; then
  printf '%s\n' 'platform worker grant workload constraint does not match authority policy' >&2
  diff -u "$temporary_directory/policy-workloads" "$temporary_directory/migration-workloads" >&2 || true
  exit 1
fi

printf '%s\n' 'platform worker grant workload contract: PASS'
