-- name: runtime_completeexecution_cancel_planned_workflow_nodes :many
UPDATE control_plane.run_nodes
SET state='CANCELLED', safe_error_code=@safe_error_code,
    safe_error_message='', next_actions=ARRAY['OPEN'],
    finished_at=clock_timestamp(), version=version+1
WHERE root_run_id=@root_run_id::uuid
  AND type='AGENT_EXECUTION'
  AND state='PLANNED'
  AND materialization_state='PLANNED'
RETURNING ref
