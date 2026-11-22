-- name: ssh_execution__finish :exec
WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at)
UPDATE control_plane.integration_invocations invocation
SET state=@state,result_summary=@result,safe_error_code=@reason,effect_receipt_id=NULLIF(@receipt_id,'')::uuid,
 lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,version=invocation.version+1,updated_at=stamp.at
FROM control_plane.ssh_invocation_identity_bindings pin,stamp
WHERE invocation.id=@invocation_id::uuid AND invocation.organization_id=@organization_id::uuid
 AND pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
 AND control_plane.ssh_invocation_identity_current(pin)
 AND invocation.state='RUNNING' AND invocation.workload_instance=@instance AND invocation.lease_ref=@lease_ref
 AND invocation.effect_fence_digest=@fence_digest AND invocation.generation=@generation AND invocation.input_digest=@input_digest
 AND invocation.lease_expires_at>stamp.at
 AND (invocation.approval_policy<>'HUMAN_SCOPED' OR EXISTS(SELECT 1 FROM control_plane.integration_approval_scopes approval
  WHERE approval.id=invocation.approval_scope_id AND approval.organization_id=invocation.organization_id
   AND approval.revoked_at IS NULL AND approval.expires_at>stamp.at))
 AND EXISTS(SELECT 1 FROM control_plane.ssh_execution_intents intent WHERE intent.invocation_id=invocation.id
  AND intent.organization_id=invocation.organization_id AND intent.ref=@attempt_ref AND intent.expires_at>stamp.at);
