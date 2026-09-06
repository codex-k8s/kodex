-- name: provider_verification_start :one
INSERT INTO control_plane.provider_account_verifications
    (ref, organization_id, provider_account_id, account_version, provider_credential_revision_id, requested_by)
SELECT @verification_ref, account.organization_id, account.id, account.version, account.current_credential_revision_id, @actor_id::uuid
FROM control_plane.provider_accounts account
WHERE account.organization_id = @organization_id::uuid AND account.id = @account_id::uuid
  AND account.enabled AND account.state = 'AUTHORIZED' AND account.current_credential_revision_id IS NOT NULL
  AND (SELECT attempt.method FROM control_plane.provider_authorization_attempts attempt
       WHERE attempt.provider_account_id = account.id AND attempt.organization_id = account.organization_id
         AND attempt.state = 'AUTHORIZED' ORDER BY attempt.updated_at DESC, attempt.ref DESC LIMIT 1) = 'DEVICE_CODE'
  AND NOT EXISTS (SELECT 1 FROM control_plane.provider_account_verifications pending
                  WHERE pending.provider_account_id = account.id AND pending.state = 'PENDING')
RETURNING id::text;
