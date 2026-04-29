-- name: allowlist_entry__upsert :exec
INSERT INTO access_allowlist_entries (
    id, match_type, value, organization_id, default_status, status, version, created_at, updated_at
) VALUES (
    @id, @match_type, @value, @organization_id, @default_status, @status, @version, @created_at, @updated_at
)
ON CONFLICT (match_type, value) DO UPDATE SET
    id = EXCLUDED.id,
    organization_id = EXCLUDED.organization_id,
    default_status = EXCLUDED.default_status,
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    created_at = EXCLUDED.created_at,
    updated_at = EXCLUDED.updated_at;
