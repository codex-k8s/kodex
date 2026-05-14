-- name: role_profile__update :exec
UPDATE agent_manager_role_profiles
SET
    display_name = @display_name,
    icon_object_uri = @icon_object_uri,
    role_kind = @role_kind,
    runtime_profile = @runtime_profile,
    allowed_mcp_tools = @allowed_mcp_tools,
    provider_account_policy_ref = @provider_account_policy_ref,
    status = @status,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
