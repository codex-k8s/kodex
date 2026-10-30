-- name: workspace_restore__complete :one
WITH restored AS (
 UPDATE control_plane.session_workspace_storage w
 SET state='RESTORED',version=version+1,updated_at=clock_timestamp()
 FROM control_plane.session_archive_tasks t,control_plane.session_archives source
 WHERE w.id=@workspace_id::uuid AND w.organization_id=@organization_id::uuid AND w.version=@workspace_version
  AND w.state='RESTORING' AND w.writer_lease_id IS NULL AND w.source_archive_id=source.id
  AND source.id=@archive_id::uuid AND source.lifecycle_state='AVAILABLE' AND source.deleted_at IS NULL
  AND source.organization_id=w.organization_id AND source.project_id=w.project_id AND source.session_id=w.session_id
  AND source.workspace_id=w.id AND source.workspace_generation=w.generation-1 AND source.format_version=2
  AND t.id=@task_id::uuid AND t.kind='RESTORE' AND t.archive_id=source.id AND t.workspace_id=w.id
  AND t.workspace_generation=w.generation AND t.state='CLAIMED' AND t.generation=@task_generation
  AND t.lease_ref=@lease_ref AND t.fence_digest=@fence_digest AND t.lease_expires_at>clock_timestamp()
 RETURNING w.id,source.ref
), completed AS (
 UPDATE control_plane.session_archive_tasks t
 SET state='SUCCEEDED',workload_instance=NULL,lease_ref=NULL,fence_digest=NULL,lease_expires_at=NULL,
  completed_at=clock_timestamp(),updated_at=clock_timestamp()
 FROM restored WHERE t.id=@task_id::uuid AND t.workspace_id=restored.id
  AND t.state='CLAIMED' AND t.generation=@task_generation
 RETURNING restored.ref
)
SELECT ref FROM completed;
