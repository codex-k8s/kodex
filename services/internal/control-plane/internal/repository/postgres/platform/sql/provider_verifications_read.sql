-- name: provider_verifications_read :many
SELECT account.ref, verification.ref,
    CASE WHEN changed.value THEN 'STALE'
         WHEN verification.state = 'PENDING' AND verification.deadline <= clock_timestamp() THEN 'FAILED'
         ELSE verification.state END,
    CASE WHEN changed.value THEN 'VERIFICATION_SOURCE_CHANGED'
         WHEN verification.state = 'PENDING' AND verification.deadline <= clock_timestamp() THEN 'CREDENTIAL_VERIFICATION_FAILED'
         ELSE verification.safe_reason END,
    verification.account_version, credential.revision_number, verification.requested_at,
    CASE WHEN changed.value OR verification.deadline <= clock_timestamp()
         THEN COALESCE(verification.completed_at, LEAST(verification.deadline, clock_timestamp()))
         ELSE verification.completed_at END
FROM control_plane.provider_accounts account
JOIN LATERAL (
    SELECT attempt.* FROM control_plane.provider_account_verifications attempt
    WHERE attempt.provider_account_id = account.id AND attempt.organization_id = account.organization_id
    ORDER BY attempt.requested_at DESC, attempt.id DESC LIMIT 1
) verification ON true
JOIN control_plane.provider_credential_revisions credential ON credential.id = verification.provider_credential_revision_id
CROSS JOIN LATERAL (SELECT account.version <> verification.account_version
    OR account.current_credential_revision_id IS DISTINCT FROM verification.provider_credential_revision_id
    OR NOT account.enabled OR account.state <> 'AUTHORIZED' AS value) changed
WHERE account.organization_id = @organization_id::uuid AND account.ref = ANY(@account_refs::text[]);
