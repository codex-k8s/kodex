-- name: provider_operation__list :many
SELECT
    id,
    command_id,
    actor_id,
    external_account_id,
    provider_slug,
    operation_type,
    target_ref,
    status,
    result_ref,
    error_code,
    error_message,
    rate_limit_snapshot_id,
    operation_policy_context_json,
    approval_gate_ref_json,
    provider_version,
    started_at,
    finished_at,
    version,
    created_at,
    updated_at
FROM provider_hub_operations
WHERE (@provider_slug::text = '' OR provider_slug = @provider_slug)
  AND (@external_account_id::uuid IS NULL OR external_account_id = @external_account_id)
  AND (cardinality(@operation_types::text[]) = 0 OR operation_type = ANY(@operation_types::text[]))
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
  AND (@target_ref::text = '' OR target_ref = @target_ref)
  AND (@started_since::timestamptz IS NULL OR started_at >= @started_since)
ORDER BY started_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
