-- name: ssh_setup__complete_state :exec
WITH closed AS (
 UPDATE control_plane.ssh_identity_upload_attempts SET state='CLOSED',closed_at=coalesce(closed_at,clock_timestamp()),close_reason=coalesce(close_reason,@reason)
 WHERE setup_id=@setup_id::uuid AND state='ACTIVE' RETURNING id
)
UPDATE control_plane.ssh_identity_setups SET state=@state,reason_code=@reason,version=version+1,
 closed_at=CASE WHEN @state IN('PREPARED','ATTEMPTING','UNKNOWN') THEN NULL ELSE coalesce(closed_at,clock_timestamp()) END
WHERE id=@setup_id::uuid AND organization_id=@organization_id::uuid AND version=@version
 AND (@state NOT IN('UNKNOWN','COMPLETED') OR clock_timestamp()<least(expires_at,@authorized_until::timestamptz));
