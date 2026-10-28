-- name: workspace_archive__expire :one
UPDATE control_plane.session_archive_tasks
SET state='DEAD_LETTER',workload_instance=NULL,lease_ref=NULL,fence_digest=NULL,lease_expires_at=NULL,
 safe_error_code='SESSION_ARCHIVE_LEASE_EXPIRED',completed_at=clock_timestamp(),updated_at=clock_timestamp()
WHERE id=@task_id::uuid AND state='CLAIMED' AND attempt>=5 AND generation=@generation AND lease_expires_at<=clock_timestamp()
RETURNING ref;
