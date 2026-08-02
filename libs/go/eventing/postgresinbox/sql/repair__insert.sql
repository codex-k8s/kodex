-- name: repair__insert :one
INSERT INTO runtime_inbox_repairs (
    consumer_name,
    consumer_scope,
    idempotency_key,
    request_digest,
    repair_id,
    event_id,
    event_digest,
    expected_generation,
    expected_fence,
    action,
    actor,
    reason,
    evidence_digest,
    result_generation,
    result_fence
)
VALUES (
    @consumer_name,
    @consumer_scope,
    @idempotency_key,
    @request_digest,
    @repair_id::uuid,
    @event_id::uuid,
    @event_digest,
    @expected_generation,
    @expected_fence,
    'REQUEUE',
    @actor,
    @reason,
    @evidence_digest,
    @result_generation,
    @result_fence
)
RETURNING created_at;
