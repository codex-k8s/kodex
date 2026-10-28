-- name: workspace_volume__renew :one
UPDATE control_plane.session_workspace_volume_attempts
SET expires_at=GREATEST(expires_at,LEAST(clock_timestamp()+interval '2 minutes',
 COALESCE(@deadline_at::timestamptz,'infinity'::timestamptz)))
WHERE task_id=@task_id::uuid AND lease_ref=@lease_ref AND attempt=@generation::integer
 AND fence_digest=@fence_digest AND grant_state='ACTIVE' AND expires_at>clock_timestamp()
 AND reported_at IS NULL
RETURNING expires_at;
