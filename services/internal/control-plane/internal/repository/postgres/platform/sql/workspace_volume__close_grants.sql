-- name: workspace_volume__close_grants :exec
UPDATE control_plane.session_workspace_volume_attempts
SET grant_state='CLOSED'
WHERE task_id=@task_id::uuid AND grant_state='ACTIVE';
