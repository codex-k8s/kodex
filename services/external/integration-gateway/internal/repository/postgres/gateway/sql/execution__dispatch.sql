-- name: ExecutionDispatch
UPDATE integration_gateway.execution_attempts SET
    provider_dispatched_at = @dispatched_at,
    payload = @payload::jsonb
 WHERE attempt_id = @attempt_id AND invocation_id = @invocation_id
   AND finished_at IS NULL AND provider_dispatched_at IS NULL
RETURNING attempt_id
