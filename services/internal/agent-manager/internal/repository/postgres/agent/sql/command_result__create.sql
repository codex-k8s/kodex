-- name: command_result__create :exec
INSERT INTO agent_manager_command_results (
    key, command_id, idempotency_key, actor_type, actor_id, operation,
    aggregate_type, aggregate_id, result_payload, created_at
) VALUES (
    @key, @command_id::uuid, @idempotency_key, @actor_type, @actor_id, @operation,
    @aggregate_type, @aggregate_id, @result_payload::jsonb, @created_at
);
