-- name: account_runtime_state__upsert_from_snapshot :one
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
        WHEN provider_hub_account_runtime_states.last_checked_at IS NOT NULL
            AND EXCLUDED.last_checked_at < provider_hub_account_runtime_states.last_checked_at
        THEN provider_hub_account_runtime_states.status
        WHEN provider_hub_account_runtime_states.status = 'limited'
            AND EXCLUDED.status = 'active'
        THEN provider_hub_account_runtime_states.status
        ELSE EXCLUDED.status
    END,
    last_checked_at = CASE
        WHEN provider_hub_account_runtime_states.last_checked_at IS NULL
            OR EXCLUDED.last_checked_at >= provider_hub_account_runtime_states.last_checked_at
        THEN COALESCE(EXCLUDED.last_checked_at, provider_hub_account_runtime_states.last_checked_at)
        ELSE provider_hub_account_runtime_states.last_checked_at
    END,
    last_success_at = CASE
        WHEN provider_hub_account_runtime_states.last_success_at IS NULL
            OR (
                EXCLUDED.last_success_at IS NOT NULL
                AND EXCLUDED.last_success_at >= provider_hub_account_runtime_states.last_success_at
            )
        THEN COALESCE(EXCLUDED.last_success_at, provider_hub_account_runtime_states.last_success_at)
        ELSE provider_hub_account_runtime_states.last_success_at
    END,
    last_error_code = CASE
        WHEN provider_hub_account_runtime_states.last_checked_at IS NOT NULL
            AND EXCLUDED.last_checked_at < provider_hub_account_runtime_states.last_checked_at
        THEN provider_hub_account_runtime_states.last_error_code
        ELSE EXCLUDED.last_error_code
    END,
    last_error_message = CASE
        WHEN provider_hub_account_runtime_states.last_checked_at IS NOT NULL
            AND EXCLUDED.last_checked_at < provider_hub_account_runtime_states.last_checked_at
        THEN provider_hub_account_runtime_states.last_error_message
        ELSE EXCLUDED.last_error_message
    END,
    version = CASE
        WHEN provider_hub_account_runtime_states.last_checked_at IS NOT NULL
            AND EXCLUDED.last_checked_at < provider_hub_account_runtime_states.last_checked_at
        THEN provider_hub_account_runtime_states.version
        ELSE provider_hub_account_runtime_states.version + 1
    END,
    updated_at = CASE
        WHEN provider_hub_account_runtime_states.last_checked_at IS NOT NULL
            AND EXCLUDED.last_checked_at < provider_hub_account_runtime_states.last_checked_at
        THEN provider_hub_account_runtime_states.updated_at
        ELSE EXCLUDED.updated_at
    END
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
