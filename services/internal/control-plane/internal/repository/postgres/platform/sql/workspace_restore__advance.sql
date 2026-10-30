-- name: workspace_restore__advance :one
UPDATE control_plane.session_workspace_storage w
SET generation=generation+1,state='RESTORE_READY',source_archive_id=current_archive_id,current_archive_id=NULL,
 writer_lease_id=NULL,writer_acknowledgement=NULL,quiescence_record=NULL,writer_released=false,
 version=version+1,updated_at=clock_timestamp()
WHERE w.id=@workspace_id::uuid AND w.organization_id=@organization_id::uuid AND w.version=@workspace_version
 AND w.generation=@workspace_generation AND w.generation<9223372036854775807
 AND w.state='CHECKPOINT_READY' AND w.writer_released AND w.current_archive_id=@archive_id::uuid
 AND w.destructive_task_id IS NULL
 AND NOT EXISTS(SELECT 1 FROM control_plane.session_archive_tasks t WHERE t.session_id=w.session_id AND t.state IN('READY','CLAIMED'))
RETURNING w.generation;
