-- name: sync_cursor__list_for_request :many
SELECT
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
FROM provider_hub_sync_cursors
WHERE provider_slug = @provider_slug
  AND scope_type = @scope_type
  AND scope_ref = @scope_ref
  AND artifact_kind = ANY(@artifact_kinds::text[])
ORDER BY array_position(@artifact_kinds::text[], artifact_kind);
