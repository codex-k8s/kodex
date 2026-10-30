-- name: workspace_volume__prepare :one
UPDATE control_plane.session_workspace_volumes v
SET state='PREPARE_PENDING',preparation_task_ref=COALESCE(preparation_task_ref,@preparation_task_ref),
 block_reason='',version=version+1,updated_at=clock_timestamp()
WHERE v.id=@volume_id::uuid AND v.generation=@generation AND v.pvc_uid=@pvc_uid
 AND v.state IN('CREATED','PREPARE_PENDING','BLOCKED')
 AND (v.preparation_task_ref IS NULL OR v.preparation_task_ref=@preparation_task_ref)
 AND NOT EXISTS(SELECT 1 FROM control_plane.session_workspace_volume_tasks t
   WHERE t.volume_id=v.id AND t.volume_generation=v.generation AND t.state IN('READY','CLAIMED'))
RETURNING v.id::text,v.ref,v.generation,v.version,v.state,v.create_task_ref,
 COALESCE(v.preparation_task_ref,''),COALESCE(v.pvc_uid,''),COALESCE(v.ready_record,'');
