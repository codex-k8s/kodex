#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Direct production authority bootstrap reset failed: %s\n' "$*" >&2; exit 1; }
trap 'fail "unexpected command failure at line $LINENO"' ERR

usage() {
  printf 'Usage: %s --owner-approved --revision <exact-git-sha> --context <exact-context> --public-host <dns-name>\n' "$0" >&2
}

owner_approved=false
revision=""
expected_context=""
public_host=""
while (($# > 0)); do
  case "$1" in
    --owner-approved) owner_approved=true; shift ;;
    --revision) revision="${2:-}"; shift 2 ;;
    --context) expected_context="${2:-}"; shift 2 ;;
    --public-host) public_host="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$owner_approved" == true ]] || fail "explicit owner approval is required"
[[ "$revision" =~ ^[a-f0-9]{40}$ ]] || fail "exact git revision is required"
[[ -n "$expected_context" ]] || fail "exact Kubernetes context is required"
[[ "$public_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || fail "public host is invalid"
for command_name in git jq kubectl; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
[[ "$(git -C "$repository_root" rev-parse HEAD)" == "$revision" ]] ||
  fail "checked-out revision does not match owner-approved revision"
[[ "$(git -C "$repository_root" status --porcelain)" == "" ]] ||
  fail "repository worktree is not clean"
[[ "$(kubectl config current-context)" == "$expected_context" ]] ||
  fail "Kubernetes context mismatch"

namespace=mattercodex-system
postgres_pod=mattercodex-postgresql-0
kubectl -n "$namespace" get pod "$postgres_pod" >/dev/null 2>&1 ||
  fail "direct-production PostgreSQL pod is absent"

# Этот destructive path допустим только до первого публичного cutover.
if kubectl -n "$namespace" get ingress -o json | jq -e --arg public_host "$public_host" '
  any(.items[]?; any(.spec.rules[]?; .host == $public_host))
' >/dev/null; then
  fail "public Control Center ingress already exists"
fi

schema_present=$(
  kubectl -n "$namespace" exec -i "$postgres_pod" -- \
    psql -U postgres -d internal_rpc_authority -At <<'SQL'
SELECT to_regclass('internal_rpc_authority.authority_snapshot_history') IS NOT NULL;
SQL
)
case "$schema_present" in
  t)
    read -r history_count delivery_count readback_count < <(
      kubectl -n "$namespace" exec -i "$postgres_pod" -- \
        psql -U postgres -d internal_rpc_authority -At -F ' ' <<'SQL'
SELECT
  (SELECT count(*) FROM internal_rpc_authority.authority_snapshot_history),
  (SELECT count(*) FROM internal_rpc_authority.authority_publisher_delivery_receipts),
  (SELECT count(*) FROM internal_rpc_authority.authority_snapshot_readbacks);
SQL
    )
    ;;
  f)
    goose_table_present=$(
      kubectl -n "$namespace" exec -i "$postgres_pod" -- \
        psql -U postgres -d internal_rpc_authority -At <<'SQL'
SELECT to_regclass('public.goose_db_version') IS NOT NULL;
SQL
    )
    if [[ "$goose_table_present" == t ]]; then
      goose_rows=$(
        kubectl -n "$namespace" exec -i "$postgres_pod" -- \
          psql -U postgres -d internal_rpc_authority -At <<'SQL'
SELECT count(*) FROM public.goose_db_version;
SQL
      )
    elif [[ "$goose_table_present" == f ]]; then
      goose_rows=0
    else
      fail "Goose schema readback is invalid"
    fi
    [[ "$goose_rows" == 0 ]] || fail "an unknown partial migration state exists"
    history_count=1
    delivery_count=0
    readback_count=0
    ;;
  *) fail "authority schema readback is invalid" ;;
esac
[[ "$history_count" =~ ^[0-9]+$ && "$delivery_count" =~ ^[0-9]+$ && "$readback_count" =~ ^[0-9]+$ ]] ||
  fail "authority bootstrap evidence is invalid"
(( delivery_count == 0 && readback_count == 0 )) ||
  fail "served authority state exists; pre-cutover reset is forbidden"
(( history_count > 0 )) || fail "no failed authority bootstrap state was found"

deployments=(
  automation-scheduler
  control-api-gateway
  control-plane
  integration-gateway
  interaction-gateway
  internal-rpc-authority-database-credential-reconciler
  internal-rpc-authority-publisher
  internal-rpc-authority-readback-attestor
  internal-rpc-authority-restore-controller
  runtime-controller
)
for deployment in "${deployments[@]}"; do
  if kubectl -n "$namespace" get deployment "$deployment" >/dev/null 2>&1; then
    kubectl -n "$namespace" scale deployment "$deployment" --replicas=0 >/dev/null
  fi
done

kubectl -n "$namespace" exec -i "$postgres_pod" -- psql -U postgres -d postgres -v ON_ERROR_STOP=1 <<'SQL'
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = 'internal_rpc_authority'
  AND pid <> pg_backend_pid();
DROP DATABASE internal_rpc_authority;
CREATE DATABASE internal_rpc_authority OWNER internal_rpc_authority_owner TEMPLATE template0;
REVOKE ALL ON DATABASE internal_rpc_authority FROM PUBLIC;
SQL

reset_resources=$(mktemp)
trap 'rm -f -- "$reset_resources"' EXIT HUP INT TERM
jq -rs '
  (.[0].publisher_owned_empty_resources |
    map({kind,name})) +
  (.[1].runtime_owned_empty_resources |
    map({kind,name})) +
  (.[1].publisher_owned_runtime_keys |
    map({kind,name})) |
  unique_by([.kind,.name]) |
  .[] | [.kind,.name] | @tsv
' \
  "$repository_root/infra/direct-production/application-material-policy.json" \
  "$repository_root/infra/direct-production/internal-rpc-authority-prototype-material-policy.json" \
  | sed 's/^"//;s/"$//;s/\\t/\t/g' >"$reset_resources"

while IFS=$'\t' read -r kind name; do
  [[ "$kind" == Secret && "$name" == internal-rpc-authority-* ]] ||
    fail "authority reset policy contains an unexpected resource"
  kubectl -n "$namespace" delete "${kind,,}/$name" --ignore-not-found >/dev/null
done <"$reset_resources"

printf 'Direct production authority bootstrap state reset completed for revision %s\n' "$revision"
