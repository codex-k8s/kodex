-- name: ApprovalUpdate
WITH changed_approval AS (
    UPDATE integration_gateway.approvals SET
        status = @approval_status,
        payload = @approval_payload::jsonb,
        decided_at = @decided_at
     WHERE approval_id = @approval_id AND status = 'PENDING'
       AND request_hash = @request_hash AND expires_at > clock_timestamp()
    RETURNING invocation_id
)
UPDATE integration_gateway.invocations AS invocation SET
    status = @invocation_status,
    payload = @invocation_payload::jsonb,
    updated_at = @decided_at
  FROM changed_approval
 WHERE invocation.invocation_id = changed_approval.invocation_id
   AND invocation.status = 'PENDING_APPROVAL'
   AND invocation.canonical_request_hash = @request_hash
RETURNING invocation.payload
