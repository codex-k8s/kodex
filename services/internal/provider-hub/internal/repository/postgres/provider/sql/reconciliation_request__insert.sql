-- name: reconciliation_request__insert :one
INSERT INTO provider_hub_reconciliation_requests (
    id,
    provider_slug,
    scope_type,
    scope_ref,
    idempotency_key,
    artifact_kinds_json,
    priority,
    created_at,
    updated_at
) VALUES (
    @id,
    @provider_slug,
    @scope_type,
    @scope_ref,
    @idempotency_key,
    @artifact_kinds_json,
    @priority,
    @created_at,
    @updated_at
)
ON CONFLICT (provider_slug, scope_type, scope_ref, idempotency_key) DO NOTHING
RETURNING
    id,
    provider_slug,
    scope_type,
    scope_ref,
    idempotency_key,
    artifact_kinds_json,
    priority,
    created_at,
    updated_at;
