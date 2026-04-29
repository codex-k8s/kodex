-- name: allowlist_entry__find :one
SELECT id, match_type, value, organization_id, default_status, status, version, created_at, updated_at
FROM access_allowlist_entries
WHERE match_type = @match_type AND value = @value;
