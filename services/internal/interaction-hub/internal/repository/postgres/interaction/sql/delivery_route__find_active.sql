-- name: delivery_route__find_active :one
SELECT
    id,
    scope_type,
    scope_ref,
    surface_kind,
    channel_capability_ref,
    package_installation_ref,
    routing_policy_ref,
    status,
    created_at,
    updated_at
FROM interaction_hub_delivery_routes
WHERE scope_type = @scope_type
  AND scope_ref = @scope_ref
  AND status = 'active'
ORDER BY updated_at DESC, id DESC
LIMIT 1;
