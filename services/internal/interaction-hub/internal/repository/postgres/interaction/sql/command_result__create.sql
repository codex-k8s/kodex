-- name: command_result__create :exec
INSERT INTO interaction_hub_command_results (
    key,
    command_id,
    idempotency_key,
    actor_ref,
    operation,
    aggregate_type,
    aggregate_id,
    request_fingerprint,
    result_payload,
    created_at
) VALUES (
    @key,
    @command_id::uuid,
    @idempotency_key,
    @actor_ref,
    @operation,
    @aggregate_type,
    @aggregate_id,
    @request_fingerprint,
    @result_payload::jsonb,
    @created_at
)
ON CONFLICT DO NOTHING;
