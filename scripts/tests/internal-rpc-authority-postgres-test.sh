#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Internal RPC authority PostgreSQL test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
container_name="mattercodex-internal-rpc-authority-postgres-${BASHPID}"

cleanup() {
  docker stop --time 5 "$container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

command -v docker >/dev/null 2>&1 || fail 'docker is required'
command -v pg_isready >/dev/null 2>&1 || fail 'pg_isready is required'
command -v psql >/dev/null 2>&1 || fail 'psql is required'

docker run --rm -d --name "$container_name" \
  -e POSTGRES_HOST_AUTH_METHOD=trust \
  -p 127.0.0.1::5432 \
  docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7 \
  >/dev/null

port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}' "$container_name")
[[ "$port" =~ ^[0-9]+$ ]] || fail 'disposable PostgreSQL port is invalid'
for _ in $(seq 1 30); do
  if pg_isready -h 127.0.0.1 -p "$port" -U postgres >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
pg_isready -h 127.0.0.1 -p "$port" -U postgres >/dev/null 2>&1 ||
  fail 'disposable PostgreSQL did not become ready'

admin_dsn="postgresql://postgres@127.0.0.1:${port}/postgres?sslmode=disable"
migrator_dsn="postgresql://internal_rpc_authority_migrator@127.0.0.1:${port}/internal_rpc_authority?sslmode=disable"
authority_admin_dsn="postgresql://postgres@127.0.0.1:${port}/internal_rpc_authority?sslmode=disable"
baseline="$repository_root/services/internal/internal-rpc-authority/cmd/cli/migrations/20260823000100_internal_rpc_authority_baseline.sql"

psql "$admin_dsn" --no-password --file \
  "$repository_root/deploy/k8s/base/platform-state/postgresql/10-bootstrap.sql" \
  >/dev/null
psql "$migrator_dsn" --no-password --file "$baseline" >/dev/null

assertion=$(psql "$authority_admin_dsn" --no-password --tuples-only --no-align <<'SQL'
SELECT
  (SELECT count(*) = 11
     FROM pg_catalog.pg_proc AS procedure
     JOIN pg_catalog.pg_namespace AS namespace
       ON namespace.oid = procedure.pronamespace
    WHERE namespace.nspname = 'internal_rpc_authority')
  AND (SELECT count(*) = 1
         FROM internal_rpc_authority.authority_restore_fences
        WHERE database_cluster_id = 'internal-rpc-authority-primary'
          AND restore_epoch = 1
          AND phase = 'OPEN')
  AND NOT EXISTS (
        SELECT 1
          FROM pg_catalog.pg_proc AS procedure
          JOIN pg_catalog.pg_namespace AS namespace
            ON namespace.oid = procedure.pronamespace
         WHERE namespace.nspname = 'internal_rpc_authority'
           AND procedure.proname = 'publisher_append_snapshot_history'
           AND procedure.pronargs = 11
      )
  AND (SELECT count(*) = 10
         FROM pg_catalog.pg_roles
        WHERE rolname IN (
          'ira_role_image_builder_issuer_g1',
          'ira_image_admission_issuer_g1',
          'ira_image_promotion_issuer_g1',
          'ira_automation_scheduler_issuer_g1',
          'ira_control_api_gateway_issuer_g1',
          'ira_control_plane_verifier_g1',
          'ira_control_plane_resolver_g1',
          'ira_integration_gateway_issuer_g1',
          'ira_interaction_gateway_issuer_g1',
          'ira_runtime_controller_issuer_g1'
        ) AND rolcanlogin)
  AND NOT EXISTS (
        SELECT 1
          FROM pg_catalog.pg_tables
         WHERE schemaname = 'internal_rpc_authority'
           AND tableowner <> 'internal_rpc_authority_readback_owner'
      );
SQL
)
[[ "$assertion" == "t" ]] || fail 'fresh authority baseline readback rejected'

lifecycle_assertion=$(psql "$authority_admin_dsn" --no-password --set ON_ERROR_STOP=1 \
  --tuples-only --no-align <<'SQL'
BEGIN;
SET SESSION AUTHORIZATION ira_database_credential_reconciler;
SET ROLE internal_rpc_authority_database_credential_reconciler;
SELECT internal_rpc_authority.reconcile_runtime_database_identity(
  'PUBLISHER',
  'ira_publisher_g3',
  3,
  'CURRENT',
  '10000000-0000-4000-8000-000000000001',
  repeat('a', 64)
);
SELECT internal_rpc_authority.reconcile_runtime_database_identity(
  'PUBLISHER',
  'ira_publisher_g1',
  1,
  'PREVIOUS',
  '10000000-0000-4000-8000-000000000002',
  repeat('b', 64)
);
SELECT internal_rpc_authority.retire_runtime_database_identity(
  'PUBLISHER',
  'ira_publisher_g1',
  1,
  '10000000-0000-4000-8000-000000000003',
  repeat('c', 64)
);
ROLLBACK;
SQL
)
[[ "$lifecycle_assertion" == $'BEGIN\nSET\nSET\nt\nt\nt\nROLLBACK' ]] ||
  fail 'runtime database identity lifecycle delegation rejected'

(
  cd "$repository_root/services/internal/internal-rpc-authority"
  INTERNAL_RPC_AUTHORITY_POSTGRES_TEST_DSN="postgresql://ira_database_credential_reconciler@127.0.0.1:${port}/internal_rpc_authority?sslmode=disable" \
    GOWORK=off go test ./internal/repository/postgres/credentiallifecycle \
      -run '^TestReconcileCredentialsReadsCanonicalDigest$' -count=1
)

if psql "$authority_admin_dsn" --no-password --set ON_ERROR_STOP=1 \
  >/dev/null 2>&1 <<'SQL'; then
BEGIN;
SET SESSION AUTHORIZATION ira_database_credential_reconciler;
SET ROLE internal_rpc_authority_database_credential_reconciler;
SELECT internal_rpc_authority.reconcile_runtime_database_identity(
  'PUBLISHER',
  'postgres',
  3,
  'CURRENT',
  '10000000-0000-4000-8000-000000000004',
  repeat('d', 64)
);
ROLLBACK;
SQL
  fail 'unregistered PostgreSQL identity was accepted'
fi

printf 'Internal RPC authority PostgreSQL tests passed\n'
