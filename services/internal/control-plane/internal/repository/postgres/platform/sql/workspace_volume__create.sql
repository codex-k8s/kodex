-- name: workspace_volume__create :one
INSERT INTO control_plane.session_workspace_volumes(ref,workspace_id,state,create_task_ref)
SELECT @volume_ref,w.id,'CREATE_PENDING',@create_task_ref
FROM control_plane.session_workspace_storage w
WHERE w.organization_id=@organization_id::uuid AND w.ref=@workspace_ref
 AND w.state='EMPTY' AND w.writer_lease_id IS NULL
RETURNING id::text,ref,generation,version,state,create_task_ref,
 COALESCE(preparation_task_ref,''),COALESCE(pvc_uid,''),COALESCE(ready_record,'');
