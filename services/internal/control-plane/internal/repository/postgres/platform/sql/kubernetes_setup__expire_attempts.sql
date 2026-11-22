-- name: kubernetes_setup__expire_attempts :exec
WITH expired AS (
 UPDATE control_plane.kubernetes_identity_upload_attempts
 SET state='UNKNOWN',closed_at=clock_timestamp()
 WHERE organization_id=@organization_id::uuid AND setup_id=@setup_id::uuid
  AND state='ACTIVE' AND expires_at<=clock_timestamp()
 RETURNING id
)
UPDATE control_plane.kubernetes_identity_setups SET state='UNKNOWN',version=version+1
WHERE organization_id=@organization_id::uuid AND id=@setup_id::uuid
 AND state='ATTEMPTING' AND EXISTS(SELECT 1 FROM expired);
