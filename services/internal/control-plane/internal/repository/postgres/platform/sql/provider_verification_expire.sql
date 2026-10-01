-- name: provider_verification_expire :many
WITH expired AS (
    SELECT verification.id,
        account.version <> verification.account_version
        OR account.current_credential_revision_id IS DISTINCT FROM verification.provider_credential_revision_id
        OR NOT account.enabled OR account.state <> 'AUTHORIZED' AS stale
    FROM control_plane.provider_account_verifications verification
    JOIN control_plane.provider_accounts account
      ON account.id = verification.provider_account_id AND account.organization_id = verification.organization_id
    LEFT JOIN control_plane.provider_model_catalog_tasks task ON task.id = verification.model_catalog_task_id
    WHERE verification.state = 'PENDING'
      AND (@account_id = '' OR account.id = NULLIF(@account_id, '')::uuid)
      AND (account.version <> verification.account_version
           OR account.current_credential_revision_id IS DISTINCT FROM verification.provider_credential_revision_id
           OR NOT account.enabled OR account.state <> 'AUTHORIZED'
           OR verification.deadline <= clock_timestamp() OR task.state = 'CANCELLED')
    ORDER BY verification.requested_at, verification.id
    LIMIT 128 FOR UPDATE OF account, verification SKIP LOCKED
), updated AS (
UPDATE control_plane.provider_account_verifications verification
SET state = CASE WHEN expired.stale THEN 'STALE' ELSE 'FAILED' END,
    safe_reason = CASE WHEN expired.stale THEN 'VERIFICATION_SOURCE_CHANGED' ELSE 'CREDENTIAL_VERIFICATION_FAILED' END,
    completed_at = clock_timestamp()
FROM expired WHERE verification.id = expired.id
RETURNING verification.organization_id, verification.provider_account_id,
          verification.ref, verification.account_version, verification.state
)
SELECT updated.organization_id::text, organization.ref, account.ref,
       updated.ref, updated.account_version, updated.state
FROM updated
JOIN control_plane.organizations organization ON organization.id = updated.organization_id
JOIN control_plane.provider_accounts account
  ON account.id = updated.provider_account_id AND account.organization_id = updated.organization_id;
