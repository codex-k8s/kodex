-- name: operator__insert_receipt :one
INSERT INTO runtime_inbox_repairs (
    consumer_name,
    consumer_scope,
    organization_scope,
    project_scope,
    operation,
    key_hash,
    request_digest,
    receipt_id,
    event_id,
    event_digest,
    expected_generation,
    expected_fence,
    action,
    actor,
    reason,
    evidence_digest,
    result_generation,
    result_fence,
    result_directive
)
VALUES (
    @consumer_name,
    @consumer_scope,
    @organization_scope,
    @project_scope,
    @operation,
    @key_hash,
    @request_digest,
    @receipt_id::uuid,
    @event_id::uuid,
    @event_digest,
    @expected_generation,
    @expected_fence,
    @action,
    @actor,
    @reason,
    @evidence_digest,
    @result_generation,
    @result_fence,
    @result_directive
)
RETURNING created_at;
