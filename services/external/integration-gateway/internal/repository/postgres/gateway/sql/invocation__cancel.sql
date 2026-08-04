-- name: InvocationCancel
UPDATE integration_gateway.invocations SET
    status = 'CANCELLED', payload = @payload::jsonb, updated_at = @cancelled_at
 WHERE invocation_id = @invocation_id AND status IN ('PENDING_APPROVAL', 'APPROVED')
RETURNING invocation_id
