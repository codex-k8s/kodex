-- name: ssh_setup__cancel :exec
WITH closed AS (
 UPDATE control_plane.ssh_identity_upload_attempts SET state='CLOSED',closed_at=clock_timestamp(),close_reason='SETUP_CANCELLED'
 WHERE organization_id=@organization_id::uuid AND setup_id=@setup_id::uuid AND state='ACTIVE' RETURNING id
)
UPDATE control_plane.ssh_identity_setups
SET state=CASE WHEN clock_timestamp()>=expires_at THEN 'EXPIRED' ELSE 'CANCELLED' END,
 reason_code=CASE WHEN clock_timestamp()>=expires_at THEN 'SETUP_EXPIRED' ELSE 'CANCELLED' END,
 version=version+1,closed_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND id=@setup_id::uuid AND version=@expected_version
 AND state IN('PREPARED','ATTEMPTING','UNKNOWN');
