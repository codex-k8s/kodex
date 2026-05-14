-- name: role_profile__get :one
SELECT
    id,
    scope_type,
    scope_ref,
    slug,
    display_name,
    icon_object_uri,
    role_kind,
    runtime_profile,
    allowed_mcp_tools,
    provider_account_policy_ref,
    status,
    version,
    created_at,
    updated_at
FROM agent_manager_role_profiles
WHERE id = @id;
