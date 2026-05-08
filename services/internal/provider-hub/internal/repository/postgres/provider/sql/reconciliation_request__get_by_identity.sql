-- name: reconciliation_request__get_by_identity :one
SELECT
    id,
    provider_slug,
    external_account_id,
    scope_type,
    scope_ref,
    idempotency_key,
    artifact_kinds_json,
    priority,
    created_at,
    updated_at
FROM provider_hub_reconciliation_requests
WHERE provider_slug = @provider_slug
  AND scope_type = @scope_type
  AND scope_ref = @scope_ref
  AND idempotency_key = @idempotency_key;
