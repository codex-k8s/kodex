-- name: account_runtime_state__get :one
SELECT
    id,
    external_account_id,
    provider_slug,
    status,
    last_checked_at,
    last_success_at,
    last_error_code,
    last_error_message,
    version,
    created_at,
    updated_at
FROM provider_hub_account_runtime_states
WHERE (
    @id::uuid IS NOT NULL
    AND id = @id
) OR (
    @id::uuid IS NULL
    AND @external_account_id::uuid IS NOT NULL
    AND external_account_id = @external_account_id
    AND (@provider_slug::text = '' OR provider_slug = @provider_slug)
)
ORDER BY updated_at DESC, id
LIMIT 1;
