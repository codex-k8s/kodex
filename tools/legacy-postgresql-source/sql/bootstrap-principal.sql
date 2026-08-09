-- name: bootstrap_migration_principal :exec
\set ON_ERROR_STOP on
BEGIN;
SET LOCAL log_statement = 'none';

SELECT format(
    'CREATE ROLE matter_codex_migration_g1 NOLOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 2 PASSWORD %L',
    :'migration_password'
)
WHERE NOT EXISTS (
    SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'matter_codex_migration_g1'
) \gexec

DO $bootstrap$
DECLARE
    principal_oid oid;
BEGIN
    SELECT oid INTO principal_oid
    FROM pg_catalog.pg_roles
    WHERE rolname = 'matter_codex_migration_g1';

    IF principal_oid IS NULL THEN
        RAISE EXCEPTION 'migration principal was not created';
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
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_class WHERE relowner = principal_oid)
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_namespace WHERE nspowner = principal_oid)
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_database WHERE datdba = principal_oid) THEN
        RAISE EXCEPTION 'migration principal owns a database object';
    END IF;
END
$bootstrap$;

SELECT format(
    'ALTER ROLE matter_codex_migration_g1 NOLOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 2 PASSWORD %L',
    :'migration_password'
) \gexec
GRANT matter_codex_migration TO matter_codex_migration_g1;
COMMIT;
