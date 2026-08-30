-- name: provider_accounts_fail_pending_authorizations :exec
UPDATE control_plane.provider_authorization_attempts
SET state = 'FAILED', safe_failure_code = @safe_failure_code,
    verification_uri = '', user_code = '', version = version + 1,
    updated_at = clock_timestamp()
WHERE provider_account_id = @account_id::uuid
  AND state = 'PENDING';
