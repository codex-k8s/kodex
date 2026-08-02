-- name: InvocationComplete
UPDATE integration_gateway.invocations SET
    status = @status, payload = @payload::jsonb, updated_at = @updated_at
 WHERE invocation_id = @invocation_id AND status = 'EXECUTING'
   AND connection_generation = @connection_generation
   AND grant_generation = @grant_generation
RETURNING invocation_id
