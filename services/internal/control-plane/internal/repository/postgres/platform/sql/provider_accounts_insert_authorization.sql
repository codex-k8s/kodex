-- name: provider_accounts_insert_authorization :exec
INSERT INTO control_plane.provider_authorization_attempts
    (ref, organization_id, provider_account_id, method, state,
     materializer_attempt_ref, verification_uri, user_code, expires_at,
     safe_failure_code, created_by)
VALUES
    (@authorization_ref, @organization_id::uuid, @account_id::uuid, @method, @state,
     @materializer_attempt_ref, @verification_uri, @user_code, @expires_at,
     @safe_failure_code, @created_by::uuid);
