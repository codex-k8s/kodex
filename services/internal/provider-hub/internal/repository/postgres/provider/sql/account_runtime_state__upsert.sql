-- name: account_runtime_state__upsert :one
INSERT INTO provider_hub_account_runtime_states (
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
) VALUES (
    @id,
    @external_account_id,
    @provider_slug,
    @status,
    @last_checked_at,
    @last_success_at,
    @last_error_code,
    @last_error_message,
    @version,
    @created_at,
    @updated_at
)
ON CONFLICT (external_account_id, provider_slug) DO UPDATE SET
    status = CASE
        WHEN provider_hub_account_runtime_states.status = 'limited'
            AND EXCLUDED.status = 'active'
        THEN provider_hub_account_runtime_states.status
        ELSE EXCLUDED.status
    END,
    last_checked_at = COALESCE(EXCLUDED.last_checked_at, provider_hub_account_runtime_states.last_checked_at),
    last_success_at = COALESCE(EXCLUDED.last_success_at, provider_hub_account_runtime_states.last_success_at),
    last_error_code = EXCLUDED.last_error_code,
    last_error_message = EXCLUDED.last_error_message,
    version = provider_hub_account_runtime_states.version + 1,
    updated_at = EXCLUDED.updated_at
RETURNING
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
    updated_at;
