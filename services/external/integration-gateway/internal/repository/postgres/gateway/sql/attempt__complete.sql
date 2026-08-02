-- name: AttemptComplete
UPDATE integration_gateway.execution_attempts SET
    outcome = @outcome, payload = @payload::jsonb, finished_at = @finished_at
 WHERE attempt_id = @attempt_id AND invocation_id = @invocation_id
   AND fence = @fence AND finished_at IS NULL
   AND connection_generation = @connection_generation
   AND grant_generation = @grant_generation
RETURNING attempt_id
