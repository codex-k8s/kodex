-- name: RuntimeReconcile
SELECT * FROM integration_gateway.reconcile_runtime_credentials(
    @principals::jsonb, @context_key_id, @context_key,
    @current_generation, @served_generation
)
