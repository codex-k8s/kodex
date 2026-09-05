#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Internal RPC authority PostgreSQL test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
container_name="kodex-internal-rpc-authority-postgres-${BASHPID}"

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
  (SELECT count(*) = 9
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
  AND (SELECT count(*) = 14
         FROM pg_catalog.pg_roles
        WHERE rolname IN (
          'ira_restore_controller_g1',
          'ira_publisher_g4',
          'ira_readback_attestor_g4',
          'ira_role_image_builder_issuer_g1',
          'ira_image_admission_issuer_g1',
          'ira_image_promotion_issuer_g1',
          'ira_automation_scheduler_issuer_g1',
          'ira_control_api_gateway_issuer_g1',
          'ira_control_plane_verifier_g1',
          'ira_control_plane_resolver_g1',
          'ira_integration_gateway_issuer_g1',
          'ira_interaction_gateway_issuer_g1',
          'ira_email_bridge_issuer_g1',
          'ira_runtime_controller_issuer_g1'
        ) AND rolcanlogin)
  AND (SELECT count(*) = 2
         FROM internal_rpc_authority.authority_runtime_database_identities
        WHERE (capability, principal, generation) IN (
          ('PUBLISHER', 'ira_publisher_g4', 4),
          ('READBACK_ATTESTOR', 'ira_readback_attestor_g4', 4)
        )
          AND lifecycle_status = 'CURRENT'
          AND registered_set_digest_sha256 =
              'ed499a5c2dfdd8365c567ccdaeddaf78fd878e0c73c78db30748506625b70986')
  AND NOT EXISTS (
        SELECT 1
          FROM pg_catalog.pg_roles
         WHERE rolname IN (
           'internal_rpc_authority_database_credential_reconciler',
           'internal_rpc_authority_credential_lifecycle_definer',
           'ira_database_credential_reconciler',
           'ira_publisher_g1',
           'ira_publisher_g2',
           'ira_publisher_g3',
           'ira_publisher_g5',
           'ira_readback_attestor_g1',
           'ira_readback_attestor_g2',
           'ira_readback_attestor_g3',
           'ira_readback_attestor_g5'
         ))
  AND NOT EXISTS (
        SELECT 1
          FROM pg_catalog.pg_tables
         WHERE schemaname = 'internal_rpc_authority'
           AND tablename IN (
             'database_credential_reconciler_leases',
             'database_credential_reconciliation_receipts',
             'database_credential_rotation_intents'
           ))
  AND NOT EXISTS (
        SELECT 1
          FROM pg_catalog.pg_tables
         WHERE schemaname = 'internal_rpc_authority'
           AND tableowner <> 'internal_rpc_authority_readback_owner'
      );
SQL
)
[[ "$assertion" == "t" ]] || fail 'fresh authority baseline readback rejected'

for principal in ira_email_bridge_issuer_g1 ira_interaction_gateway_issuer_g1; do
  issuer_dsn="postgresql://${principal}@127.0.0.1:${port}/internal_rpc_authority?sslmode=disable"
  issuer_assertion=$(psql "$issuer_dsn" --no-password --set ON_ERROR_STOP=1 \
    --tuples-only --no-align --quiet <<'SQL'
SET ROLE internal_rpc_authority_issuer;
SELECT current_user = 'internal_rpc_authority_issuer'
  AND session_user IN ('ira_email_bridge_issuer_g1', 'ira_interaction_gateway_issuer_g1')
  AND (SELECT rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole
       AND NOT rolinherit AND NOT rolreplication AND NOT rolbypassrls
       FROM pg_roles WHERE rolname = session_user)
  AND (SELECT count(*) = 1 FROM pg_auth_members
       WHERE member = (SELECT oid FROM pg_roles WHERE rolname = session_user)
         AND roleid = (SELECT oid FROM pg_roles WHERE rolname = 'internal_rpc_authority_issuer')
         AND set_option AND NOT inherit_option AND NOT admin_option);
SQL
  )
  [[ "$issuer_assertion" == t ]] || fail 'optional issuer login or capability binding mismatch'
  for forbidden in internal_rpc_authority_publisher internal_rpc_authority_verifier \
    internal_rpc_authority_readback_attestor internal_rpc_authority_restore_controller; do
    if psql "$issuer_dsn" --no-password --set ON_ERROR_STOP=1 \
      --command "SET ROLE $forbidden" >/dev/null 2>&1; then
      fail 'optional issuer acquired an unrelated authority capability'
    fi
  done
done

static_identity_assertion=$(psql "$authority_admin_dsn" --no-password --set ON_ERROR_STOP=1 \
  --tuples-only --no-align <<'SQL'
BEGIN;
SET SESSION AUTHORIZATION ira_publisher_g4;
SET ROLE internal_rpc_authority_publisher;
SELECT internal_rpc_authority.record_database_credential_session_readback(
  repeat('a', 64),
  '10000000-0000-4000-8000-000000000001'
);
ROLLBACK;
SQL
)
[[ "$static_identity_assertion" == $'BEGIN\nSET\nSET\nCURRENT\nROLLBACK' ]] ||
  fail 'static database identity readback rejected'

psql "$authority_admin_dsn" --no-password --set ON_ERROR_STOP=1 >/dev/null <<'SQL'
INSERT INTO internal_rpc_authority.authority_snapshot_history (
  source_revision, source_digest_sha256, key_set_revision, policy_revision,
  signer_generation, predecessor_revision, predecessor_digest_sha256,
  canonical_payload, published_at, snapshot_compact_jws,
  publication_intent_id, publication_input_digest_sha256,
  expected_readback_count
) VALUES (
  1, repeat('1', 64), 1, 1, 1, 0, repeat('0', 64),
  '{"source_revision":1}'::jsonb, clock_timestamp(), repeat('j', 64),
  '10000000-0000-4000-8000-000000000010', repeat('2', 64), 2
);
INSERT INTO internal_rpc_authority.authority_rotation_intents (
  intent_id, source_revision, source_digest_sha256, status, created_at, updated_at
) VALUES (
  '10000000-0000-4000-8000-000000000010', 1, repeat('1', 64),
  'PREPARED', clock_timestamp(), clock_timestamp()
);
INSERT INTO internal_rpc_authority.authority_snapshot_readbacks (
  readback_id, workload_id, role, workload_generation,
  source_revision, digest_sha256, verified_at
) VALUES
  ('10000000-0000-4000-8000-000000000011', 'required-a',
   'AUTHORIZATION_ISSUER', 1, 1, repeat('1', 64), clock_timestamp()),
  ('10000000-0000-4000-8000-000000000012', 'required-b',
   'AUTHORIZATION_VERIFIER', 1, 1, repeat('1', 64), clock_timestamp()),
  ('10000000-0000-4000-8000-000000000013', 'optional-dynamic',
   'AUTHORIZATION_ISSUER', 1, 1, repeat('1', 64), clock_timestamp());
SQL

promotion_assertion=$(psql "$authority_admin_dsn" --no-password --set ON_ERROR_STOP=1 \
  --tuples-only --no-align <<'SQL'
SET SESSION AUTHORIZATION ira_publisher_g4;
SET ROLE internal_rpc_authority_publisher;
SELECT internal_rpc_authority.publisher_promote_snapshot(
  '10000000-0000-4000-8000-000000000010', 1, repeat('1', 64), 2,
  ARRAY['required-a', 'required-b'],
  ARRAY['AUTHORIZATION_ISSUER', 'AUTHORIZATION_VERIFIER'],
  ARRAY[1, 1]::bigint[]
);
SQL
)
[[ "$promotion_assertion" == $'SET\nSET\nt' ]] ||
  fail 'optional dynamic readback blocked exact snapshot promotion'

if psql "$authority_admin_dsn" --no-password --set ON_ERROR_STOP=1 \
  >/dev/null 2>&1 <<'SQL'; then
SELECT internal_rpc_authority.reconcile_runtime_database_identity(
  'PUBLISHER', 'ira_publisher_g4', 4, 'CURRENT',
  '10000000-0000-4000-8000-000000000004', repeat('d', 64)
);
SQL
  fail 'removed PostgreSQL credential lifecycle function remains callable'
fi

printf 'Internal RPC authority PostgreSQL tests passed\n'
