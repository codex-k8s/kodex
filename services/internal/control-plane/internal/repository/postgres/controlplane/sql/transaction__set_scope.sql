-- name: TransactionActivateRuntimeContext
SELECT control_plane.activate_runtime_context(
    @organization_id::uuid,
    nullif(@project_id, '')::uuid,
    @actor_id::uuid,
    @principal_name::name,
    @principal_generation,
    @context_key_id,
    @nonce::uuid,
    @expires_unix_micro,
    @signature::bytea
)
