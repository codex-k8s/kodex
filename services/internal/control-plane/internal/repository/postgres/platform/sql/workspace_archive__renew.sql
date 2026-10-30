-- name: workspace_archive__renew :one
UPDATE control_plane.session_archive_tasks t SET lease_expires_at=@lease_expires_at,updated_at=clock_timestamp()
WHERE t.id=@task_id::uuid AND t.state='CLAIMED' AND t.workspace_id=@workspace_id::uuid AND t.workspace_generation=@workspace_generation
 AND t.lease_ref=@lease_ref AND t.fence_digest=@fence_digest AND t.generation=@generation AND t.lease_expires_at>clock_timestamp()
 AND EXISTS(SELECT 1 FROM control_plane.session_workspace_storage w WHERE w.id=t.workspace_id AND w.generation=t.workspace_generation
   AND (t.kind='SNAPSHOT' AND w.state='CHECKPOINT_PENDING' AND w.writer_released AND w.quiescence_record=t.workspace_input->>'quiescence'
    OR t.kind='RESTORE' AND w.state='RESTORING' AND NOT w.writer_released AND w.source_archive_id=t.archive_id))
RETURNING t.lease_expires_at;
