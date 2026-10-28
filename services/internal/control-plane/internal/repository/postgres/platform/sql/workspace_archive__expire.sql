-- name: workspace_archive__expire :one
WITH closed AS (
UPDATE control_plane.session_archive_tasks
SET state='DEAD_LETTER',workload_instance=NULL,lease_ref=NULL,fence_digest=NULL,lease_expires_at=NULL,
 safe_error_code='SESSION_ARCHIVE_LEASE_EXPIRED',completed_at=clock_timestamp(),updated_at=clock_timestamp()
WHERE id=@task_id::uuid AND state='CLAIMED' AND attempt>=5 AND generation=@generation AND lease_expires_at<=clock_timestamp()
RETURNING ref,state,kind,workspace_id,workspace_generation
), blocked AS (
 UPDATE control_plane.session_workspace_storage w
 SET state='BLOCKED',block_reason='RESTORE_UNCONFIRMED',version=version+1,updated_at=clock_timestamp()
 FROM closed WHERE closed.kind='RESTORE' AND closed.state='DEAD_LETTER'
  AND w.id=closed.workspace_id AND w.generation=closed.workspace_generation AND w.state='RESTORING'
 RETURNING w.id
)
SELECT ref FROM closed;
