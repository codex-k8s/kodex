-- name: delivery_route__get :one
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
WHERE id = @id;
