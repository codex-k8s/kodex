-- name: sync_cursor__claim :one
WITH candidates AS (
    SELECT id
    FROM provider_hub_sync_cursors
    WHERE (@id::uuid IS NULL OR id = @id)
      AND (@provider_slug::text = '' OR provider_slug = @provider_slug)
      AND (lease_until IS NULL OR lease_until <= @now)
    ORDER BY
        CASE priority
            WHEN 'hot' THEN 1
            WHEN 'warm' THEN 2
            ELSE 3
        END,
        last_checked_at ASC NULLS FIRST,
        updated_at ASC,
        id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE provider_hub_sync_cursors AS cursor
SET
    lease_owner = @lease_owner,
    lease_until = @lease_until,
    last_checked_at = @now,
    updated_at = @now,
    version = cursor.version + 1
FROM candidates
WHERE cursor.id = candidates.id
RETURNING
    cursor.id,
    cursor.provider_slug,
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
