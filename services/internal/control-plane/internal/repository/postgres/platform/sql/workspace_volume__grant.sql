-- name: workspace_volume__grant :one
INSERT INTO control_plane.session_workspace_volume_attempts
 (task_id,attempt,lease_ref,fence_digest,workload_instance,input_snapshot,input_digest,expires_at)
VALUES(@task_id::uuid,@attempt,@lease_ref,@fence_digest,@workload_instance,@input_snapshot,@input_digest,
 LEAST(clock_timestamp()+interval '2 minutes',COALESCE(@deadline_at::timestamptz,'infinity'::timestamptz)))
RETURNING expires_at;
