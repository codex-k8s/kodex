-- name: secret_draft_expire_operations :exec
UPDATE control_plane.runtime_secret_draft_operations
SET state='FAILED',failure_code='GRANT_EXPIRED',grant_expires_at=LEAST(grant_expires_at,clock_timestamp()),
lease_deadline=LEAST(lease_deadline,clock_timestamp()),updated_at=clock_timestamp()
WHERE draft_id=@draft_id::uuid AND state IN ('PREPARED','CLAIMED');
