-- name: command_result__create :exec
INSERT INTO access_command_results (
    key, command_id, idempotency_key, operation, aggregate_type, aggregate_id, created_at
) VALUES (
    @key, @command_id, @idempotency_key, @operation, @aggregate_type, @aggregate_id, @created_at
)
ON CONFLICT DO NOTHING;
