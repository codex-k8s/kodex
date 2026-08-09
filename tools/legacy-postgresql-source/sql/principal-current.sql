-- name: promote_migration_principal_current :exec
\set ON_ERROR_STOP on
BEGIN;
SET LOCAL statement_timeout = '15s';
SET LOCAL lock_timeout = '5s';

DO $promote$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_roles
        WHERE rolname = 'matter_codex_migration_g1'
          AND rolcanlogin
          AND NOT rolsuper
          AND NOT rolcreatedb
          AND NOT rolcreaterole
          AND NOT rolreplication
          AND NOT rolbypassrls
    ) THEN
        RAISE EXCEPTION 'pending migration principal invariant failed';
    END IF;
    IF NOT pg_catalog.pg_has_role('matter_codex_migration_g1', 'matter_codex_migration', 'MEMBER') THEN
        RAISE EXCEPTION 'migration capability role is missing';
    END IF;
END
$promote$;

ALTER ROLE matter_codex_migration_g1 VALID UNTIL 'infinity';
SELECT format('COMMENT ON ROLE matter_codex_migration_g1 IS %L', :'lifecycle_comment') \gexec
COMMIT;
