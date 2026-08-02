-- name: InvocationMarkExecuting
UPDATE integration_gateway.invocations SET
    status = 'EXECUTING', payload = @payload::jsonb, updated_at = @updated_at
 WHERE invocation_id = @invocation_id AND status = 'APPROVED'
RETURNING invocation_id
