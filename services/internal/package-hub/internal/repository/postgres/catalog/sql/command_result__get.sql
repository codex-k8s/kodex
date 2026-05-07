-- name: command_result__get :one
SELECT
    key,
    command_id,
    idempotency_key,
    operation,
    aggregate_type,
    aggregate_id,
    result_payload,
    created_at
FROM package_hub_command_results
WHERE (@command_id::uuid IS NOT NULL AND command_id = @command_id::uuid)
   OR (@idempotency_key::text <> '' AND operation = @operation AND idempotency_key = @idempotency_key)
ORDER BY CASE WHEN @command_id::uuid IS NOT NULL AND command_id = @command_id::uuid THEN 0 ELSE 1 END
LIMIT 1;
