-- name: allowlist_entry__update :exec
UPDATE access_allowlist_entries
SET
    organization_id = @organization_id,
    default_status = @default_status,
    status = @status,
    version = @version,
    updated_at = @updated_at
WHERE id = @id AND version = @previous_version;
