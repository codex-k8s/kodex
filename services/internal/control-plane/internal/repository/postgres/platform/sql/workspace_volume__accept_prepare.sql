-- name: workspace_volume__accept_prepare :one
-- A late fact may persist its canonical record without granting READY to a cancelled origin.
UPDATE control_plane.session_workspace_volumes
SET ready_record=@ready_record,state=CASE WHEN @make_ready::boolean THEN 'READY' ELSE state END,
 block_reason=CASE WHEN @make_ready::boolean THEN '' ELSE block_reason END,
 version=version+1,updated_at=clock_timestamp()
WHERE id=@volume_id::uuid AND generation=@generation AND preparation_task_ref=@preparation_task_ref
 AND pvc_uid=@pvc_uid AND state IN('PREPARE_PENDING','READY','BLOCKED')
 AND (ready_record IS NULL OR ready_record=@ready_record)
RETURNING state;
