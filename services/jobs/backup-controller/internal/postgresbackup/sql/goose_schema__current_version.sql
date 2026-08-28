-- name: goose_schema__current_version :one
SELECT version_id
FROM public.goose_db_version
WHERE is_applied
ORDER BY id DESC
LIMIT 1;
