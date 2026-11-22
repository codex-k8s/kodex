-- name: kubernetes_setup__close_authority :exec
WITH closed_attempts AS (
 UPDATE control_plane.kubernetes_identity_upload_attempts
 SET state=CASE WHEN @state='ACK_REJECTED' THEN 'CANCELLED' ELSE @state END,closed_at=clock_timestamp()
 WHERE organization_id=@organization_id::uuid AND setup_id=@setup_id::uuid AND state='ACTIVE' RETURNING id
)
UPDATE control_plane.kubernetes_identity_setups SET state=@state,version=version+1
WHERE organization_id=@organization_id::uuid AND id=@setup_id::uuid AND state IN('PREPARED','ATTEMPTING','UNKNOWN');
