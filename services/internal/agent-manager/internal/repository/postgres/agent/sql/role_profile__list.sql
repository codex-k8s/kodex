-- name: role_profile__list :many
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
WHERE (@scope_type::text IS NULL OR scope_type = @scope_type::text)
  AND (@scope_ref::text IS NULL OR scope_ref = @scope_ref::text)
  AND (@role_kind::text IS NULL OR role_kind = @role_kind::text)
  AND (@status::text IS NULL OR status = @status::text)
ORDER BY scope_type, scope_ref, slug, id
LIMIT @limit::integer
OFFSET @offset::integer;
