-- name: command_result__create :exec
INSERT INTO package_hub_command_results (
    key,
    command_id,
    idempotency_key,
    operation,
    aggregate_type,
    aggregate_id,
    result_payload,
    created_at
) VALUES (
    @key,
    @command_id::uuid,
    @idempotency_key,
    @operation,
    @aggregate_type,
    @aggregate_id,
    @result_payload::jsonb,
    @created_at
)
ON CONFLICT DO NOTHING;
