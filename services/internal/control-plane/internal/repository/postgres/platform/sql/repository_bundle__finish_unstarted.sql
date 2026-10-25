-- name: repository_bundle__finish_unstarted :exec
WITH closed AS (
    UPDATE control_plane.integration_invocations
    SET state=CASE WHEN EXISTS (SELECT 1 FROM control_plane.owner_gates gate WHERE gate.id=@gate_id::uuid AND gate.state='OPEN') THEN 'CANCELLED' ELSE 'FAILED' END,
        safe_error_code=@safe_error_code,
        lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
        version=version+1,updated_at=clock_timestamp()
    WHERE organization_id=@organization_id::uuid AND id=@invocation_id::uuid
      AND operation='github.repository.bundle.publish' AND effect_receipt_id IS NULL
      AND state IN ('WAITING_APPROVAL','READY','RUNNING','UNKNOWN_OUTCOME')
    RETURNING id
), closed_gates AS (
    UPDATE control_plane.owner_gates gate
    SET state='CANCELLED',decision='CANCEL',decision_comment=@decision_comment,
        resolved_by=@actor_id::uuid,resolved_at=clock_timestamp(),version=version+1
    WHERE gate.organization_id=@organization_id::uuid AND gate.id=@gate_id::uuid
      AND gate.integration_invocation_id IN (SELECT id FROM closed) AND gate.state='OPEN'
    RETURNING node_id
)
UPDATE control_plane.run_nodes
SET state='CANCELLED',finished_at=clock_timestamp(),version=version+1,next_actions=ARRAY['OPEN']
WHERE organization_id=@organization_id::uuid AND id IN (SELECT node_id FROM closed_gates) AND state='WAITING';
