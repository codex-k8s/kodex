-- name: workspace_archive__complete :one
WITH receipt AS (
 INSERT INTO control_plane.session_archives(ref,organization_id,project_id,session_id,provider_account_id,runtime_revision_id,
   codex_session_id,content_generation,format_version,source_relative_path,source_sha256,source_size_bytes,
   object_key,object_version,object_etag,object_digest,object_size_bytes,retention_until,
   workspace_id,workspace_generation,workspace_checkpoint_ref,workspace_entries,workspace_quiescence)
 SELECT @archive_ref,w.organization_id,w.project_id,w.session_id,r.provider_account_id,r.id,
   NULL,t.content_generation,2,NULL,@tree_sha256,@source_bytes,@object_key,@object_version,@object_etag,@object_digest,@object_bytes,
   clock_timestamp()+interval '30 days',w.id,w.generation,t.workspace_input->>'checkpoint_ref',@source_entries,w.quiescence_record
 FROM control_plane.session_workspace_storage w
 JOIN control_plane.runtime_leases l ON l.id=w.writer_lease_id
 JOIN control_plane.runtime_revisions r ON r.id=l.runtime_revision_id AND r.session_id=w.session_id AND r.organization_id=w.organization_id
 JOIN control_plane.session_archive_tasks t ON t.id=@task_id::uuid AND t.workspace_id=w.id AND t.workspace_generation=w.generation
 WHERE w.id=@workspace_id::uuid AND w.organization_id=@organization_id::uuid AND w.version=@workspace_version
   AND w.state='CHECKPOINT_PENDING' AND w.writer_released AND t.state='CLAIMED' AND t.generation=@task_generation
   AND t.lease_ref=@lease_ref AND t.fence_digest=@fence_digest AND t.lease_expires_at>clock_timestamp()
 RETURNING id,ref,workspace_id,workspace_generation
), stored AS (
 UPDATE control_plane.session_workspace_storage w
 SET state='CHECKPOINT_READY',current_archive_id=receipt.id,version=version+1,updated_at=clock_timestamp()
 FROM receipt WHERE w.id=receipt.workspace_id AND w.generation=receipt.workspace_generation AND w.version=@workspace_version
 RETURNING receipt.ref,w.id
), completed AS (
 UPDATE control_plane.session_archive_tasks t
 SET state='SUCCEEDED',workload_instance=NULL,lease_ref=NULL,fence_digest=NULL,lease_expires_at=NULL,completed_at=clock_timestamp(),updated_at=clock_timestamp()
 FROM stored WHERE t.id=@task_id::uuid AND t.workspace_id=stored.id AND t.state='CLAIMED' AND t.generation=@task_generation
 RETURNING stored.ref
)
SELECT ref FROM completed;
