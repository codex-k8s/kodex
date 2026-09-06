-- name: provider_authorization_reservation_abandon :exec
UPDATE control_plane.provider_authorization_attempts
SET preparation_state='ABANDONED', state='FAILED', safe_failure_code='SUPERSEDED',
    verification_uri='', user_code='', version=version+1, updated_at=clock_timestamp()
WHERE provider_account_id=@account_id::uuid
  AND (preparation_state='RESERVED' OR state='PENDING');
