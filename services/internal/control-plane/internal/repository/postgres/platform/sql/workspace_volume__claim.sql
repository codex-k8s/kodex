-- name: workspace_volume__claim :one
UPDATE control_plane.session_workspace_volume_tasks
SET state='CLAIMED',attempt=attempt+1,updated_at=clock_timestamp()
WHERE id=@task_id::uuid AND state IN('READY','CLAIMED') AND attempt=@previous_attempt
 AND attempt<maximum_attempts
RETURNING attempt;
