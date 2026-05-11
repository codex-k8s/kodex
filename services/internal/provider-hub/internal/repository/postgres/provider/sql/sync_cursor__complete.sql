-- name: sync_cursor__complete :one
UPDATE provider_hub_sync_cursors AS cursor
SET
    cursor_value = @cursor_value,
    overlap_since = @overlap_since,
    priority = CASE
        WHEN @last_error::text = '' AND cursor.priority = 'hot' THEN 'warm'
        ELSE cursor.priority
    END,
    last_success_at = COALESCE(@last_success_at, cursor.last_success_at),
    last_checked_at = @now,
    last_error = @last_error,
    rate_budget_state_json = @rate_budget_state_json,
    lease_owner = @lease_owner,
    lease_until = @lease_until,
    updated_at = @now,
    version = cursor.version + 1
WHERE cursor.id = @id
  AND cursor.lease_owner = @expected_lease_owner
RETURNING
    cursor.id,
    cursor.provider_slug,
    cursor.external_account_id,
    cursor.scope_type,
    cursor.scope_ref,
    cursor.artifact_kind,
    cursor.cursor_value,
    cursor.overlap_since,
    cursor.priority,
    cursor.last_success_at,
    cursor.last_checked_at,
    cursor.last_error,
    cursor.rate_budget_state_json,
    cursor.lease_owner,
    cursor.lease_until,
    cursor.version,
    cursor.created_at,
    cursor.updated_at;
