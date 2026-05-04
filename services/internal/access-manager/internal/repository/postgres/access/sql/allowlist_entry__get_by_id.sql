-- name: allowlist_entry__get_by_id :one
SELECT
    id,
    match_type,
    value,
    organization_id,
    default_status,
    status,
    version,
    created_at,
    updated_at
FROM access_allowlist_entries
WHERE id = @id;
