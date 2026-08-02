-- name: repair__get_by_idempotency :one
SELECT
    request_digest,
    repair_id::text,
    event_id::text,
    event_digest,
    expected_generation,
    expected_fence,
    result_generation,
    result_fence,
    created_at
FROM runtime_inbox_repairs
WHERE consumer_name = @consumer_name
  AND consumer_scope = @consumer_scope
  AND idempotency_key = @idempotency_key;
