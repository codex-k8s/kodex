-- name: provider_accounts_insert_authorization :exec
INSERT INTO control_plane.provider_authorization_attempts
    (ref, organization_id, provider_account_id, method, state,
     materializer_attempt_ref, verification_uri, user_code, expires_at,
     safe_failure_code, created_by, materializer_attempt_uid, materializer_attempt_resource_version)
VALUES
    (@authorization_ref, @organization_id::uuid, @account_id::uuid, @method, @state,
     @materializer_attempt_ref, @verification_uri, @user_code, @expires_at,
     @safe_failure_code, @created_by::uuid, NULLIF(@materializer_attempt_uid, '')::uuid,
     NULLIF(@materializer_attempt_resource_version, ''))
ON CONFLICT (ref) DO UPDATE
SET state=EXCLUDED.state, preparation_state='APPLIED', materializer_attempt_ref=EXCLUDED.materializer_attempt_ref,
    materializer_attempt_uid=EXCLUDED.materializer_attempt_uid,
    materializer_attempt_resource_version=EXCLUDED.materializer_attempt_resource_version,
    verification_uri=EXCLUDED.verification_uri,user_code=EXCLUDED.user_code,expires_at=EXCLUDED.expires_at,
    safe_failure_code=EXCLUDED.safe_failure_code,version=provider_authorization_attempts.version+1,updated_at=clock_timestamp()
WHERE provider_authorization_attempts.preparation_state='RESERVED'
  AND provider_authorization_attempts.organization_id=EXCLUDED.organization_id
  AND provider_authorization_attempts.provider_account_id=EXCLUDED.provider_account_id
  AND provider_authorization_attempts.created_by=EXCLUDED.created_by
  AND provider_authorization_attempts.method=EXCLUDED.method
  AND provider_authorization_attempts.reservation_deadline>clock_timestamp()
  AND EXISTS (SELECT 1 FROM control_plane.provider_accounts account
              WHERE account.id=EXCLUDED.provider_account_id
                AND account.version=provider_authorization_attempts.reserved_account_version
                AND account.state='PENDING_AUTHORIZATION');
