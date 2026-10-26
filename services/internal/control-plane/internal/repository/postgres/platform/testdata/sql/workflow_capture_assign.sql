-- name: workflow_capture_assign :exec
UPDATE control_plane.run_nodes anchor
SET materialization_state='MATERIALIZED',state='QUEUED',turn_id=original.turn_id,workflow_business_attempt=1
FROM control_plane.run_nodes original
WHERE anchor.ref=@node_ref AND original.run_id=anchor.run_id AND original.organization_id=anchor.organization_id
    AND original.type='AGENT_EXECUTION' AND original.state='SKIPPED' AND original.workflow_step_key=''
    AND anchor.state='PLANNED' AND anchor.workflow_business_attempt=0;
