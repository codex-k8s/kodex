-- name: operator__get_receipt :one
SELECT
    request_digest,
    receipt_id::text,
    event_id::text,
    event_digest,
    expected_generation,
    expected_fence,
    action,
    result_generation,
    result_fence,
    result_directive,
    created_at
FROM runtime_inbox_repairs
WHERE organization_scope = @organization_scope
  AND project_scope = @project_scope
  AND operation = @operation
  AND key_hash = @key_hash;
