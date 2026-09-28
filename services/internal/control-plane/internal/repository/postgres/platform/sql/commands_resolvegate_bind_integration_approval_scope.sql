-- name: commands_resolvegate_bind_integration_approval_scope :exec
UPDATE control_plane.integration_invocations
SET approval_scope_id=$2::uuid
WHERE id=$1::uuid AND state='WAITING_APPROVAL' AND approval_policy='HUMAN_SCOPED'
  AND approval_scope_id IS NULL
