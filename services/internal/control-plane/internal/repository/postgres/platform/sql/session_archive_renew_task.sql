-- name: session_archive_renew_task :one
UPDATE control_plane.session_archive_tasks
SET lease_expires_at = @lease_expires_at, updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid AND ref = @task_ref
  AND state = 'CLAIMED' AND lease_ref = @lease_ref
  AND fence_digest = @fence_digest AND generation = @generation
  AND lease_expires_at > clock_timestamp()
  AND NOT EXISTS (SELECT 1 FROM control_plane.session_workspace_storage workspace
                  WHERE workspace.session_id=session_archive_tasks.session_id)
RETURNING lease_expires_at;
