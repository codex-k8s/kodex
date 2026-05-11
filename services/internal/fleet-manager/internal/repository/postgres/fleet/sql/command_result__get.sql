-- name: command_result__get :one
SELECT
    key, command_id, idempotency_key, actor_type, actor_id, operation,
    aggregate_type, aggregate_id, result_payload, created_at
FROM fleet_manager_command_results
WHERE (@command_id::uuid IS NOT NULL AND command_id = @command_id)
   OR (
        @command_id::uuid IS NULL
        AND operation = @operation
        AND actor_type = @actor_type
        AND actor_id = @actor_id
        AND idempotency_key = @idempotency_key
    );
