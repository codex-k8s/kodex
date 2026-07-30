#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dsn="${INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_DSN:-}"

if [[ -z "$dsn" ]]; then
  echo "INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_DSN is required" >&2
  exit 2
fi
for role_dsn_name in \
  INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_ATTESTOR_DSN \
  INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_ATTESTOR_G2_DSN \
  INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_PUBLISHER_DSN \
  INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_PUBLISHER_G2_DSN \
  INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_VERIFIER_G1_DSN \
  INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_VERIFIER_G2_DSN
do
  if [[ -z "${!role_dsn_name:-}" ]]; then
    echo "${role_dsn_name} is required" >&2
    exit 2
  fi
done

database_name="$(psql "$dsn" -X -Atq -v ON_ERROR_STOP=1 -c 'select current_database()')"
if [[ ! "$database_name" =~ ^mattercodex_contract_[a-z0-9_]+$ ]]; then
  echo "contract PostgreSQL database name must start with mattercodex_contract_" >&2
  exit 2
fi

psql "$dsn" -X -q -v ON_ERROR_STOP=1 \
  -f "$repo_root/contracts/authorization/v1/postgresql-readback-boundary.sql"
for role_check in \
  "INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_ATTESTOR_DSN:ira_readback_attestor_g1" \
  "INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_ATTESTOR_G2_DSN:ira_readback_attestor_g2" \
  "INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_PUBLISHER_DSN:ira_publisher_g1" \
  "INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_PUBLISHER_G2_DSN:ira_publisher_g2" \
  "INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_VERIFIER_G1_DSN:ira_control_plane_verifier_g1" \
  "INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_VERIFIER_G2_DSN:ira_control_plane_verifier_g2"
do
  role_dsn_name="${role_check%%:*}"
  expected_role="${role_check#*:}"
  actual_role="$(
    psql "${!role_dsn_name}" -X -Atq -v ON_ERROR_STOP=1 \
      -c 'select session_user'
  )"
  if [[ "$actual_role" != "$expected_role" ]]; then
    echo "${role_dsn_name} does not use the required PostgreSQL role" >&2
    exit 2
  fi
done
psql "$dsn" -X -q -v ON_ERROR_STOP=1 \
  -f "$repo_root/contracts/authorization/v1/postgresql-readback-assertions.sql"
psql "$dsn" -X -q -v ON_ERROR_STOP=1 \
  -f "$repo_root/contracts/authorization/v1/postgresql-readback-behavior.sql"

publisher_log="$(mktemp)"
attestor_log="$(mktemp)"
publisher_pid=""
attestor_pid=""
cleanup() {
  if [[ -n "$publisher_pid" ]] && kill -0 "$publisher_pid" 2>/dev/null; then
    kill "$publisher_pid" 2>/dev/null || true
    wait "$publisher_pid" 2>/dev/null || true
  fi
  if [[ -n "$attestor_pid" ]] && kill -0 "$attestor_pid" 2>/dev/null; then
    kill "$attestor_pid" 2>/dev/null || true
    wait "$attestor_pid" 2>/dev/null || true
  fi
  rm -f "$publisher_log" "$attestor_log"
}
trap cleanup EXIT

psql "$INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_PUBLISHER_DSN" \
  -X -q -v ON_ERROR_STOP=1 \
  -f "$repo_root/contracts/authorization/v1/postgresql-publisher-live-session-retirement.sql" \
  >"$publisher_log" 2>&1 &
publisher_pid="$!"

lock_ready="false"
for _ in $(seq 1 50); do
  lock_ready="$(
    psql "$dsn" -X -Atq -v ON_ERROR_STOP=1 -c \
      "select exists (
         select 1
           from pg_catalog.pg_locks
          where locktype = 'advisory'
            and objid = 186200
            and granted
       )"
  )"
  if [[ "$lock_ready" == "t" ]]; then
    break
  fi
  sleep 0.1
done
if [[ "$lock_ready" != "t" ]]; then
  echo "publisher live session did not reach retirement barrier" >&2
  exit 1
fi

psql "$dsn" -X -q -v ON_ERROR_STOP=1 <<'SQL'
BEGIN;
DO $assertion$
BEGIN
  UPDATE internal_rpc_authority.authority_runtime_database_identities
     SET credential_status = 'RETIRED',
         retired_at = pg_catalog.clock_timestamp()
   WHERE session_login = 'ira_publisher_g1'
     AND capability_role = 'PUBLISHER'
     AND credential_status = 'CURRENT';
  IF NOT FOUND THEN
    RAISE EXCEPTION 'publisher runtime retirement state transition failed';
  END IF;
END
$assertion$;
COMMIT;
ALTER ROLE ira_publisher_g1 NOLOGIN;
REVOKE internal_rpc_authority_publisher FROM ira_publisher_g1;
SQL

if ! wait "$publisher_pid"; then
  cat "$publisher_log" >&2
  publisher_pid=""
  exit 1
fi
publisher_pid=""

psql "$INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_PUBLISHER_G2_DSN" \
  -X -q -v ON_ERROR_STOP=1 <<'SQL'
BEGIN;
SET LOCAL ROLE internal_rpc_authority_publisher;
DO $assertion$
BEGIN
  IF NOT internal_rpc_authority.is_active_runtime_database_session(
    'PUBLISHER'
  ) THEN
    RAISE EXCEPTION 'next publisher runtime database identity was rejected';
  END IF;
  PERFORM 1
    FROM internal_rpc_authority.authority_key_delivery_readbacks;
  PERFORM 1
    FROM internal_rpc_authority.authority_snapshot_readbacks;
  PERFORM internal_rpc_authority.publisher_append_snapshot_history(
    2,
    repeat('a', 64),
    2,
    2,
    2,
    1,
    repeat('b', 64),
    'header.payload.signature'
  );
  PERFORM internal_rpc_authority.publisher_record_rotation_intent(
    '85000000-0000-4000-8000-000000000001',
    2,
    repeat('c', 64),
    2,
    '86000000-0000-4000-8000-000000000001'
  );
  PERFORM * FROM internal_rpc_authority.publisher_read_restore_fence();
END
$assertion$;
COMMIT;
SQL

psql "$INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_ATTESTOR_DSN" \
  -X -q -v ON_ERROR_STOP=1 \
  -f "$repo_root/contracts/authorization/v1/postgresql-attestor-live-session-retirement.sql" \
  >"$attestor_log" 2>&1 &
attestor_pid="$!"

lock_ready="false"
for _ in $(seq 1 50); do
  lock_ready="$(
    psql "$dsn" -X -Atq -v ON_ERROR_STOP=1 -c \
      "select exists (
         select 1
           from pg_catalog.pg_locks
          where locktype = 'advisory'
            and objid = 186201
            and granted
       )"
  )"
  if [[ "$lock_ready" == "t" ]]; then
    break
  fi
  sleep 0.1
done
if [[ "$lock_ready" != "t" ]]; then
  echo "attestor live session did not reach retirement barrier" >&2
  exit 1
fi

psql "$dsn" -X -q -v ON_ERROR_STOP=1 <<'SQL'
BEGIN;
DO $assertion$
BEGIN
  UPDATE internal_rpc_authority.authority_runtime_database_identities
     SET credential_status = 'RETIRED',
         retired_at = pg_catalog.clock_timestamp()
   WHERE session_login = 'ira_readback_attestor_g1'
     AND capability_role = 'READBACK_ATTESTOR'
     AND credential_status = 'CURRENT';
  IF NOT FOUND THEN
    RAISE EXCEPTION 'attestor runtime retirement state transition failed';
  END IF;
END
$assertion$;
COMMIT;
ALTER ROLE ira_readback_attestor_g1 NOLOGIN;
REVOKE internal_rpc_authority_readback_attestor FROM ira_readback_attestor_g1;
SQL

if ! wait "$attestor_pid"; then
  cat "$attestor_log" >&2
  attestor_pid=""
  exit 1
fi
attestor_pid=""

psql "$INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_ATTESTOR_G2_DSN" \
  -X -q -v ON_ERROR_STOP=1 <<'SQL'
BEGIN;
SET LOCAL ROLE internal_rpc_authority_readback_attestor;
DO $assertion$
BEGIN
  IF NOT internal_rpc_authority.is_active_runtime_database_session(
    'READBACK_ATTESTOR'
  ) THEN
    RAISE EXCEPTION 'next attestor runtime database identity was rejected';
  END IF;
  PERFORM 1
    FROM internal_rpc_authority.authority_readback_intents;
  PERFORM 1
    FROM internal_rpc_authority.authority_readback_attestation_challenges;
  PERFORM 1
    FROM internal_rpc_authority.authority_readback_attestation_receipts;
END
$assertion$;
COMMIT;
SQL
