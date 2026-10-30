-- name: workspace_volume__observe :one
UPDATE control_plane.session_workspace_volume_attempts
SET grant_state='CLOSED',observed_pvc_uid=@pvc_uid,job_uid=NULLIF(@job_uid,''),pod_uid=NULLIF(@pod_uid,''),
 ready_record=NULLIF(@ready_record,''),reported_at=clock_timestamp()
WHERE task_id=@task_id::uuid AND attempt=@generation::integer AND lease_ref=@lease_ref
 AND fence_digest=@fence_digest AND reported_at IS NULL
RETURNING true;
