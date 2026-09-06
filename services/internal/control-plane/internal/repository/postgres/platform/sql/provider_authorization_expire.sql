-- name: provider_authorization_expire :exec
WITH expired AS (
 UPDATE control_plane.provider_authorization_attempts
 SET preparation_state='ABANDONED',state='FAILED',safe_failure_code='EXPIRED',
     version=version+1,updated_at=clock_timestamp()
 WHERE provider_account_id=@account_id::uuid AND preparation_state='RESERVED'
   AND reservation_deadline<=clock_timestamp()
 RETURNING reserved_account_version
)
UPDATE control_plane.provider_accounts account
SET state='REAUTHORIZATION_REQUIRED',enabled=false,version=version+1,updated_at=clock_timestamp()
WHERE account.id=@account_id::uuid AND account.state='PENDING_AUTHORIZATION'
  AND EXISTS (SELECT 1 FROM expired WHERE reserved_account_version=account.version);
