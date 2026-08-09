-- name: enable_pending_migration_principal :exec
\set ON_ERROR_STOP on
BEGIN;
SET LOCAL statement_timeout = '15s';
SET LOCAL lock_timeout = '5s';
DO $enable$
DECLARE
    principal_oid oid;
BEGIN
    SELECT oid INTO principal_oid
    FROM pg_catalog.pg_roles
    WHERE rolname = 'matter_codex_migration_g1';

    IF principal_oid IS NULL THEN
        RAISE EXCEPTION 'migration principal is missing';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM pg_catalog.pg_auth_members AS membership
        JOIN pg_catalog.pg_roles AS granted_role ON granted_role.oid = membership.roleid
        WHERE membership.member = principal_oid
          AND granted_role.rolname <> 'matter_codex_migration'
    ) THEN
        RAISE EXCEPTION 'migration principal has an unexpected role membership';
    END IF;
    IF NOT pg_catalog.pg_has_role('matter_codex_migration_g1', 'matter_codex_migration', 'MEMBER') THEN
        RAISE EXCEPTION 'migration capability role is missing';
    END IF;
END
$enable$;

SELECT format(
    'ALTER ROLE matter_codex_migration_g1 LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 2 VALID UNTIL %L',
    pg_catalog.clock_timestamp() + interval '5 minutes'
) \gexec
ALTER ROLE matter_codex_migration_g1 SET statement_timeout = '30s';
ALTER ROLE matter_codex_migration_g1 SET lock_timeout = '5s';
ALTER ROLE matter_codex_migration_g1 SET idle_session_timeout = '60s';
ALTER ROLE matter_codex_migration_g1 SET idle_in_transaction_session_timeout = '30s';
SELECT format('COMMENT ON ROLE matter_codex_migration_g1 IS %L', :'lifecycle_comment') \gexec
COMMIT;
