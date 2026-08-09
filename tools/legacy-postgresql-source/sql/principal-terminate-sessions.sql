-- name: terminate_migration_principal_sessions :exec
\set ON_ERROR_STOP on
BEGIN;
SET LOCAL statement_timeout = '20s';
SET LOCAL lock_timeout = '5s';

DO $terminate$
DECLARE
    termination_result boolean;
BEGIN
    SELECT coalesce(bool_and(pg_catalog.pg_terminate_backend(pid, 5000)), true)
    INTO termination_result
    FROM pg_catalog.pg_stat_activity
    WHERE usename = 'matter_codex_migration_g1'
      AND pid <> pg_catalog.pg_backend_pid();

    IF NOT termination_result THEN
        RAISE EXCEPTION 'migration principal session termination was not proven';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM pg_catalog.pg_stat_activity
        WHERE usename = 'matter_codex_migration_g1'
          AND pid <> pg_catalog.pg_backend_pid()
    ) THEN
        RAISE EXCEPTION 'migration principal still has live sessions';
    END IF;
END
$terminate$;
COMMIT;
