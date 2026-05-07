-- name: sync_cursor__list :many
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
WHERE (@provider_slug::text = '' OR provider_slug = @provider_slug)
  AND (@scope_type::text = '' OR scope_type = @scope_type)
  AND (@scope_ref::text = '' OR scope_ref = @scope_ref)
  AND (cardinality(@artifact_kinds::text[]) = 0 OR artifact_kind = ANY(@artifact_kinds::text[]))
  AND (cardinality(@priorities::text[]) = 0 OR priority = ANY(@priorities::text[]))
  AND (@include_healthy::boolean OR last_error <> '')
ORDER BY
    CASE priority
        WHEN 'hot' THEN 1
        WHEN 'warm' THEN 2
        ELSE 3
    END,
    last_checked_at ASC NULLS FIRST,
    updated_at ASC,
    id
LIMIT @limit::integer OFFSET @offset::integer;
