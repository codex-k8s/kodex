-- name: workspace_backup__version :one
SELECT max(version_id) FROM public.goose_db_version WHERE is_applied;
