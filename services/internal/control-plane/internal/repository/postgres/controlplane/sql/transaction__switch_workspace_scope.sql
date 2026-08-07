SELECT control_plane.switch_runtime_workspace_context(
    @organization_id::uuid,
    nullif(@project_id, '')::uuid,
    @actor_id::uuid,
    @principal_name::name,
    @principal_generation,
    @context_key_id,
    @nonce::uuid,
    @expires_unix_micro,
    @signature::bytea
);
