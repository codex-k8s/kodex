-- name: runtime_catalog__bootstrap_accounts :many
WITH eligible AS (
    SELECT account.ref,
           COALESCE(auth_attempt.method, '') AS authorization_method
    FROM control_plane.provider_accounts account
    LEFT JOIN LATERAL (
        SELECT attempt.method
        FROM control_plane.provider_authorization_attempts attempt
        WHERE attempt.organization_id = account.organization_id
          AND attempt.provider_account_id = account.id
          AND attempt.state = 'AUTHORIZED'
          AND attempt.preparation_state = 'APPLIED'
        ORDER BY attempt.updated_at DESC, attempt.id DESC
        LIMIT 1
    ) auth_attempt ON true
    WHERE account.organization_id = $1::uuid AND account.definition_key = $2
      AND account.enabled AND account.state = 'AUTHORIZED'
      AND account.current_credential_revision_id IS NOT NULL
)
SELECT candidate.ref
FROM eligible candidate
WHERE candidate.authorization_method = 'DEVICE_CODE'
   OR NOT EXISTS (
       SELECT 1 FROM eligible preferred
       WHERE preferred.authorization_method = 'DEVICE_CODE'
   )
ORDER BY candidate.ref
LIMIT 32;
