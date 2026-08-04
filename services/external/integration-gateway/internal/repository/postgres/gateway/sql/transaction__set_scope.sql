-- name: TransactionActivateRuntimeContext
SELECT integration_gateway.activate_runtime_context(
    @tenant_id, @project_id, @actor_id, @principal_name::name,
    @principal_generation, @context_key_id, @nonce::uuid,
    @expires_unix_micro, @signature::bytea
)
