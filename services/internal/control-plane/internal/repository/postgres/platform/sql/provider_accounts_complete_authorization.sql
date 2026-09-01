-- name: provider_accounts_complete_authorization :exec
UPDATE control_plane.provider_authorization_attempts
SET state = @state,
    verification_uri = '',
    user_code = '',
    safe_failure_code = @safe_failure_code,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid
  AND provider_account_id = @account_id::uuid
  AND ref = @authorization_ref
  AND materializer_attempt_ref = @materializer_attempt_ref
  AND state = 'PENDING';
