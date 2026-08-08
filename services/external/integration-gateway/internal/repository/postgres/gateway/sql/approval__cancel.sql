-- name: ApprovalCancel
UPDATE integration_gateway.approvals SET
    status = 'CANCELLED', version = version + 1, payload = @payload::jsonb, decided_at = @cancelled_at
 WHERE invocation_id = @invocation_id AND status IN ('PENDING', 'APPROVED')
RETURNING approval_id
