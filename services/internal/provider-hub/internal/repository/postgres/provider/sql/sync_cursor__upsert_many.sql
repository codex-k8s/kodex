-- name: sync_cursor__upsert_many :many
WITH input_rows AS (
    SELECT *
    FROM unnest(@ids::uuid[], @artifact_kinds::text[]) AS input(id, artifact_kind)
),
upserted AS (
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
    )
    SELECT
        input_rows.id,
        @provider_slug,
        @scope_type,
        @scope_ref,
        input_rows.artifact_kind,
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
    FROM input_rows
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
        updated_at
)
SELECT *
FROM upserted
ORDER BY array_position(@artifact_kinds::text[], artifact_kind);
