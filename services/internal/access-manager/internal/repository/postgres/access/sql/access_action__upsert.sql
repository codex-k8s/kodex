-- name: access_action__upsert :exec
INSERT INTO access_actions (
    id, key, display_name, description, resource_type, status, version, created_at, updated_at
) VALUES (
    @id, @key, @display_name, @description, @resource_type, @status, @version, @created_at, @updated_at
)
ON CONFLICT (key) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    description = EXCLUDED.description,
    resource_type = EXCLUDED.resource_type,
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    updated_at = EXCLUDED.updated_at
WHERE access_actions.id = @id;
