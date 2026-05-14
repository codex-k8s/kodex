-- name: role_profile__create :exec
INSERT INTO agent_manager_role_profiles (
    id, scope_type, scope_ref, slug, display_name, icon_object_uri, role_kind,
    runtime_profile, allowed_mcp_tools, provider_account_policy_ref, status,
    version, created_at, updated_at
) VALUES (
    @id, @scope_type, @scope_ref, @slug, @display_name, @icon_object_uri, @role_kind,
    @runtime_profile, @allowed_mcp_tools, @provider_account_policy_ref, @status,
    @version, @created_at, @updated_at
);
