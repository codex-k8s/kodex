-- name: kubernetes_setup__cancel :exec
WITH closed_attempts AS (
 UPDATE control_plane.kubernetes_identity_upload_attempts
 SET state='CANCELLED',closed_at=clock_timestamp()
 WHERE organization_id=@organization_id::uuid AND setup_id=@setup_id::uuid AND state='ACTIVE'
 RETURNING id
)
UPDATE control_plane.kubernetes_identity_setups
SET state='CANCELLED',version=version+1
WHERE organization_id=@organization_id::uuid AND id=@setup_id::uuid
 AND version=@expected_version AND state IN('PREPARED','ATTEMPTING','UNKNOWN');
