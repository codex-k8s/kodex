-- name: provider_authorization_abandoned_cleanup :exec
INSERT INTO control_plane.provider_credential_cleanup_tasks
 (ref,organization_id,provider_account_id,target_kind,provider_authorization_attempt_id,
  materializer_attempt_ref,eligible_at,maximum_attempts)
SELECT 'pcct_'||gen_random_uuid()::text,attempt.organization_id,attempt.provider_account_id,
       'AUTHORIZATION_METADATA',attempt.id,
       'pmat_'||left(encode(sha256(convert_to(attempt.ref,'UTF8')||decode('00','hex')||convert_to(account.ref,'UTF8')),'hex'),32),
       clock_timestamp(),@maximum_attempts
FROM control_plane.provider_authorization_attempts attempt
JOIN control_plane.provider_accounts account ON account.id=attempt.provider_account_id AND account.organization_id=attempt.organization_id
WHERE attempt.provider_account_id=@account_id::uuid AND attempt.preparation_state='ABANDONED'
  AND NOT EXISTS (SELECT 1 FROM control_plane.provider_credential_cleanup_tasks task WHERE task.provider_authorization_attempt_id=attempt.id);
