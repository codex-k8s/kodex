-- name: interaction_approval_invalidate :many
WITH closed AS (
    UPDATE control_plane.interaction_deliveries delivery
    SET state=CASE WHEN delivery.state='CLAIMED' THEN 'UNKNOWN_OUTCOME' ELSE 'CANCELLED' END,
        safe_error_code=CASE WHEN delivery.state='CLAIMED' THEN 'INTERACTION_OUTCOME_UNKNOWN' ELSE 'INTERACTION_AUTHORITY_CHANGED' END,
        lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
        completed_at=clock_timestamp(),version=delivery.version+1,updated_at=clock_timestamp()
    FROM control_plane.integration_connections connection
    WHERE connection.organization_id=@organization_id::uuid AND connection.ref=@connection_ref
      AND delivery.connection_id=connection.id AND delivery.organization_id=connection.organization_id
      AND delivery.approval_gate_id IS NOT NULL
      AND delivery.state IN ('WAITING_APPROVAL','DUE','FAILED','CLAIMED')
    RETURNING delivery.approval_gate_id,delivery.root_run_id,delivery.project_id
)
SELECT gate.id::text,gate.ref,gate.node_id::text,node.ref,closed.root_run_id::text,closed.project_id::text,run.state
FROM closed
JOIN control_plane.owner_gates gate ON gate.id=closed.approval_gate_id AND gate.state='OPEN'
JOIN control_plane.run_nodes node ON node.id=gate.node_id
JOIN control_plane.runs run ON run.id=closed.root_run_id
ORDER BY gate.ref
FOR UPDATE OF gate;
