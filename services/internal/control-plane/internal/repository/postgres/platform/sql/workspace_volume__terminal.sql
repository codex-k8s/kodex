-- name: workspace_volume__terminal :one
UPDATE control_plane.session_workspace_volume_tasks
SET state=@state,safe_error_code=@safe_error_code,completed_at=clock_timestamp(),updated_at=clock_timestamp()
WHERE id=@task_id::uuid AND state IN('READY','CLAIMED')
RETURNING state;
