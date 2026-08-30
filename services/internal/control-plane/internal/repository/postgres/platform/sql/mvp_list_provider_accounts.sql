-- name: mvp_list_provider_accounts :many
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
    ORDER BY attempt_row.created_at DESC, attempt_row.ref DESC
    LIMIT 1
) attempt ON true
WHERE account.organization_id = @organization_id::uuid
  AND (@query = '' OR account.name ILIKE '%' || @query || '%'
       OR account.definition_key ILIKE '%' || @query || '%'
       OR account.external_account_masked ILIKE '%' || @query || '%')
  AND (@state = '' OR account.state = @state)
  AND (@definition_key = '' OR account.definition_key = @definition_key)
  AND (@cursor_time::timestamptz IS NULL OR (account.updated_at, account.ref) < (@cursor_time, @cursor_ref))
ORDER BY account.updated_at DESC, account.ref DESC
LIMIT @page_size;
