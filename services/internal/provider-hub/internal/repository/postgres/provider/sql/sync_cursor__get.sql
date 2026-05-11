-- name: sync_cursor__get :one
SELECT
    id,
    provider_slug,
    external_account_id,
    scope_type,
    scope_ref,
    artifact_kind,
    cursor_value,
    overlap_since,
    priority,
    last_success_at,
    last_checked_at,
    last_error,
    rate_budget_state_json,
    lease_owner,
    lease_until,
    version,
    created_at,
    updated_at
FROM provider_hub_sync_cursors
WHERE id = @id;
