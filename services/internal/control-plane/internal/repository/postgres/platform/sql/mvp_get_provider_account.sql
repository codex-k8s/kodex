-- name: mvp_get_provider_account :one
SELECT account.ref, account.definition_key, account.name, account.external_account_masked,
       account.state, account.enabled, account.version, account.created_at, account.updated_at,
       COALESCE(attempt.ref, ''), COALESCE(attempt.method, ''), COALESCE(attempt.state, ''),
       COALESCE(attempt.verification_uri, ''), COALESCE(attempt.user_code, ''), attempt.expires_at,
       COALESCE(attempt.safe_failure_code, ''), COALESCE(attempt.materializer_attempt_ref, '')
FROM control_plane.provider_accounts account
LEFT JOIN LATERAL (
    SELECT attempt_row.ref, attempt_row.method, attempt_row.state, attempt_row.materializer_attempt_ref,
           attempt_row.verification_uri, attempt_row.user_code, attempt_row.expires_at,
           attempt_row.safe_failure_code
    FROM control_plane.provider_authorization_attempts attempt_row
    WHERE attempt_row.provider_account_id = account.id
      AND attempt_row.preparation_state <> 'RESERVED'
    ORDER BY attempt_row.created_at DESC, attempt_row.ref DESC
    LIMIT 1
) attempt ON true
WHERE account.organization_id = @organization_id::uuid
  AND account.ref = @account_ref;
