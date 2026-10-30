-- name: workspace_archive__claim :one
WITH restoring AS (
 UPDATE control_plane.session_workspace_storage w
 SET state='RESTORING',version=version+1,updated_at=clock_timestamp()
 WHERE w.id=@workspace_id::uuid AND w.organization_id=@organization_id::uuid AND w.generation=@workspace_generation AND w.state='RESTORE_READY'
 AND EXISTS(SELECT 1 FROM control_plane.session_archive_tasks t WHERE t.id=@task_id::uuid AND t.workspace_id=w.id
  AND t.kind='RESTORE' AND t.generation=@expected_generation AND t.attempt<5
  AND (t.state='READY' AND t.available_at<=clock_timestamp() OR t.state='CLAIMED' AND t.lease_expires_at<=clock_timestamp()))
 RETURNING w.id
)
UPDATE control_plane.session_archive_tasks t
SET state='CLAIMED',attempt=attempt+1,generation=generation+1,input_digest=@input_digest,
 workload_instance=@workload_instance,lease_ref=@lease_ref,fence_digest=@fence_digest,lease_expires_at=@lease_expires_at,
 safe_error_code='',updated_at=clock_timestamp()
WHERE t.id=@task_id::uuid AND t.organization_id=@organization_id::uuid AND t.workspace_id=@workspace_id::uuid
 AND t.workspace_generation=@workspace_generation AND t.generation=@expected_generation AND t.attempt<5
 AND (t.state='READY' AND t.available_at<=clock_timestamp() OR t.state='CLAIMED' AND t.lease_expires_at<=clock_timestamp())
 AND EXISTS(SELECT 1 FROM control_plane.session_workspace_storage w WHERE w.id=t.workspace_id AND w.generation=t.workspace_generation
   AND (t.kind='SNAPSHOT' AND w.state='CHECKPOINT_PENDING' AND w.writer_released AND w.quiescence_record=t.workspace_input->>'quiescence'
    OR t.kind='RESTORE' AND w.state IN('RESTORE_READY','RESTORING') AND NOT w.writer_released AND w.source_archive_id=t.archive_id))
RETURNING attempt,generation;
