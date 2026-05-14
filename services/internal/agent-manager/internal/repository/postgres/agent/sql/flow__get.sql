-- name: flow__get :one
SELECT
    id,
    scope_type,
    scope_ref,
    slug,
    display_name,
    description,
    icon_object_uri,
    status,
    active_version_id,
    version,
    created_at,
    updated_at
FROM agent_manager_flows
WHERE id = @id;
