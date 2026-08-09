-- name: retire_migration_principal :exec
\set ON_ERROR_STOP on
BEGIN;
SET LOCAL statement_timeout = '15s';
SET LOCAL lock_timeout = '5s';
ALTER ROLE matter_codex_migration_g1 NOLOGIN;
REVOKE matter_codex_migration FROM matter_codex_migration_g1;
SELECT format('COMMENT ON ROLE matter_codex_migration_g1 IS %L', :'lifecycle_comment') \gexec
COMMIT;
