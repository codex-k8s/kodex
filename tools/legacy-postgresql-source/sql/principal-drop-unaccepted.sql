-- name: drop_unaccepted_migration_principal :exec
\set ON_ERROR_STOP on
BEGIN;
SET LOCAL statement_timeout = '15s';
SET LOCAL lock_timeout = '5s';

DO $drop_unaccepted$
DECLARE
    principal_oid oid;
BEGIN
    SELECT oid INTO principal_oid
    FROM pg_catalog.pg_roles
    WHERE rolname = 'matter_codex_migration_g1';

    IF principal_oid IS NULL THEN
        RETURN;
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_catalog.pg_roles
        WHERE oid = principal_oid AND rolcanlogin
    ) THEN
        RAISE EXCEPTION 'unaccepted migration principal still has LOGIN';
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_catalog.pg_stat_activity
        WHERE usename = 'matter_codex_migration_g1'
          AND pid <> pg_catalog.pg_backend_pid()
    ) THEN
        RAISE EXCEPTION 'unaccepted migration principal still has live sessions';
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_catalog.pg_auth_members
        WHERE member = principal_oid OR roleid = principal_oid
    ) THEN
        RAISE EXCEPTION 'unaccepted migration principal still has memberships';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_class WHERE relowner = principal_oid)
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_namespace WHERE nspowner = principal_oid)
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_database WHERE datdba = principal_oid) THEN
        RAISE EXCEPTION 'unaccepted migration principal owns a database object';
    END IF;
END
$drop_unaccepted$;

SELECT 'DROP ROLE matter_codex_migration_g1'
WHERE EXISTS (
    SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'matter_codex_migration_g1'
) \gexec
COMMIT;
