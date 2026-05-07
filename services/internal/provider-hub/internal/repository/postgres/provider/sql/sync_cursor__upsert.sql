-- name: sync_cursor__upsert :one
INSERT INTO provider_hub_sync_cursors (
    id,
    provider_slug,
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
) VALUES (
    @id,
    @provider_slug,
    @scope_type,
    @scope_ref,
    @artifact_kind,
    @cursor_value,
    @overlap_since,
    @priority,
    @last_success_at,
    @last_checked_at,
    @last_error,
    @rate_budget_state_json,
    @lease_owner,
    @lease_until,
    @version,
    @created_at,
    @updated_at
)
ON CONFLICT (provider_slug, scope_type, scope_ref, artifact_kind) DO UPDATE SET
    priority = CASE
        WHEN provider_hub_sync_cursors.priority = 'hot' OR EXCLUDED.priority = 'hot' THEN 'hot'
        WHEN provider_hub_sync_cursors.priority = 'warm' OR EXCLUDED.priority = 'warm' THEN 'warm'
        ELSE 'cold'
    END,
    updated_at = EXCLUDED.updated_at,
    version = provider_hub_sync_cursors.version + 1
RETURNING
    id,
    provider_slug,
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
    updated_at;
