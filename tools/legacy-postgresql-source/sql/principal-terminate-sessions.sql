-- name: terminate_migration_principal_sessions :one
SELECT coalesce(bool_and(pg_catalog.pg_terminate_backend(pid)), true)
FROM pg_catalog.pg_stat_activity
WHERE usename = 'matter_codex_migration_g1'
  AND pid <> pg_catalog.pg_backend_pid();
