-- name: integration_approval__lock_call_scope :one
SELECT approval.id::text
FROM control_plane.integration_approval_scopes approval
JOIN control_plane.integration_invocations invocation ON invocation.approval_scope_id=approval.id
WHERE invocation.id=$1::uuid AND invocation.organization_id=$2::uuid
 AND approval.organization_id=invocation.organization_id
 AND ((approval.revoked_at IS NULL AND approval.expires_at>clock_timestamp())
   OR invocation.state='UNKNOWN_OUTCOME' OR (invocation.state='RUNNING' AND invocation.claimed_recovery_read_only))
FOR UPDATE OF approval
