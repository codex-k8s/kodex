-- name: workflow_rework__supersede_gate :exec
UPDATE control_plane.owner_gates gate SET state='CANCELLED',closure_kind='SUPERSEDED',
    decision=NULL,decision_comment='',resolved_by=@actor_id::uuid,resolved_at=clock_timestamp(),version=version+1
WHERE gate.organization_id=@organization_id::uuid AND gate.root_run_id=@root_run_id::uuid
    AND gate.id=@gate_id::uuid AND gate.ref=@gate_ref AND gate.version=@gate_version::bigint
    AND gate.node_id=@node_id::uuid AND gate.integration_invocation_id IS NULL
    AND gate.state='OPEN' AND gate.closure_kind='';
