-- name: provider_accounts_is_api_key :one
SELECT COALESCE((
    SELECT attempt.method = 'API_KEY'
    FROM control_plane.provider_authorization_attempts attempt
    WHERE attempt.organization_id = @organization_id::uuid
      AND attempt.provider_account_id = @account_id::uuid
      AND attempt.state = 'AUTHORIZED'
    ORDER BY attempt.updated_at DESC, attempt.ref DESC
    LIMIT 1
), false);
