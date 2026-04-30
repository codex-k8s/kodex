-- name: command_result__get :one
SELECT key, command_id, idempotency_key, operation, aggregate_type, aggregate_id, created_at
FROM access_command_results
WHERE (@command_id::uuid IS NOT NULL AND command_id = @command_id)
   OR (@idempotency_key <> '' AND idempotency_key = @idempotency_key)
ORDER BY created_at DESC
LIMIT 1;
