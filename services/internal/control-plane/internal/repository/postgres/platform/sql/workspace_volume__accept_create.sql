-- name: workspace_volume__accept_create :one
UPDATE control_plane.session_workspace_volumes
SET state='CREATED',pvc_uid=@pvc_uid,block_reason='',version=version+1,updated_at=clock_timestamp()
WHERE id=@volume_id::uuid AND generation=@generation AND create_task_ref=@create_task_ref
 AND pvc_uid IS NULL AND state IN('CREATE_PENDING','BLOCKED')
RETURNING state;
